package mqtt

import (
	"bufio"
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

// TestDecodeEmptyBodyAuth pins that a zero-length AUTH body is success with no
// properties and that the decoder stops at the frame boundary: the following
// PINGREQ is left untouched.
func TestDecodeEmptyBodyAuth(t *testing.T) {
	stream := []byte{
		0xf0, 0x00, // AUTH, remaining length 0
		0xc0, 0x00, // PINGREQ, remaining length 0
	}
	d := &Decoder{Version: Version5}
	r := bufio.NewReader(bytes.NewReader(stream))
	first, err := d.Decode(r)
	if err != nil {
		t.Fatalf("decode AUTH: %v", err)
	}
	auth, ok := first.(*Auth)
	if !ok {
		t.Fatalf("decoded %T, want *Auth", first)
	}
	if auth.ReasonCode != 0 {
		t.Fatalf("reason code = %#x, want 0x00", auth.ReasonCode)
	}
	if !reflect.DeepEqual(auth.Properties, Properties{}) {
		t.Fatalf("properties = %+v, want the zero value", auth.Properties)
	}
	if auth.Version() != Version5 {
		t.Fatalf("version = %d, want %d", auth.Version(), Version5)
	}
	second, err := d.Decode(r)
	if err != nil {
		t.Fatalf("decode PINGREQ: %v", err)
	}
	if second.Type() != TypePingreq {
		t.Fatalf("type = %d, want %d", second.Type(), TypePingreq)
	}
}

// TestAuthReservedFlags pins that the AUTH fixed header accepts flag 0 and
// nothing else.
func TestAuthReservedFlags(t *testing.T) {
	for flags := byte(0); flags <= 0x0f; flags++ {
		raw := []byte{0xf0 | flags, 0x00}
		_, err := (&Decoder{Version: Version5}).Decode(bufio.NewReader(bytes.NewReader(raw)))
		if flags == 0 {
			if err != nil {
				t.Fatalf("decode % x: %v", raw, err)
			}
			continue
		}
		if wire.KindOf(err) != wire.KindMalformed {
			t.Fatalf("decode % x: kind = %v, want %v (err %v)", raw, wire.KindOf(err), wire.KindMalformed, err)
		}
	}
}

// TestAuthRequiresVersion5 pins that AUTH is refused in both directions when
// the packet belongs to MQTT 3.1.1. A zero Level means 3.1.1 like every other
// packet.
func TestAuthRequiresVersion5(t *testing.T) {
	for _, version := range []byte{0, Version311} {
		_, err := (&Decoder{Version: version}).Decode(bufio.NewReader(bytes.NewReader([]byte{0xf0, 0x00})))
		if wire.KindOf(err) != wire.KindUnsupported {
			t.Fatalf("decode with Decoder.Version %d: kind = %v, want %v (err %v)", version, wire.KindOf(err), wire.KindUnsupported, err)
		}
		err = Encode(&bytes.Buffer{}, &Auth{MessageHeader: MessageHeader{Level: version}})
		if wire.KindOf(err) != wire.KindUnsupported {
			t.Fatalf("encode with level %d: kind = %v, want %v (err %v)", version, wire.KindOf(err), wire.KindUnsupported, err)
		}
		if !errors.Is(err, ErrMalformedPacket) {
			t.Fatalf("encode err = %v, want it to wrap ErrMalformedPacket", err)
		}
	}
}

// TestAuthShortForms pins the body shapes the encoder chooses: both the reason
// code and the property block are omitted at their defaults, so success with no
// properties has no body and a bare reason code stays one byte. A property
// block always brings the reason code with it.
func TestAuthShortForms(t *testing.T) {
	v5 := MessageHeader{Level: Version5}
	cases := []struct {
		name string
		msg  Auth
		body []byte
	}{
		{"empty", Auth{MessageHeader: v5}, nil},
		{"reason only", Auth{MessageHeader: v5, ReasonCode: 0x18}, []byte{0x18}},
		{"empty property block dropped", Auth{MessageHeader: v5, ReasonCode: 0x19, Properties: Properties{}}, []byte{0x19}},
		{
			"default reason kept with properties",
			Auth{MessageHeader: v5, Properties: Properties{AuthenticationData: []byte{0xaa}}},
			[]byte{0x00, 0x04, 0x16, 0x00, 0x01, 0xaa},
		},
		{
			"method and data",
			Auth{
				MessageHeader: v5,
				ReasonCode:    0x18,
				Properties: Properties{
					AuthenticationMethod: "m",
					AuthenticationData:   []byte{0xaa},
				},
			},
			[]byte{0x18, 0x08, 0x15, 0x00, 0x01, 'm', 0x16, 0x00, 0x01, 0xaa},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Encode(&buf, &tc.msg); err != nil {
				t.Fatalf("Encode: %v", err)
			}
			want := []byte{0xf0}
			want = AppendRemainingLength(want, len(tc.body))
			want = append(want, tc.body...)
			if !bytes.Equal(buf.Bytes(), want) {
				t.Fatalf("Encode mismatch\n got: % x\nwant: % x", buf.Bytes(), want)
			}
		})
	}
}

