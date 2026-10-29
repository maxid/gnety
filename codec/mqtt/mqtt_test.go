package mqtt

import (
	"bufio"
	"bytes"
	"errors"
	"testing"
)

func TestRemainingLengthRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, 127, 128, 16383, 16384, 2097151, 2097152, MaxRemainingLength} {
		enc := AppendRemainingLength(nil, n)
		got, err := ReadRemainingLength(bufio.NewReader(bytes.NewReader(enc)))
		if err != nil {
			t.Fatalf("%d: %v", n, err)
		}
		if got != n {
			t.Fatalf("%d: got %d", n, got)
		}
	}
}

func TestRemainingLengthMalformed(t *testing.T) {
	_, err := ReadRemainingLength(bufio.NewReader(bytes.NewReader([]byte{0x80, 0x80, 0x80, 0x80, 0x00})))
	if !errors.Is(err, ErrMalformedPacket) {
		t.Fatalf("err = %v", err)
	}
}

func TestPacketRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	body := AppendString(nil, "sensors/a")
	if err := WritePacket(&buf, 0x30, body); err != nil {
		t.Fatal(err)
	}
	header, got, err := ReadPacket(bufio.NewReader(&buf), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if header != 0x30 || !bytes.Equal(got, body) {
		t.Fatalf("header=%x body=%x", header, got)
	}
}

func TestPacketTooLarge(t *testing.T) {
	var buf bytes.Buffer
	if err := WritePacket(&buf, 0x30, bytes.Repeat([]byte{0}, 10)); err != nil {
		t.Fatal(err)
	}
	_, _, err := ReadPacket(bufio.NewReader(&buf), 5)
	if !errors.Is(err, ErrMalformedPacket) {
		t.Fatalf("err = %v", err)
	}
}

func TestStringAndBytes(t *testing.T) {
	b := AppendString(nil, "MQTT")
	s, rest, err := ReadString(b)
	if err != nil || s != "MQTT" || len(rest) != 0 {
		t.Fatalf("s=%q rest=%x err=%v", s, rest, err)
	}
	if _, _, err := ReadString([]byte{0, 5, 'a'}); !errors.Is(err, ErrMalformedPacket) {
		t.Fatalf("short string err = %v", err)
	}

	blob := []byte{0, 3, 1, 2, 3}
	got, rest, err := ReadBytes(blob)
	if err != nil || !bytes.Equal(got, []byte{1, 2, 3}) || len(rest) != 0 {
		t.Fatalf("blob=%x rest=%x err=%v", got, rest, err)
	}
}

func TestVariableByteInt(t *testing.T) {
	for _, n := range []int{0, 127, 128, 16384} {
		enc := AppendRemainingLength(nil, n)
		got, rest, err := ReadVariableByteInt(enc)
		if err != nil || got != n || len(rest) != 0 {
			t.Fatalf("%d: got=%d rest=%x err=%v", n, got, rest, err)
		}
	}
	if _, _, err := ReadVariableByteInt([]byte{0x80, 0x80, 0x80, 0x80, 0x00}); !errors.Is(err, ErrMalformedPacket) {
		t.Fatalf("malformed err = %v", err)
	}
}

func TestSkipProperties(t *testing.T) {
	// length 2, two property bytes, then trailing payload.
	b := []byte{2, 0x11, 0x22, 0xAA}
	rest, err := SkipProperties(b)
	if err != nil || !bytes.Equal(rest, []byte{0xAA}) {
		t.Fatalf("rest=%x err=%v", rest, err)
	}
	if _, err := SkipProperties([]byte{3, 0x11}); !errors.Is(err, ErrMalformedPacket) {
		t.Fatalf("short props err = %v", err)
	}
}
