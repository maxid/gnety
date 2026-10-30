package coap

import (
	"errors"
	"reflect"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

// FuzzParse checks that no datagram, however malformed, makes the decoder panic.
//
// When a datagram is accepted, the message must also survive a re-encode and a
// second decode unchanged, which is the other half of the wire-format promise.
func FuzzParse(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x41, 0x01, 0x7d, 0x34, 0x71, 0xb4, 't', 'e', 's', 't'})
	f.Add([]byte{0x64, 0x45, 0x7d, 0x35, 't', 'o', 'k', 'n', 0xc1, 0x32, 0xff, 'h', 'i'})
	f.Add([]byte{0x60, 0x45, 0x12, 0x34, 0xc0, 0xb3, 0x00, 0x00, 0x2a, 0xff, 0x01, 0x02})
	f.Add([]byte{0x42, 0x45, 0x00, 0x01, 0x12, 0x34, 0x62, 0x00, 0x10, 0xff, 0x7b})
	f.Add([]byte{0x40, 0x01, 0x00, 0x00, 0xff})
	f.Add([]byte{0x40, 0x01, 0x00, 0x00, 0xed, 0x00, 0x1f, 0x07})
	f.Add([]byte{0x4f, 0x01, 0x00, 0x00})
	f.Add([]byte{0xc0, 0x01, 0x00, 0x00})
	f.Add([]byte{0x40, 0x01, 0x00, 0x00, 0xe0, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		msg, err := Parse(data)
		if err != nil {
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("Parse(% x) error does not wrap ErrMalformed: %v", data, err)
			}
			switch wire.KindOf(err) {
			case wire.KindMalformed, wire.KindUnsupported:
			default:
				t.Fatalf("Parse(% x) error kind = %s: %v", data, wire.KindOf(err), err)
			}
			return
		}
		raw, err := msg.Marshal()
		if err != nil {
			t.Fatalf("Marshal of the message parsed from % x failed: %v", data, err)
		}
		again, err := Parse(raw)
		if err != nil {
			t.Fatalf("Parse of the re-encoded message % x failed: %v", raw, err)
		}
		if !reflect.DeepEqual(msg, again) {
			t.Fatalf("round trip changed the message:\nfrom % x: %+v\nreencoded % x: %+v", data, msg, raw, again)
		}
	})
}