// TestAuthReencodeIsIdentical pins that every AUTH fixture decodes and then
// re-encodes to the same bytes, so the short forms are stable.
func TestAuthReencodeIsIdentical(t *testing.T) {
	found := 0
	for _, tc := range goldenCases() {
		if tc.msg.Type() != TypeAuth {
			continue
		}
		found++
		t.Run(tc.name, func(t *testing.T) {
			got := decodeBytes(t, Version5, tc.wire)
			var buf bytes.Buffer
			if err := Encode(&buf, got); err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if !bytes.Equal(buf.Bytes(), tc.wire) {
				t.Fatalf("re-encode mismatch\n got: % x\nwant: % x", buf.Bytes(), tc.wire)
			}
		})
	}
	if found == 0 {
		t.Fatal("no AUTH golden fixture")
	}
}

// TestAuthPropertyTable pins the per-packet property table: AUTH carries the
// authentication method and data and nothing else, while the same two remain
// legal in CONNECT and CONNACK.
func TestAuthPropertyTable(t *testing.T) {
	props := Properties{AuthenticationMethod: "SCRAM-SHA-1", AuthenticationData: []byte{0x01, 0x02}}
	for _, packetType := range []byte{TypeConnect, TypeConnack, TypeAuth} {
		raw, err := encodeProperties(nil, props, packetType)
		if err != nil {
			t.Fatalf("%s encodeProperties: %v", packetName(packetType), err)
		}
		got, rest, err := decodeProperties(raw, packetType)
		if err != nil {
			t.Fatalf("%s decodeProperties: %v", packetName(packetType), err)
		}
		if len(rest) != 0 {
			t.Fatalf("%s decodeProperties left %d bytes", packetName(packetType), len(rest))
		}
		if !reflect.DeepEqual(got, props) {
			t.Fatalf("%s round trip mismatch\n got: %+v\nwant: %+v", packetName(packetType), got, props)
		}
	}

	// Everything else is refused in AUTH, in both directions.
	illegal := []struct {
		name       string
		properties Properties
	}{
		{"session expiry", Properties{SessionExpiryInterval: 1}},
		{"user property", Properties{UserProperties: []UserProperty{{Key: "k", Value: "v"}}}},
		{"reason string", Properties{ReasonString: "r"}},
	}
	for _, tc := range illegal {
		if _, err := encodeProperties(nil, tc.properties, TypeAuth); !errors.Is(err, ErrMalformedPacket) {
			t.Fatalf("encodeProperties(%s) err = %v, want ErrMalformedPacket", tc.name, err)
		}
	}
}

// TestAuthDecodeErrors covers the AUTH packets the decoder must reject.
func TestAuthDecodeErrors(t *testing.T) {
	cases := []struct {
		name    string
		version byte
		wire    []byte
		kind    wire.Kind
	}{
		{
			name:    "3.1.1 decoder",
			version: Version311,
			wire:    []byte{0xf0, 0x00},
			kind:    wire.KindUnsupported,
		},
		{
			name:    "property illegal in AUTH",
			version: Version5,
			wire:    []byte{0xf0, 0x07, 0x18, 0x05, 0x11, 0x00, 0x00, 0x00, 0x01},
			kind:    wire.KindMalformed,
		},
		{
			name:    "property length past the body",
			version: Version5,
			wire:    []byte{0xf0, 0x03, 0x18, 0x05, 0x15},
			kind:    wire.KindMalformed,
		},
		{
			name:    "truncated property value",
			version: Version5,
			wire:    []byte{0xf0, 0x04, 0x18, 0x02, 0x15, 0x00},
			kind:    wire.KindMalformed,
		},
		{
			name:    "truncated packet",
			version: Version5,
			wire:    []byte{0xf0, 0x04, 0x18, 0x02, 0x15},
			kind:    wire.KindIncomplete,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (&Decoder{Version: tc.version}).Decode(bufio.NewReader(bytes.NewReader(tc.wire)))
			if err == nil {
				t.Fatalf("Decode(% x) succeeded, want %v", tc.wire, tc.kind)
			}
			if got := wire.KindOf(err); got != tc.kind {
				t.Fatalf("kind = %v, want %v (err %v)", got, tc.kind, err)
			}
			if tc.kind != wire.KindIncomplete && !errors.Is(err, ErrMalformedPacket) {
				t.Fatalf("err = %v, want it to wrap ErrMalformedPacket", err)
			}
		})
	}
}

// TestAuthEncodeErrors covers AUTH messages the encoder must refuse.
func TestAuthEncodeErrors(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{
			"property illegal in AUTH",
			&Auth{
				MessageHeader: MessageHeader{Level: Version5},
				Properties:    Properties{SessionExpiryInterval: 1},
			},
		},
		{
			"3.1.1 AUTH",
			&Auth{MessageHeader: MessageHeader{Level: Version311}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := Encode(&buf, tc.msg)
			if err == nil {
				t.Fatalf("Encode(%s) succeeded, want an error", tc.name)
			}
			if buf.Len() != 0 {
				t.Fatalf("Encode wrote %d bytes before failing", buf.Len())
			}
			if !errors.Is(err, ErrMalformedPacket) && wire.KindOf(err) != wire.KindUnsupported {
				t.Fatalf("err = %v, want a classified error", err)
			}
		})
	}
}
