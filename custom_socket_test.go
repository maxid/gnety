package gnety

import (
	"context"
	"net"
	"testing"
)

type stubListener struct {
	addr net.Addr
}

func (l *stubListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (l *stubListener) Close() error              { return nil }
func (l *stubListener) Addr() net.Addr            { return l.addr }

func TestServerUsesProvidedListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(Config{Network: "tcp", Listener: ln, Codec: LengthFieldCodec{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if srv.Addr() != ln.Addr().String() {
		t.Fatalf("addr = %q, want %q", srv.Addr(), ln.Addr())
	}
	_ = srv.Stop(context.Background())
}

func TestServerUsesProvidedPacketConn(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(Config{Network: "udp", PacketConn: pc, DatagramCodec: BytesDatagramCodec{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if srv.Addr() != pc.LocalAddr().String() {
		t.Fatalf("addr = %q, want %q", srv.Addr(), pc.LocalAddr())
	}
	_ = srv.Stop(context.Background())
}

func TestProvidedSocketsAreValidated(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	pc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer pc.Close()

	if _, err := New(Config{Network: "udp", Listener: ln, DatagramCodec: BytesDatagramCodec{}}); err == nil {
		t.Fatal("udp accepted a stream listener")
	}
	if _, err := New(Config{Network: "tcp", PacketConn: pc, Codec: LengthFieldCodec{}}); err == nil {
		t.Fatal("tcp accepted a packet conn")
	}
	if _, err := New(Config{Network: "tcp", Listener: ln, PacketConn: pc, Codec: LengthFieldCodec{}}); err == nil {
		t.Fatal("both sockets accepted")
	}
}

func TestProvidedTLSListenerSkipsCertificateWrapping(t *testing.T) {
	srv, err := New(Config{
		Network:  "tcp-tls",
		Listener: &stubListener{addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}},
		Codec:    LengthFieldCodec{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if srv.cfg.Listener == nil {
		t.Fatal("provided listener was not stored")
	}
}

func TestNewClientWrapsConn(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	client, err := NewClient(left, ClientConfig{Network: "tcp", Codec: LengthFieldCodec{}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if client.LocalAddr().String() != left.LocalAddr().String() {
		t.Fatalf("local addr = %v, want %v", client.LocalAddr(), left.LocalAddr())
	}
	if _, err := NewClient(nil, ClientConfig{}); err == nil {
		t.Fatal("nil conn accepted")
	}
}

func TestNewClientDefaultsToTCP(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	client, err := NewClient(left, ClientConfig{Codec: LengthFieldCodec{}})
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
}

var _ net.Listener = (*stubListener)(nil)
