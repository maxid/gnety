package gnety

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestWebSocketOriginPolicy(t *testing.T) {
	srv, err := New(Config{
		Network: "ws", Addr: "127.0.0.1:0", Path: "/origin",
		Subprotocol: "gnety", OriginPatterns: []string{"example.com"},
		RawHandler: func(conn net.Conn) {
			buf := make([]byte, 64)
			for {
				if _, err := conn.Read(buf); err != nil {
					return
				}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	good, err := Dial(ctx, ClientConfig{
		Network: "ws", Addr: srv.Addr(), Path: "/origin", Subprotocol: "gnety",
		Origin: "https://example.com", Codec: LineCodec{},
	})
	if err != nil {
		t.Fatalf("allowed origin: %v", err)
	}
	_ = good.Close()
	if _, err := Dial(ctx, ClientConfig{
		Network: "ws", Addr: srv.Addr(), Path: "/origin", Subprotocol: "gnety",
		Origin: "https://evil.example", Codec: LineCodec{},
	}); err == nil {
		t.Fatal("expected origin rejection")
	}
}

func TestWebSocketSubprotocolAndReadLimit(t *testing.T) {
	srv, err := New(Config{
		Network: "ws", Addr: "127.0.0.1:0", Path: "/echo", Subprotocol: "gnety",
		Codec: LineCodec{}, Handler: HandlerFuncs{Message: func(s *Session, msg any) { _ = s.Send(msg) }},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cli, err := Dial(ctx, ClientConfig{
		Network: "ws", Addr: srv.Addr(), Path: "/echo", Subprotocol: "gnety", Codec: LineCodec{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ws, ok := cli.WebSocket()
	if !ok {
		t.Fatal("expected WebSocket connection")
	}
	if ws.Subprotocol() != "gnety" {
		t.Fatalf("subprotocol = %q", ws.Subprotocol())
	}
	ws.SetReadLimit(4)
	if err := cli.Send([]byte("12345")); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.Receive(ctx); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("expected ErrFrameTooLarge, got %v", err)
	}
}

func TestWebSocketPing(t *testing.T) {
	srv, err := New(Config{
		Network: "ws", Addr: "127.0.0.1:0", Path: "/ping",
		RawHandler: func(conn net.Conn) {
			buf := make([]byte, 64)
			for {
				if _, err := conn.Read(buf); err != nil {
					return
				}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cli, err := Dial(ctx, ClientConfig{
		Network: "ws", Addr: srv.Addr(), Path: "/ping", Codec: LineCodec{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ws, ok := cli.WebSocket()
	if !ok {
		t.Fatal("expected WebSocket connection")
	}
	readErr := make(chan error, 1)
	go func() {
		_, err := cli.Receive(ctx)
		readErr <- err
	}()
	if err := ws.Ping(ctx, []byte("probe")); err != nil {
		t.Fatalf("ping: %v", err)
	}
	_ = ws.CloseWithStatus(WebSocketStatusGoingAway, "done")
	select {
	case <-readErr:
	case <-time.After(time.Second):
	}
}
