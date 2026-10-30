package mqtt

import (
	"bufio"
	"bytes"
	"testing"
)

// TestDecodeEmptyBodyDisconnect is a regression test: an MQTT 5 DISCONNECT with
// remaining length 0 has no reason code and must not read past its own frame.
// The trailing PINGREQ proves the decoder stopped at the boundary; before the
// budget fix this blocked waiting for bytes that belong to the next packet.
func TestDecodeEmptyBodyDisconnect(t *testing.T) {
	stream := []byte{
		0xe0, 0x00, // DISCONNECT, remaining length 0
		0xc0, 0x00, // PINGREQ, remaining length 0
	}
	d := &Decoder{Version: Version5}
	first, err := d.Decode(bufio.NewReader(bytes.NewReader(stream)))
	if err != nil {
		t.Fatalf("decode DISCONNECT: %v", err)
	}
	if _, ok := first.(*Disconnect); !ok {
		t.Fatalf("decoded %T, want *Disconnect", first)
	}
	second, err := d.Decode(bufio.NewReader(bytes.NewReader(stream[2:])))
	if err != nil {
		t.Fatalf("decode PINGREQ: %v", err)
	}
	if _, ok := second.(*Pingreq); !ok {
		t.Fatalf("decoded %T, want *Pingreq", second)
	}
}

// TestDecodeDisconnectCarriesBody pins the other side: a DISCONNECT that does
// carry a reason code still decodes it.
func TestDecodeDisconnectCarriesBody(t *testing.T) {
	d := &Decoder{Version: Version5}
	m, err := d.Decode(bufio.NewReader(bytes.NewReader([]byte{0xe0, 0x01, 0x8e})))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	disc, ok := m.(*Disconnect)
	if !ok {
		t.Fatalf("decoded %T", m)
	}
	if disc.ReasonCode != 0x8e {
		t.Fatalf("reason code = %#x, want 0x8e", disc.ReasonCode)
	}
}
