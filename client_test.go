package gnety

import (
	"context"
	"testing"
	"time"
)

func TestTCPClientEcho(t *testing.T) {
	srv, err := New(Config{Network: "tcp", Addr: "127.0.0.1:0", Codec: LineCodec{}, Handler: HandlerFuncs{
		Message: func(s *Session, msg any) { _ = s.Send(msg) },
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(context.Background())
	cli, err := Dial(context.Background(), ClientConfig{Network: "tcp", Addr: srv.Addr(), Codec: LineCodec{}})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := cli.Send([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	msg, err := cli.Receive(ctx)
	if err != nil || string(msg.([]byte)) != "hello" {
		t.Fatalf("msg=%v err=%v", msg, err)
	}
}

func TestUDPClientEcho(t *testing.T) {
	srv, err := New(Config{Network: "udp", Addr: "127.0.0.1:0", DatagramCodec: BytesDatagramCodec{}, Handler: HandlerFuncs{
		Message: func(s *Session, msg any) { _ = s.Send(msg) },
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(context.Background())
	cli, err := Dial(context.Background(), ClientConfig{Network: "udp", Addr: srv.Addr(), DatagramCodec: BytesDatagramCodec{}})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := cli.Send([]byte("udp")); err != nil {
		t.Fatal(err)
	}
	msg, err := cli.Receive(ctx)
	if err != nil || string(msg.([]byte)) != "udp" {
		t.Fatalf("msg=%v err=%v", msg, err)
	}
}
