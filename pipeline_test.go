package gnety

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestMiddlewareChainOrder(t *testing.T) {
	var order []string
	base := HandlerFuncs{Message: func(*Session, any) { order = append(order, "base") }}
	mw := func(name string) Middleware {
		return func(next Handler) Handler {
			return HandlerFuncs{Message: func(s *Session, msg any) {
				order = append(order, name+":before")
				next.OnMessage(s, msg)
				order = append(order, name+":after")
			}}
		}
	}
	h := Chain(base, mw("a"), mw("b"))
	h.OnMessage(&Session{}, []byte("x"))
	want := []string{"a:before", "b:before", "base", "b:after", "a:after"}
	if len(order) != len(want) {
		t.Fatalf("order = %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestLineAndFixedCodecs(t *testing.T) {
	var buf bytes.Buffer
	line := LineCodec{}
	if err := line.Encode(&buf, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := line.Decode(bufio.NewReader(&buf))
	if err != nil || string(got.([]byte)) != "hello" {
		t.Fatalf("line = %q err=%v", got, err)
	}

	var fixed bytes.Buffer
	fc := FixedLengthCodec{Size: 3}
	if err := fc.Encode(&fixed, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	got, err = fc.Decode(bufio.NewReader(&fixed))
	if err != nil || string(got.([]byte)) != "abc" {
		t.Fatalf("fixed = %q err=%v", got, err)
	}
	if err := fc.Encode(&fixed, []byte("ab")); err == nil {
		t.Fatal("expected size error")
	}
}

func TestBroadcast(t *testing.T) {
	srv, err := New(Config{Network: "tcp", Addr: "127.0.0.1:0", Codec: LineCodec{}, Handler: HandlerFuncs{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(context.Background())
	var conns []net.Conn
	for i := 0; i < 2; i++ {
		conn, err := net.Dial("tcp", srv.Addr())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conns = append(conns, conn)
	}
	deadline := time.Now().Add(time.Second)
	for srv.SessionCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := srv.Broadcast([]byte("hi")); n != 2 {
		t.Fatalf("broadcast count = %d", n)
	}
	for _, conn := range conns {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		r := bufio.NewReader(conn)
		line, err := r.ReadString('\n')
		if err != nil || line != "hi\n" {
			t.Fatalf("line = %q err=%v", line, err)
		}
	}
	_ = slog.Default()
}

func TestRawHandlerTCP(t *testing.T) {
	srv, err := New(Config{
		Network: "tcp", Addr: "127.0.0.1:0",
		RawHandler: func(conn net.Conn) {
			defer conn.Close()
			buf := make([]byte, 4)
			if _, err := io.ReadFull(conn, buf); err != nil {
				return
			}
			_, _ = conn.Write(buf)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(context.Background())
	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte("raw!")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "raw!" {
		t.Fatalf("raw echo = %q", buf)
	}
}
