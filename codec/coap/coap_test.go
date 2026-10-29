package coap

import (
	"bytes"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	msg := Message{
		Type:      Confirmable,
		Code:      CodeGET,
		MessageID: 0x1234,
		Token:     []byte{0xAA, 0xBB},
		Options: []Option{
			{Number: OptionURIPath, Value: []byte("sensors")},
			{Number: OptionURIQuery, Value: []byte("a=1")},
		},
		Payload: []byte("payload"),
	}
	raw, err := msg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != msg.Type || got.Code != msg.Code || got.MessageID != msg.MessageID ||
		!bytes.Equal(got.Token, msg.Token) || !bytes.Equal(got.Payload, msg.Payload) ||
		len(got.Options) != 2 || got.Options[0].Number != OptionURIPath {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestExtendedOptions(t *testing.T) {
	msg := Message{
		Code:  CodePOST,
		Token: nil,
		Options: []Option{
			{Number: 300, Value: bytes.Repeat([]byte("x"), 20)},
		},
	}
	raw, err := msg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Options) != 1 || got.Options[0].Number != 300 || len(got.Options[0].Value) != 20 {
		t.Fatalf("options = %+v", got.Options)
	}
}

func TestMalformed(t *testing.T) {
	cases := [][]byte{
		{},
		{0x40, 0x01, 0x00},             // too short
		{0x80, 0x01, 0x00, 0x00},       // version 2
		{0x49, 0x01, 0x00, 0x00},       // token len 9
		{0x40, 0x01, 0x00, 0x00, 0xFF}, // empty payload marker
	}
	for i, c := range cases {
		if _, err := Parse(c); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestCodeString(t *testing.T) {
	if CodeContent.String() != "2.05" || CodeGET.String() != "0.01" {
		t.Fatalf("code strings: %s %s", CodeContent, CodeGET)
	}
}
