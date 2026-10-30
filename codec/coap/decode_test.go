package coap

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

func TestGoldenFixtures(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
		want Message
	}{
		{
			// RFC 7252 section 1.2: CON GET /test with token 0x71.
			name: "rfc7252/get-uri-path",
			raw:  []byte{0x41, 0x01, 0x7d, 0x34, 0x71, 0xb4, 't', 'e', 's', 't'},
			want: Message{
				Version:   Version,
				Type:      Confirmable,
				Code:      CodeGET,
				MessageID: 0x7d34,
				Token:     []byte{0x71},
				Options:   []Option{{Number: OptionURIPath, Value: []byte("test")}},
			},
		},
		{
			// ACK 2.05 Content with a JSON Content-Format and a payload.
			name: "content-format-and-payload",
			raw:  []byte{0x64, 0x45, 0x7d, 0x35, 't', 'o', 'k', 'n', 0xc1, 0x32, 0xff, 'h', 'i'},
			want: Message{
				Version:   Version,
				Type:      Acknowledgement,
				Code:      CodeContent,
				MessageID: 0x7d35,
				Token:     []byte("tokn"),
				Options:   []Option{{Number: OptionContentFormat, Value: []byte{0x32}}},
				Payload:   []byte("hi"),
			},
		},
		{
			// ACK 2.05 Content carrying a Block2 option (number 2, M set, SZX 2)
			// and a two-byte body.
			name: "block2-response",
			raw:  []byte{0x60, 0x45, 0x12, 0x34, 0xc0, 0xb3, 0x00, 0x00, 0x2a, 0xff, 0x01, 0x02},
			want: Message{
				Version:   Version,
				Type:      Acknowledgement,
				Code:      CodeContent,
				MessageID: 0x1234,
				Options: []Option{
					{Number: OptionContentFormat},
					{Number: OptionBlock2, Value: []byte{0x00, 0x00, 0x2a}},
				},
				Payload: []byte{0x01, 0x02},
			},
		},
		{
			// CON 2.05 notification with Observe sequence number 16.
			name: "observe-notification",
			raw:  []byte{0x42, 0x45, 0x00, 0x01, 0x12, 0x34, 0x62, 0x00, 0x10, 0xff, 0x7b},
			want: Message{
				Version:   Version,
				Type:      Confirmable,
				Code:      CodeContent,
				MessageID: 1,
				Token:     []byte{0x12, 0x34},
				Options:   []Option{{Number: OptionObserve, Value: []byte{0x00, 0x10}}},
				Payload:   []byte{0x7b},
			},
		},
		{
			name: "empty-ack",
			raw:  []byte{0x60, 0x00, 0x12, 0x34},
			want: Message{Version: Version, Type: Acknowledgement, Code: CodeEmpty, MessageID: 0x1234},
		},
		{
			name: "empty-rst",
			raw:  []byte{0x70, 0x00, 0x12, 0x34},
			want: Message{Version: Version, Type: Reset, Code: CodeEmpty, MessageID: 0x1234},
		},
		{
			name: "empty-con-ping",
			raw:  []byte{0x40, 0x00, 0x12, 0x34},
			want: Message{Version: Version, Type: Confirmable, Code: CodeEmpty, MessageID: 0x1234},
		},
		{
			name: "non-confirmable-request",
			raw:  []byte{0x50, 0x02, 0x00, 0x01},
			want: Message{Version: Version, Type: NonConfirmable, Code: CodePOST, MessageID: 1},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mustParse(t, c.raw)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Parse = %+v, want %+v", got, c.want)
			}
			raw, err := got.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(raw, c.raw) {
				t.Fatalf("Marshal = % x, want % x", raw, c.raw)
			}
		})
	}
}

func TestExtendedOptionGolden(t *testing.T) {
	// Option number 300 with a 20-byte value: delta 300 needs the 14 nibble
	// with extension 31, length 20 needs the 13 nibble with extension 7.
	raw := []byte{0x40, 0x02, 0x00, 0x01, 0xed, 0x00, 0x1f, 0x07}
	raw = append(raw, bytes.Repeat([]byte{'x'}, 20)...)
	msg := mustParse(t, raw)
	if len(msg.Options) != 1 || msg.Options[0].Number != 300 || len(msg.Options[0].Value) != 20 {
		t.Fatalf("options = %+v", msg.Options)
	}
	if got := mustMarshal(t, msg); !bytes.Equal(got, raw) {
		t.Fatalf("Marshal = % x, want % x", got, raw)
	}
}

