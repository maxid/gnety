package gnety

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// recorder collects metrics events for assertions.
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(e string) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

func (r *recorder) Snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func (r *recorder) has(want string) bool {
	for _, e := range r.Snapshot() {
		if e == want {
			return true
		}
	}
	return false
}

func (r *recorder) SessionOpen(transport string) { r.add("open:" + transport) }
func (r *recorder) SessionClose(transport, reason string) {
	r.add("close:" + transport + ":" + reason)
}
func (r *recorder) SessionRejected(transport, reason string) {
	r.add("reject:" + transport + ":" + reason)
}
func (r *recorder) MessageIn(transport string)  { r.add("in:" + transport) }
func (r *recorder) MessageOut(transport string) { r.add("out:" + transport) }
func (r *recorder) WriteFailed(transport, reason string) {
	r.add("writefail:" + transport + ":" + reason)
}

func (r *recorder) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r.has(want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("metric %q never fired; got %v", want, r.Snapshot())
}

func TestMetricsSessionLifecycle(t *testing.T) {
	rec := &recorder{}
	got := make(chan any, 1)
	srv, err := New(Config{
		Network: "tcp",
		Addr:    "127.0.0.1:0",
		Codec:   LengthFieldCodec{},
		Metrics: rec,
		Handler: HandlerFuncs{
			Open:    func(s *Session) { _ = s.Send([]byte("hi")) },
			Message: func(_ *Session, msg any) { got <- msg },
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Stop(context.Background()) }()

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Server greeting: 4-byte big-endian length + "hi".
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		t.Fatalf("read greeting header: %v", err)
	}
	if n := binary.BigEndian.Uint32(header); n != 2 {
		t.Fatalf("greeting length = %d, want 2", n)
	}
	body := make([]byte, 2)
	if _, err := io.ReadFull(conn, body); err != nil {
		t.Fatalf("read greeting: %v", err)
	}

	// Client frame: length 2 + payload.
	frame := []byte{0, 0, 0, 2, 0xaa, 0xbb}
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never received the frame")
	}

	rec.waitFor(t, "open:tcp")
	rec.waitFor(t, "out:tcp")
	rec.waitFor(t, "in:tcp")

	_ = conn.Close()
	rec.waitFor(t, "close:tcp:closed")
}

func TestWriteBackpressure(t *testing.T) {
	rec := &recorder{}
	opened := make(chan *Session, 1)
	srv, err := New(Config{
		Network:         "tcp",
		Addr:            "127.0.0.1:0",
		Codec:           LengthFieldCodec{},
		Metrics:         rec,
		MaxQueuedWrites: 1,
		Handler: HandlerFuncs{
			Open: func(s *Session) { opened <- s },
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Stop(context.Background()) }()

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	var sess *Session
	select {
	case sess = <-opened:
	case <-time.After(2 * time.Second):
		t.Fatal("session never opened")
	}

	// Occupy the single write slot, then a second writer must be rejected
	// instead of queueing.
	sess.writeSem <- struct{}{}
	if err := sess.Send([]byte("x")); !errors.Is(err, ErrWriteBusy) {
		t.Fatalf("Send with a full write queue = %v, want ErrWriteBusy", err)
	}
	rec.waitFor(t, "writefail:tcp:backpressure")

	// Releasing the slot makes the next write work again.
	<-sess.writeSem
	if err := sess.Send([]byte("ok")); err != nil {
		t.Fatalf("Send after release: %v", err)
	}
	rec.waitFor(t, "out:tcp")
}

func TestMaxQueuedWritesResolution(t *testing.T) {
	cases := []struct {
		cfg  int
		want int
	}{
		{0, DefaultMaxQueuedWrites},
		{-1, 0},
		{3, 3},
	}
	for _, tc := range cases {
		srv, err := New(Config{Network: "tcp", Codec: LengthFieldCodec{}, MaxQueuedWrites: tc.cfg})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if got := srv.maxQueuedWrites(); got != tc.want {
			t.Fatalf("MaxQueuedWrites %d resolved to %d, want %d", tc.cfg, got, tc.want)
		}
	}
}
