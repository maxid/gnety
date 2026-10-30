package gnety

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestMaxSessions(t *testing.T) {
	srv, err := New(Config{Network: "tcp", Addr: "127.0.0.1:0", Codec: LineCodec{}, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(context.Background())
	first, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	deadline := time.Now().Add(time.Second)
	for srv.SessionCount() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	second, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	_ = second.SetDeadline(time.Now().Add(time.Second))
	_, _ = second.Write([]byte("x\n"))
	buf := make([]byte, 8)
	if _, err := second.Read(buf); err == nil {
		t.Fatal("second connection should be rejected")
	}
}

func TestMaxPendingRequests(t *testing.T) {
	policy := RequestKeyPolicyFuncs{
		Request:  func(msg any) (string, bool) { return string(msg.([]byte)), true },
		Response: func(msg any) (string, bool) { return string(msg.([]byte)), true },
	}
	srv := &Server{cfg: Config{MaxPendingRequests: 1, RequestKeyPolicy: policy, Codec: LineCodec{}}}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	sess := &Session{srv: srv, conn: a}
	sess.init()
	go func() {
		buf := make([]byte, 32)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = sess.Request(ctx, []byte("one")) }()
	deadline := time.Now().Add(time.Second)
	for {
		sess.pendingMu.Lock()
		n := len(sess.pending)
		sess.pendingMu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := sess.Request(context.Background(), []byte("two")); !errors.Is(err, ErrTooManyPending) {
		t.Fatalf("expected ErrTooManyPending, got %v", err)
	}
}

func TestWriteTimeout(t *testing.T) {
	srv := &Server{cfg: Config{WriteTimeout: 20 * time.Millisecond, Codec: LineCodec{}}}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	sess := &Session{srv: srv, conn: a}
	sess.init()
	err := sess.Send(bytes.Repeat([]byte("x"), 1<<20))
	if !errors.Is(err, ErrWriteTimeout) {
		t.Fatalf("expected ErrWriteTimeout, got %v", err)
	}
}

func TestDTLSClientRequiresExplicitInsecure(t *testing.T) {
	_, err := Dial(context.Background(), ClientConfig{
		Network: "dtls", Addr: "127.0.0.1:1", DatagramCodec: BytesDatagramCodec{},
	})
	if err == nil {
		t.Fatal("expected DTLS client config error without CA")
	}
}