func TestRoundTripAllMessageTypes(t *testing.T) {
	types := []Type{Confirmable, NonConfirmable, Acknowledgement, Reset}
	for _, messageType := range types {
		for tokenLen := 0; tokenLen <= 8; tokenLen++ {
			token := make([]byte, tokenLen)
			for i := range token {
				token[i] = byte(i*7 + tokenLen)
			}
			msg := Message{
				Version:   Version,
				Type:      messageType,
				Code:      CodeContent,
				MessageID: uint16(int(messageType)<<8 | tokenLen),
				Payload:   []byte("body"),
			}
			if tokenLen > 0 {
				msg.Token = token
			}
			mustSet(t, msg.SetURIPath("a", "b"))
			mustSet(t, msg.SetURIQuery("x=1"))
			msg.SetContentFormat(ApplicationLinkFormat)
			raw := mustMarshal(t, msg)
			got := mustParse(t, raw)
			if !reflect.DeepEqual(got, msg) {
				t.Fatalf("type %s token %d: Parse = %+v, want %+v", messageType, tokenLen, got, msg)
			}
		}
	}
}

func TestRoundTripEmptyMessages(t *testing.T) {
	for _, messageType := range []Type{Confirmable, NonConfirmable, Acknowledgement, Reset} {
		msg := Message{Version: Version, Type: messageType, Code: CodeEmpty, MessageID: 0xabcd}
		raw := mustMarshal(t, msg)
		if len(raw) != 4 {
			t.Fatalf("%s empty message is % x", messageType, raw)
		}
		got := mustParse(t, raw)
		if !reflect.DeepEqual(got, msg) {
			t.Fatalf("%s: Parse = %+v, want %+v", messageType, got, msg)
		}
	}
}

func TestTokenLengthBounds(t *testing.T) {
	for tokenLen := 0; tokenLen <= 8; tokenLen++ {
		msg := Message{Version: Version, Type: Confirmable, Code: CodeGET, Token: make([]byte, tokenLen)}
		which := mustParse(t, mustMarshal(t, msg))
		if len(which.Token) != tokenLen {
			t.Fatalf("token length %d round-tripped as %d", tokenLen, len(which.Token))
		}
	}
	if _, err := (Message{Token: make([]byte, 9)}).Marshal(); err == nil {
		t.Fatal("Marshal accepted a 9-byte token")
	} else {
		assertKind(t, err, wire.KindMalformed)
		if !errors.Is(err, ErrMalformed) {
			t.Fatalf("token error does not wrap ErrMalformed: %v", err)
		}
	}
	for tokenLen := 9; tokenLen <= 15; tokenLen++ {
		raw := []byte{0x40 | byte(tokenLen), 0x01, 0x00, 0x00}
		raw = append(raw, make([]byte, tokenLen)...)
		if _, err := Parse(raw); err == nil {
			t.Fatalf("Parse accepted the reserved token length %d", tokenLen)
		} else {
			assertKind(t, err, wire.KindMalformed)
		}
	}
	// A token length inside the datagram but larger than the bytes it carries.
	if _, err := Parse([]byte{0x48, 0x01, 0x00, 0x00, 0x01}); err == nil {
		t.Fatal("Parse accepted a truncated token")
	} else {
		assertKind(t, err, wire.KindMalformed)
	}
}

func TestVersionValidation(t *testing.T) {
	for _, first := range []byte{0x00, 0x80, 0xc0} {
		raw := []byte{first, 0x01, 0x00, 0x00}
		if _, err := Parse(raw); err == nil {
			t.Fatalf("Parse accepted version %d", first>>6)
		} else {
			assertKind(t, err, wire.KindUnsupported)
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("version error does not wrap ErrMalformed: %v", err)
			}
		}
	}
	if _, err := (Message{Version: 2}).Marshal(); err == nil {
		t.Fatal("Marshal accepted version 2")
	} else {
		assertKind(t, err, wire.KindUnsupported)
	}
}

func TestReservedOptionNibbles(t *testing.T) {
	cases := map[string][]byte{
		"delta-15":       {0x40, 0x01, 0x00, 0x00, 0xf0},
		"delta-15-value": {0x40, 0x01, 0x00, 0x00, 0xf1, 0x00},
		"length-15":      {0x40, 0x01, 0x00, 0x00, 0x0f},
		"length-15-path": {0x40, 0x01, 0x00, 0x00, 0xbf, 'a'},
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(raw); err == nil {
				t.Fatalf("Parse(% x) accepted a reserved nibble", raw)
			} else {
				assertKind(t, err, wire.KindMalformed)
			}
		})
	}
}

