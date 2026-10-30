package coap

import (
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

// kindOf names the codec/wire kind of an error for failure messages.
func kindOf(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return "none"
	}
	return wire.KindOf(err).String()
}

// mustMarshal encodes a message and fails on error.
func mustMarshal(t *testing.T, msg Message) []byte {
	t.Helper()
	raw, err := msg.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	return raw
}

// mustParse decodes a datagram and fails on error.
func mustParse(t *testing.T, raw []byte) Message {
	t.Helper()
	msg, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse(% x) failed: %v", raw, err)
	}
	return msg
}
