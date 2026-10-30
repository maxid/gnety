package modbus

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

func TestRoundTrip(t *testing.T) {
	f := Frame{TransactionID: 0x1234, UnitID: 0x11, PDU: []byte{FuncReadHoldingRegisters, 0x00, 0x10, 0x00, 0x02}}
	buf, err := f.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if len(buf) != MBAPLen+len(f.PDU) {
		t.Fatalf("encoded %d bytes, want %d", len(buf), MBAPLen+len(f.PDU))
	}
	if got := binary.BigEndian.Uint16(buf[4:6]); got != uint16(1+len(f.PDU)) {
		t.Fatalf("length field = %d, want %d", got, 1+len(f.PDU))
	}

	got, err := Read(bufio.NewReader(bytes.NewReader(buf)), 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.TransactionID != f.TransactionID || got.UnitID != f.UnitID || !bytes.Equal(got.PDU, f.PDU) {
		t.Fatalf("round trip = %+v, want %+v", got, f)
	}
	if got.Function() != FuncReadHoldingRegisters {
		t.Fatalf("Function = %#x", got.Function())
	}
}

func TestTwoFramesBackToBack(t *testing.T) {
	a, _ := Frame{TransactionID: 1, UnitID: 1, PDU: []byte{0x03, 0x00}}.Marshal()
	b, _ := Frame{TransactionID: 2, UnitID: 2, PDU: []byte{0x04, 0x00}}.Marshal()
	r := bufio.NewReader(bytes.NewReader(append(a, b...)))

	first, err := Read(r, 0)
	if err != nil || first.TransactionID != 1 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	second, err := Read(r, 0)
	if err != nil || second.TransactionID != 2 {
		t.Fatalf("second = %+v, %v", second, err)
	}
}

func TestReadClassifiesErrors(t *testing.T) {
	header := func(proto, length uint16) []byte {
		b := make([]byte, 6)
		binary.BigEndian.PutUint16(b[2:4], proto)
		binary.BigEndian.PutUint16(b[4:6], length)
		return b
	}

	cases := []struct {
		name string
		in   []byte
		want wire.Kind
	}{
		{"truncated header", []byte{0x00, 0x01}, wire.KindIncomplete},
		{"bad protocol id", append(header(1, 2), 0x01, 0x03), wire.KindMalformed},
		{"length below minimum", append(header(0, 1), 0x01, 0x03), wire.KindMalformed},
		{"length above max", append(header(0, 255), 0x01), wire.KindTooLarge},
		{"truncated pdu", append(header(0, 3), 0x01, 0x03), wire.KindIncomplete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Read(bufio.NewReader(bytes.NewReader(tc.in)), 0)
			if got := wire.KindOf(err); got != tc.want {
				t.Fatalf("kind = %v, want %v (err %v)", got, tc.want, err)
			}
		})
	}
}

func TestMarshalRejectsBadPDU(t *testing.T) {
	if _, err := (Frame{}).Marshal(); wire.KindOf(err) != wire.KindMalformed {
		t.Fatalf("empty PDU: kind = %v, want malformed", wire.KindOf(err))
	}
	if _, err := (Frame{PDU: bytes.Repeat([]byte{0}, MaxPDU+1)}).Marshal(); wire.KindOf(err) != wire.KindTooLarge {
		t.Fatalf("oversized PDU: kind = %v, want too large", wire.KindOf(err))
	}
}

func TestCodecRoundTripAndLimits(t *testing.T) {
	var buf bytes.Buffer
	c := Codec{}
	f := Frame{TransactionID: 7, UnitID: 3, PDU: []byte{FuncWriteSingleRegister, 0x00, 0x01, 0x00, 0x0a}}
	if err := c.Encode(&buf, &f); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := c.Decode(bufio.NewReader(&buf))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if frame, ok := got.(Frame); !ok || frame.TransactionID != 7 {
		t.Fatalf("Decode = %#v", got)
	}

	strict := Codec{MaxPDU: 2}
	if err := strict.Encode(&bytes.Buffer{}, f); wire.KindOf(err) != wire.KindTooLarge {
		t.Fatalf("MaxPDU encode: kind = %v, want too large", wire.KindOf(err))
	}
	if err := c.Encode(&bytes.Buffer{}, "nope"); wire.KindOf(err) != wire.KindUnsupported {
		t.Fatalf("wrong type: kind = %v, want unsupported", wire.KindOf(err))
	}
}

func TestExceptionFlag(t *testing.T) {
	if !IsException(FuncReadHoldingRegisters | 0x80) {
		t.Fatal("0x83 must read as an exception")
	}
	if IsException(FuncReadHoldingRegisters) {
		t.Fatal("0x03 must not read as an exception")
	}
}

func TestFrameStringHidesPayload(t *testing.T) {
	f := Frame{TransactionID: 1, UnitID: 1, PDU: []byte{0x03, 0xde, 0xad}}
	if got := f.String(); bytes.Contains([]byte(got), []byte{0xde}) {
		t.Fatalf("String leaks payload: %q", got)
	}
}

func FuzzRead(f *testing.F) {
	f.Add([]byte{0, 1, 0, 0, 0, 2, 1, 3})
	f.Add([]byte{0xff, 0xff, 0, 0, 0xff, 0xff, 1, 3})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Read(bufio.NewReader(bytes.NewReader(data)), 0)
	})
}
