package gnety

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
)

func boolPtr(v bool) *bool { return &v }

func TestSocketOptionsValidation(t *testing.T) {
	if err := (SocketOptions{KeepAlive: time.Second}).validate("udp"); err == nil {
		t.Fatal("UDP accepted TCP keepalive")
	}
	if err := (SocketOptions{NoDelay: boolPtr(false)}).validate("udp"); err == nil {
		t.Fatal("UDP accepted TCP no-delay")
	}
	if err := (SocketOptions{ReadBuffer: -1}).validate("tcp"); err == nil {
		t.Fatal("negative buffer accepted")
	}
	if err := (SocketOptions{ReuseAddr: true}).validate("dtls"); !errors.Is(err, ErrSocketOptionsUnsupported) {
		t.Fatalf("DTLS error = %v", err)
	}
	if err := (SocketOptions{KeepAlive: time.Second, NoDelay: boolPtr(true)}).validate("tcp"); err != nil {
		t.Fatal(err)
	}
}

func TestControlRunsAtListen(t *testing.T) {
	var called bool
	srv, err := New(Config{
		Network: "tcp",
		Addr:    "127.0.0.1:0",
		Codec:   LengthFieldCodec{},
		Socket: SocketOptions{Control: func(_, _ string, _ syscall.RawConn) error {
			called = true
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Stop(context.Background()) }()
	if !called {
		t.Fatal("Control was not called for the listener")
	}
}

func TestClientLocalAddr(t *testing.T) {
	srv, err := New(Config{Network: "tcp", Addr: "127.0.0.1:0", Codec: LengthFieldCodec{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Stop(context.Background()) }()

	client, err := Dial(context.Background(), ClientConfig{
		Network:   "tcp",
		Addr:      srv.Addr(),
		Codec:     LengthFieldCodec{},
		LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if host, _, err := net.SplitHostPort(client.LocalAddr().String()); err != nil || host != "127.0.0.1" {
		t.Fatalf("local addr = %v", client.LocalAddr())
	}
}

func TestDTLSRejectsLocalAddr(t *testing.T) {
	_, err := Dial(context.Background(), ClientConfig{
		Network:       "dtls",
		Addr:          "127.0.0.1:1",
		DatagramCodec: BytesDatagramCodec{},
		LocalAddr:     &net.UDPAddr{IP: net.ParseIP("127.0.0.1")},
	})
	if !errors.Is(err, ErrSocketOptionsUnsupported) {
		t.Fatalf("err = %v", err)
	}
}
