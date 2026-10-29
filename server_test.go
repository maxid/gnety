package gnety

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func echoHandler() HandlerFuncs {
	return HandlerFuncs{Message: func(s *Session, msg any) {
		_ = s.Send(msg)
	}}
}

func TestTCPLengthFieldEcho(t *testing.T) {
	srv, err := New(Config{Network: "tcp", Addr: "127.0.0.1:0", Codec: LengthFieldCodec{}, Handler: echoHandler()})
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
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	payload := []byte("hello-gnety")
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	if _, err := conn.Write(frame); err != nil {
		t.Fatal(err)
	}
	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		t.Fatal(err)
	}
	n := int(binary.BigEndian.Uint32(lenBuf[:]))
	buf := make([]byte, n)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("echo = %q, want %q", buf, payload)
	}
	if srv.SessionCount() != 1 {
		t.Fatalf("session count = %d, want 1", srv.SessionCount())
	}
}

func TestDelimiterCodec(t *testing.T) {
	r := bufio.NewReaderSize(stringsReader("a\x00b\x00"), 32)
	c := DelimiterCodec{Delimiter: 0}
	msg1, err := c.Decode(r)
	if err != nil || string(msg1.([]byte)) != "a" {
		t.Fatalf("first frame: %v %v", msg1, err)
	}
	msg2, err := c.Decode(r)
	if err != nil || string(msg2.([]byte)) != "b" {
		t.Fatalf("second frame: %v %v", msg2, err)
	}
}

func TestUDPEcho(t *testing.T) {
	srv, err := New(Config{Network: "udp", Addr: "127.0.0.1:0", DatagramCodec: BytesDatagramCodec{}, Handler: echoHandler()})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(context.Background())

	addr, err := net.ResolveUDPAddr("udp", srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "ping" {
		t.Fatalf("udp echo = %q", buf[:n])
	}
}

func TestRejectInvalidConfig(t *testing.T) {
	if _, err := New(Config{Network: "tcp", Addr: ":0"}); err == nil {
		t.Fatal("expected missing codec error")
	}
	if _, err := New(Config{Network: "sctp", Addr: ":0"}); err == nil {
		t.Fatal("expected unsupported network error")
	}
}

// stringsReader avoids importing strings just for the delimiter test.
func stringsReader(s string) io.Reader { return &stringReader{s: s} }

type stringReader struct {
	s string
	i int
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}