func TestPayloadMarker(t *testing.T) {
	if _, err := Parse([]byte{0x40, 0x01, 0x00, 0x00, 0xff}); err == nil {
		t.Fatal("Parse accepted a payload marker without a payload")
	} else {
		assertKind(t, err, wire.KindMalformed)
	}
	msg := mustParse(t, []byte{0x40, 0x01, 0x00, 0x00, 0xff, 0x00})
	if !bytes.Equal(msg.Payload, []byte{0x00}) {
		t.Fatalf("payload = % x", msg.Payload)
	}
}

func TestTruncatedDatagrams(t *testing.T) {
	cases := map[string][]byte{
		"empty":                  {},
		"one-byte":               {0x40},
		"two-bytes":              {0x40, 0x01},
		"three-bytes":            {0x40, 0x01, 0x00},
		"token-cut":              {0x48, 0x01, 0x00, 0x00, 0x01},
		"option-delta-ext-cut":   {0x40, 0x01, 0x00, 0x00, 0xd0},
		"option-length-ext-cut":  {0x40, 0x01, 0x00, 0x00, 0x0d},
		"option-delta-ext2-cut":  {0x40, 0x01, 0x00, 0x00, 0xe0, 0x00},
		"option-length-ext2-cut": {0x40, 0x01, 0x00, 0x00, 0x0e, 0x00},
		"option-value-cut":       {0x40, 0x01, 0x00, 0x00, 0xb5, 'a', 'b'},
		"payload-marker-only":    {0x40, 0x01, 0x00, 0x00, 0xff},
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			msg, err := Parse(raw)
			if err == nil {
				t.Fatalf("Parse(% x) accepted a truncated datagram: %+v", raw, msg)
			}
			assertKind(t, err, wire.KindMalformed)
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("error does not wrap ErrMalformed: %v", err)
			}
		})
	}
}

func TestOptionNumberOverflow(t *testing.T) {
	// The largest extended delta, 65804, is already above the 65535 option
	// number limit; a second option would only add to it.
	raw := []byte{0x40, 0x01, 0x00, 0x00, 0xe0, 0xff, 0xff}
	if _, err := Parse(raw); err == nil {
		t.Fatal("Parse accepted an option number above 65535")
	} else {
		assertKind(t, err, wire.KindMalformed)
	}
}

func TestParseCopiesBuffers(t *testing.T) {
	raw := []byte{0x41, 0x01, 0x00, 0x01, 0xaa, 0xb2, 'a', 'b', 0xff, 'x'}
	msg := mustParse(t, raw)
	for i := range raw {
		raw[i] = 0
	}
	if !bytes.Equal(msg.Token, []byte{0xaa}) {
		t.Fatalf("token aliases the datagram: % x", msg.Token)
	}
	if len(msg.Options) != 1 || !bytes.Equal(msg.Options[0].Value, []byte("ab")) {
		t.Fatalf("option aliases the datagram: %v", msg.Options)
	}
	if !bytes.Equal(msg.Payload, []byte("x")) {
		t.Fatalf("payload aliases the datagram: % x", msg.Payload)
	}
}

func TestParseLimit(t *testing.T) {
	msg := Message{Version: Version, Type: NonConfirmable, Code: CodeContent, Payload: bytes.Repeat([]byte{'x'}, DefaultMaxDatagram)}
	raw := mustMarshal(t, msg)
	if len(raw) <= DefaultMaxDatagram {
		t.Fatalf("datagram of %d bytes does not exceed the default limit", len(raw))
	}
	if _, err := Parse(raw); err != nil {
		t.Fatalf("Parse rejected a datagram it has no limit for: %v", err)
	}
	for name, decode := range map[string]func() (Message, error){
		"ParseLimit-default": func() (Message, error) { return ParseLimit(raw, 0) },
		"Decoder-default":    func() (Message, error) { return Decoder{}.Decode(raw) },
		"Codec-default": func() (Message, error) {
			v, err := Codec{}.Decode(raw)
			if err != nil {
				return Message{}, err
			}
			return v.(Message), nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decode()
			if err == nil {
				t.Fatal("decoder accepted an oversized datagram")
			}
			assertKind(t, err, wire.KindTooLarge)
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("size error does not wrap ErrMalformed: %v", err)
			}
		})
	}
	if _, err := ParseLimit(raw, len(raw)); err != nil {
		t.Fatalf("ParseLimit rejected a datagram of exactly the limit: %v", err)
	}
	if _, err := ParseLimit(raw, len(raw)-1); err == nil {
		t.Fatal("ParseLimit accepted a datagram one byte over the limit")
	}
	if _, err := (Decoder{MaxSize: 3}).Decode([]byte{0x40, 0x00, 0x00, 0x01}); err == nil {
		t.Fatal("Decoder accepted a datagram over MaxSize")
	} else {
		assertKind(t, err, wire.KindTooLarge)
	}
}

