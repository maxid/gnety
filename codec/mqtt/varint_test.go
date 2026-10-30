package mqtt

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

// TestRemainingLengthGolden pins the encoding of every boundary value so the
// shared codec/wire varint cannot drift from the MQTT wire format silently.
func TestRemainingLengthGolden(t *testing.T) {
	cases := []struct {
		value int
		bytes []byte
	}{
		{0, []byte{0x00}},
		{127, []byte{0x7f}},
		{128, []byte{0x80, 0x01}},
		{16383, []byte{0xff, 0x7f}},
		{16384, []byte{0x80, 0x80, 0x01}},
		{2097151, []byte{0xff, 0xff, 0x7f}},
		{MaxRemainingLength, []byte{0xff, 0xff, 0xff, 0x7f}},
	}
	for _, tc := range cases {
		if got := AppendRemainingLength(nil, tc.value); !bytes.Equal(got, tc.bytes) {
			t.Fatalf("AppendRemainingLength(%d) = %x, want %x", tc.value, got, tc.bytes)
		}
		got, err := ReadRemainingLength(bufio.NewReader(bytes.NewReader(tc.bytes)))
		if err != nil || got != tc.value {
			t.Fatalf("ReadRemainingLength(%x) = %d, %v; want %d", tc.bytes, got, err, tc.value)
		}
	}
}

func TestRemainingLengthRejectsMalformed(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		kind wire.Kind
	}{
		{"non-minimal encoding", []byte{0x80, 0x00}, wire.KindMalformed},
		{"five bytes", []byte{0x80, 0x80, 0x80, 0x80, 0x01}, wire.KindMalformed},
		{"truncated", []byte{0x80}, wire.KindIncomplete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadRemainingLength(bufio.NewReader(bytes.NewReader(tc.in)))
			if wire.KindOf(err) != tc.kind {
				t.Fatalf("kind = %v, want %v (err %v)", wire.KindOf(err), tc.kind, err)
			}
			if tc.kind == wire.KindMalformed && !errors.Is(err, ErrMalformedPacket) {
				t.Fatalf("malformed input must stay ErrMalformedPacket, got %v", err)
			}
			if tc.kind == wire.KindIncomplete && !errors.Is(err, io.EOF) {
				t.Fatalf("truncated input must keep its io cause, got %v", err)
			}
		})
	}
}
