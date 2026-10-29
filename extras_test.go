package gnety

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func readLenFrame(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		t.Fatal(err)
	}
	n := int(binary.BigEndian.Uint32(hdr[:]))
	buf := make([]byte, n)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	return buf
}

func TestRequestResponseCorrelation(t *testing.T) {
	policy := RequestKeyPolicyFuncs{
		Request: func(msg any) (string, bool) {
			b, ok := msg.([]byte)
			if !ok || len(b) < 6 || string(b[:2]) != "Q:" {
				return "", false
			}
			return string(b[2:6]), true
		},
		Response: func(msg any) (string, bool) {
			b, ok := msg.([]byte)
			if !ok || len(b) < 6 || string(b[:2]) != "A:" {
				return "", false
			}
			return string(b[2:6]), true
		},
	}
	got := make(chan string, 1)
	srv, err := New(Config{
		Network:          "tcp",
		Addr:             "127.0.0.1:0",
		Codec:            LengthFieldCodec{},
		RequestKeyPolicy: policy,
		Handler: HandlerFuncs{Open: func(s *Session) {
			go func() {
				resp, err := s.Request(context.Background(), []byte("Q:abcd"))
				if err != nil {
					got <- "ERR:" + err.Error()
					return
				}
				got <- string(resp.([]byte))
			}()
		}},
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
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if req := readLenFrame(t, conn); string(req) != "Q:abcd" {
		t.Fatalf("request = %q", req)
	}
	payload := []byte("A:abcd")
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	if _, err := conn.Write(frame); err != nil {
		t.Fatal(err)
	}
	select {
	case resp := <-got:
		if resp != "A:abcd" {
			t.Fatalf("response = %q", resp)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for correlated response")
	}
}

func TestHeartbeat(t *testing.T) {
	srv, err := New(Config{
		Network:           "tcp",
		Addr:              "127.0.0.1:0",
		Codec:             LengthFieldCodec{},
		HeartbeatInterval: 20 * time.Millisecond,
		Heartbeat:         func(*Session) any { return []byte("hb") },
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
	for i := 0; i < 2; i++ {
		if got := readLenFrame(t, conn); string(got) != "hb" {
			t.Fatalf("heartbeat = %q", got)
		}
	}
}

func TestAdvancedLengthFieldCodec(t *testing.T) {
	codec := LengthFieldCodec{
		Magic:             []byte{0xAA, 0x55},
		LengthFieldOffset: 4,
		LengthFieldSize:   2,
		LittleEndian:      true,
		MaxFrameSize:      1024,
	}
	var buf bytes.Buffer
	if err := codec.Encode(&buf, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	got, err := codec.Decode(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if string(got.([]byte)) != "payload" {
		t.Fatalf("decoded = %q", got)
	}
}

func TestBCD(t *testing.T) {
	b, err := EncodeBCD("12345")
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{0x01, 0x23, 0x45}; !bytes.Equal(b, want) {
		t.Fatalf("bcd = %x, want %x", b, want)
	}
	s, err := DecodeBCD(b)
	if err != nil || s != "012345" {
		t.Fatalf("decode = %q, %v", s, err)
	}
	if _, err := EncodeBCD("12a"); err == nil {
		t.Fatal("expected invalid digit error")
	}
}
