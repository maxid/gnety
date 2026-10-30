package coap

import (
	"bytes"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

// datagramCodec mirrors gnety's DatagramCodec shape, so the adapter is checked
// here without importing the root package.
type datagramCodec interface {
	Decode(data []byte) (any, error)
	Encode(msg any) ([]byte, error)
}

var _ datagramCodec = Codec{}

func TestEncodeMatchesMarshal(t *testing.T) {
	msg := typedMessage(t)
	strict, err := Encode(msg)
	if err != nil {
		t.Fatal(err)
	}
	if permissive := mustMarshal(t, msg); !bytes.Equal(strict, permissive) {
		t.Fatalf("Encode = % x, Marshal = % x", strict, permissive)
	}
}

func TestEncodeRejectsUnsortedOptions(t *testing.T) {
	msg := Message{
		Version: Version,
		Type:    Confirmable,
		Code:    CodeGET,
		Options: []Option{
			{Number: OptionBlock2, Value: []byte{0x00, 0x00, 0x00}},
			{Number: OptionObserve, Value: []byte{0x01}},
		},
	}
	if _, err := Encode(msg); err == nil {
		t.Fatal("Encode accepted unsorted options")
	} else {
		assertKind(t, err, wire.KindMalformed)
	}
	// Marshal still sorts, which is the behavior gcoap depends on.
	raw := mustMarshal(t, msg)
	got := mustParse(t, raw)
	if len(got.Options) != 2 || got.Options[0].Number != OptionObserve || got.Options[1].Number != OptionBlock2 {
		t.Fatalf("Marshal did not sort the options: %+v", got.Options)
	}
}

func TestEncodeRejectsRepeatedNonRepeatableOption(t *testing.T) {
	nonRepeatable := []uint16{
		OptionIfNoneMatch, OptionObserve, OptionContentFormat, OptionMaxAge,
		OptionAccept, OptionBlock2, OptionBlock1, OptionSize2, OptionSize1,
	}
	for _, number := range nonRepeatable {
		msg := Message{Version: Version, Code: CodeGET, Options: []Option{
			{Number: number},
			{Number: number},
		}}
		if _, err := Encode(msg); err == nil {
			t.Fatalf("Encode accepted a repeated option %d", number)
		} else if wire.KindOf(err) != wire.KindMalformed {
			t.Fatalf("repeated option %d kind = %s", number, wire.KindOf(err))
		}
	}
	repeatable := []uint16{
		OptionIfMatch, OptionETag, OptionLocationPath, OptionURIPath,
		OptionURIQuery, OptionLocationQuery,
	}
	for _, number := range repeatable {
		msg := Message{Version: Version, Code: CodeGET, Options: []Option{
			{Number: number},
			{Number: number},
		}}
		if _, err := Encode(msg); err != nil {
			t.Fatalf("Encode rejected a repeated repeatable option %d: %v", number, err)
		}
	}
}

func TestEncodeRejectsOversizedOptionValue(t *testing.T) {
	cases := map[string]Option{
		"content-format": {Number: OptionContentFormat, Value: []byte{1, 2, 3}},
		"max-age":        {Number: OptionMaxAge, Value: make([]byte, 5)},
		"uri-path":       {Number: OptionURIPath, Value: bytes.Repeat([]byte{'x'}, 256)},
		"observe":        {Number: OptionObserve, Value: make([]byte, 4)},
		"block1":         {Number: OptionBlock1, Value: make([]byte, 4)},
		"size1":          {Number: OptionSize1, Value: make([]byte, 5)},
		"if-none-match":  {Number: OptionIfNoneMatch, Value: []byte{0}},
	}
	for name, option := range cases {
		t.Run(name, func(t *testing.T) {
			msg := Message{Version: Version, Code: CodeGET, Options: []Option{option}}
			if _, err := Encode(msg); err == nil {
				t.Fatalf("Encode accepted %d value bytes for option %d", len(option.Value), option.Number)
			} else {
				assertKind(t, err, wire.KindMalformed)
			}
		})
	}
	unknown := Message{Version: Version, Code: CodeGET, Options: []Option{
		{Number: 2048, Value: bytes.Repeat([]byte{'x'}, 1000)},
	}}
	if _, err := Encode(unknown); err != nil {
		t.Fatalf("Encode rejected an unknown option with a long value: %v", err)
	}
}

func TestEncodeRejectsBadTypeAndToken(t *testing.T) {
	if _, err := Encode(Message{Version: Version, Type: Type(4)}); err == nil {
		t.Fatal("Encode accepted the message type 4")
	} else {
		assertKind(t, err, wire.KindMalformed)
	}
	if _, err := Encode(Message{Version: Version, Token: make([]byte, 9)}); err == nil {
		t.Fatal("Encode accepted a 9-byte token")
	} else {
		assertKind(t, err, wire.KindMalformed)
	}
	if _, err := Encode(Message{Version: 2}); err == nil {
		t.Fatal("Encode accepted version 2")
	} else {
		assertKind(t, err, wire.KindUnsupported)
	}
	if _, err := Encode(Message{}); err != nil {
		t.Fatalf("Encode rejected a message without a version: %v", err)
	}
}

func TestEncodeDuplicateOptionNumbers(t *testing.T) {
	// Duplicates of an unknown option are allowed: repeatability of a number
	// the codec does not know is the extension's business.
	msg := Message{Version: Version, Code: CodeGET, Options: []Option{
		{Number: 300, Value: []byte{1}},
		{Number: 300, Value: []byte{2}},
	}}
	raw, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode rejected a repeated unknown option: %v", err)
	}
	got := mustParse(t, raw)
	if len(got.Options) != 2 {
		t.Fatalf("options = %+v", got.Options)
	}
}

func TestCodecAdapter(t *testing.T) {
	var c Codec
	raw, err := c.Encode(Message{Version: Version, Type: NonConfirmable, Code: CodeGET, MessageID: 3})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := c.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	msg, ok := decoded.(Message)
	if !ok {
		t.Fatalf("Decode returned %T", decoded)
	}
	if msg.MessageID != 3 || msg.Code != CodeGET {
		t.Fatalf("decoded message = %+v", msg)
	}
	if _, err := c.Encode(&msg); err != nil {
		t.Fatalf("Encode rejected a *Message: %v", err)
	}
	var nilMessage *Message
	if _, err := c.Encode(nilMessage); err == nil {
		t.Fatal("Encode accepted a nil *Message")
	} else {
		assertKind(t, err, wire.KindMalformed)
	}
	if _, err := c.Encode("not a message"); err == nil {
		t.Fatal("Encode accepted an unrelated type")
	} else {
		assertKind(t, err, wire.KindUnsupported)
	}
	if _, err := (Codec{MaxSize: 2}).Decode(raw); err == nil {
		t.Fatal("Decode accepted a datagram over MaxSize")
	} else {
		assertKind(t, err, wire.KindTooLarge)
	}
}

func TestEncodeRepeatedBuildsSameBytes(t *testing.T) {
	// Encoding must not mutate the message, so two Encodes agree and Options
	// stay in the caller's order.
	msg := typedMessage(t)
	options := append([]Option(nil), msg.Options...)
	first, err := Encode(msg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encode(msg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("Encode is not deterministic: % x then % x", first, second)
	}
	if len(msg.Options) != len(options) {
		t.Fatalf("Encode changed the options: %v", msg.Options)
	}
	for i := range options {
		if msg.Options[i].Number != options[i].Number || !bytes.Equal(msg.Options[i].Value, options[i].Value) {
			t.Fatalf("Encode changed option %d: %v", i, msg.Options)
		}
	}
}
