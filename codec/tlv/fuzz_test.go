package tlv

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

// FuzzUnmarshal checks that no payload, however malformed, makes the decoder
// panic, that every rejection is classified, and that anything it accepts
// survives Marshal and a second decode unchanged — the other half of the wire
// format promise.
func FuzzUnmarshal(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0xc0, 0x00})
	f.Add([]byte{0xc1, 0x01, 'A'})
	f.Add([]byte{0xe4, 0x16, 0x44, '2', '1', '.', '5'})
	f.Add([]byte{0x86, 0x06, 0x41, 0x00, 0x01, 0x41, 0x01, 0x02})
	f.Add([]byte{0x08, 0x00, 0x0f, 0x86, 0x06, 0x41, 0x00, 0x01, 0x41, 0x01, 0x02,
		0xe4, 0x16, 0x44, '2', '1', '.', '5'})
	f.Add([]byte{0xf8, 0xff, 0xff, 0x00, 0x00, 0x03, 'A', 'B', 'C'})
	f.Add([]byte{0xff})
	f.Add([]byte{0xc1, 0x01, 'A', 0xc1, 0x01, 'B'})
	f.Add([]byte{0xd8, 0x01, 0x00, 0x00, 0x02, 'A'})
	f.Fuzz(func(t *testing.T, data []byte) {
		entries, err := Unmarshal(data)
		if err != nil {
			switch kind := wire.KindOf(err); kind {
			case wire.KindMalformed, wire.KindTooLarge:
			default:
				t.Fatalf("Unmarshal(% x) error kind = %s: %v", data, kind, err)
			}
			return
		}
		raw, err := Marshal(entries)
		if err != nil {
			t.Fatalf("Marshal of the entries parsed from % x failed: %v", data, err)
		}
		again, err := Unmarshal(raw)
		if err != nil {
			t.Fatalf("Unmarshal of the re-encoded % x failed: %v", raw, err)
		}
		raw2, err := Marshal(again)
		if err != nil {
			t.Fatalf("second Marshal of % x failed: %v", raw, err)
		}
		if !bytes.Equal(raw, raw2) {
			t.Fatalf("the canonical form is not stable:\nfrom % x: % x\nthen % x", data, raw, raw2)
		}
		third, err := Unmarshal(raw2)
		if err != nil {
			t.Fatalf("Unmarshal of % x failed: %v", raw2, err)
		}
		if !reflect.DeepEqual(again, third) {
			t.Fatalf("the decoded tree is not stable:\n%+v\n%+v", again, third)
		}
	})
}
