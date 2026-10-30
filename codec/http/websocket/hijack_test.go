package websocket

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// wrappedWriter exposes the underlying writer through Unwrap only, mimicking
// middleware that wraps a ResponseWriter without re-implementing Hijacker.
type wrappedWriter struct{ http.ResponseWriter }

func (w wrappedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// startEchoServer runs a single-connection echo server and reports the outcome
// of the server side, so a test can assert on the handshake error too.
func startEchoServer(t *testing.T, accept func(http.ResponseWriter, *http.Request) (*Conn, error)) (addr string, serverErr <-chan error) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	outcome := make(chan error, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := accept(w, r)
		if err != nil {
			outcome <- err
			return
		}
		defer conn.Close()

		buf := make([]byte, 64)
		n, err := conn.Read(buf)
		if err != nil {
			outcome <- err
			return
		}
		_, err = conn.Write(buf[:n])
		outcome <- err
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	return ln.Addr().String(), outcome
}

func dial(t *testing.T, addr string, opts Options) (*Conn, error) {
	t.Helper()
	raw, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	u := &url.URL{Scheme: "ws", Host: addr, Path: "/ws"}
	conn, err := ClientHandshake(raw, u, "", opts)
	if err != nil {
		raw.Close()
		return nil, err
	}
	return conn, nil
}

func echoRoundTrip(t *testing.T, conn *Conn) {
	t.Helper()
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	got := make([]byte, 5)
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("echo = %q, want hello", got)
	}
}

func waitServer(t *testing.T, outcome <-chan error) error {
	t.Helper()
	select {
	case err := <-outcome:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("server did not finish")
		return nil
	}
}

// The writer handed to Accept is hijackable, so no hook is needed.
func TestAcceptUsesTheGivenWriter(t *testing.T) {
	addr, outcome := startEchoServer(t, func(w http.ResponseWriter, r *http.Request) (*Conn, error) {
		return Accept(w, r, Options{})
	})

	conn, err := dial(t, addr, Options{})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	defer conn.Close()

	echoRoundTrip(t, conn)
	if err := waitServer(t, outcome); err != nil {
		t.Fatalf("server: %v", err)
	}
}

// A wrapped writer is the caller's problem: the library reports it instead of
// unwrapping on its own.
func TestAcceptRejectsWrappedWriterWithoutHook(t *testing.T) {
	addr, outcome := startEchoServer(t, func(w http.ResponseWriter, r *http.Request) (*Conn, error) {
		return Accept(wrappedWriter{w}, r, Options{})
	})

	if _, err := dial(t, addr, Options{}); err == nil {
		t.Fatal("handshake succeeded behind a wrapper, want a clear failure")
	}

	err := waitServer(t, outcome)
	if err == nil || !strings.Contains(err.Error(), "hijacking unsupported") {
		t.Fatalf("server error = %v, want hijacking unsupported", err)
	}
}

// The caller can supply the connection, which is how a framework that hides
// the hijackable writer adapts to the library.
func TestAcceptWithExplicitHijackHook(t *testing.T) {
	addr, outcome := startEchoServer(t, func(w http.ResponseWriter, r *http.Request) (*Conn, error) {
		return Accept(wrappedWriter{w}, r, Options{
			Hijack: func(w http.ResponseWriter) (net.Conn, *bufio.ReadWriter, error) {
				return http.NewResponseController(w).Hijack()
			},
		})
	})

	conn, err := dial(t, addr, Options{})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	defer conn.Close()

	echoRoundTrip(t, conn)
	if err := waitServer(t, outcome); err != nil {
		t.Fatalf("server: %v", err)
	}
}