func TestCodeClassification(t *testing.T) {
	cases := []struct {
		code     Code
		class    uint8
		detail   uint8
		text     string
		request  bool
		response bool
	}{
		{CodeEmpty, 0, 0, "0.00", false, false},
		{CodeGET, 0, 1, "0.01", true, false},
		{CodePOST, 0, 2, "0.02", true, false},
		{CodePUT, 0, 3, "0.03", true, false},
		{CodeDELETE, 0, 4, "0.04", true, false},
		{CodeFETCH, 0, 5, "0.05", true, false},
		{CodePATCH, 0, 6, "0.06", true, false},
		{CodeCreated, 2, 1, "2.01", false, true},
		{CodeDeleted, 2, 2, "2.02", false, true},
		{CodeValid, 2, 3, "2.03", false, true},
		{CodeChanged, 2, 4, "2.04", false, true},
		{CodeContent, 2, 5, "2.05", false, true},
		{CodeContinue, 2, 31, "2.31", false, true},
		{CodeBadRequest, 4, 0, "4.00", false, true},
		{CodeUnauthorized, 4, 1, "4.01", false, true},
		{CodeNotFound, 4, 4, "4.04", false, true},
		{CodeMethodNotAllowed, 4, 5, "4.05", false, true},
		{CodePreconditionFailed, 4, 12, "4.12", false, true},
		{CodeRequestEntityTooLarge, 4, 13, "4.13", false, true},
		{CodeUnsupportedContentFormat, 4, 15, "4.15", false, true},
		{CodeInternalServerError, 5, 0, "5.00", false, true},
		{CodeNotImplemented, 5, 1, "5.01", false, true},
		{CodeServiceUnavailable, 5, 3, "5.03", false, true},
		{Code(0x60), 3, 0, "3.00", false, false},
	}
	for _, c := range cases {
		if c.code.Class() != c.class || c.code.Detail() != c.detail {
			t.Fatalf("%s: class/detail = %d/%d", c.text, c.code.Class(), c.code.Detail())
		}
		if c.code.String() != c.text {
			t.Fatalf("Code.String() = %q, want %q", c.code.String(), c.text)
		}
		if c.code.IsRequest() != c.request {
			t.Fatalf("%s: IsRequest() = %v", c.text, c.code.IsRequest())
		}
		if c.code.IsResponse() != c.response {
			t.Fatalf("%s: IsResponse() = %v", c.text, c.code.IsResponse())
		}
	}
	if CodeCreated != 65 || CodeContent != 69 || CodePreconditionFailed != 140 ||
		CodeRequestEntityTooLarge != 141 || CodeUnsupportedContentFormat != 143 ||
		CodeServiceUnavailable != 163 {
		t.Fatal("a named response code has the wrong numeric value")
	}
	if CodeFETCH != 5 || CodePATCH != 6 {
		t.Fatal("a named method code has the wrong numeric value")
	}
}

func TestTypeString(t *testing.T) {
	cases := map[Type]string{
		Confirmable:     "CON",
		NonConfirmable:  "NON",
		Acknowledgement: "ACK",
		Reset:           "RST",
		Type(4):         "unknown",
	}
	for messageType, want := range cases {
		if got := messageType.String(); got != want {
			t.Fatalf("Type(%d).String() = %q, want %q", messageType, got, want)
		}
	}
}

// assertKind fails the test unless the error carries the expected wire kind.
func assertKind(t *testing.T, err error, want wire.Kind) {
	t.Helper()
	if got := wire.KindOf(err); got != want {
		t.Fatalf("wire.KindOf(%v) = %s, want %s", err, got, want)
	}
}
