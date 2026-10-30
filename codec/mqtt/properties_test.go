package mqtt

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

// propertiesByContext holds one complete property set per packet context, used
// to prove that every modeled property survives a round trip.
var propertiesByContext = []struct {
	packetType byte
	properties Properties
}{
	{
		TypeConnect,
		Properties{
			SessionExpiryInterval: 120,
			ReceiveMaximum:        10,
			MaximumPacketSize:     65535,
			TopicAliasMaximum:     4,
			AuthenticationMethod:  "m",
			AuthenticationData:    []byte{1},
			RequestProblemInfo:    boolPtr(true),
			RequestResponseInfo:   boolPtr(false),
			UserProperties:        []UserProperty{{Key: "k", Value: "v"}},
		},
	},
	{
		TypeConnack,
		Properties{
			SessionExpiryInterval:           1,
			ReceiveMaximum:                  2,
			MaximumQoS:                      bytePtr(1),
			RetainAvailable:                 boolPtr(false),
			MaximumPacketSize:               3,
			TopicAliasMaximum:               5,
			AssignedClientID:                "id",
			ServerKeepAlive:                 uint16Ptr(30),
			ResponseInfo:                    "ri",
			ServerReference:                 "sr",
			ReasonString:                    "rs",
			AuthenticationMethod:            "am",
			AuthenticationData:              []byte{2},
			WildcardSubscriptionAvailable:   boolPtr(true),
			SubscriptionIdentifierAvailable: boolPtr(false),
			SharedSubscriptionAvailable:     boolPtr(true),
			UserProperties:                  []UserProperty{{Key: "a", Value: "b"}, {Key: "a", Value: "c"}},
		},
	},
	{
		TypePublish,
		Properties{
			PayloadFormat:           bytePtr(1),
			MessageExpiryInterval:   60,
			ContentType:             "ct",
			ResponseTopic:           "rt",
			CorrelationData:         []byte{3, 4},
			SubscriptionIdentifiers: []uint32{1, 300},
			TopicAlias:              7,
			UserProperties:          []UserProperty{{Key: "k", Value: "v"}},
		},
	},
	{
		TypeSubscribe,
		Properties{SubscriptionIdentifiers: []uint32{9}},
	},
	{
		TypeSuback,
		Properties{ReasonString: "rs", UserProperties: []UserProperty{{Key: "k", Value: "v"}}},
	},
	{
		TypeUnsubscribe,
		Properties{UserProperties: []UserProperty{{Key: "k", Value: "v"}}},
	},
	{
		TypeUnsuback,
		Properties{ReasonString: "rs"},
	},
	{
		TypePuback,
		Properties{ReasonString: "rs", UserProperties: []UserProperty{{Key: "k", Value: "v"}}},
	},
	{
		TypeDisconnect,
		Properties{SessionExpiryInterval: 9, ServerReference: "sr", ReasonString: "rs"},
	},
}

// TestPropertiesRoundTrip encodes and decodes a complete property set per
// packet context.
func TestPropertiesRoundTrip(t *testing.T) {
	for _, tc := range propertiesByContext {
		t.Run(packetName(tc.packetType), func(t *testing.T) {
			raw, err := encodeProperties(nil, tc.properties, tc.packetType)
			if err != nil {
				t.Fatalf("encodeProperties: %v", err)
			}
			got, rest, err := decodeProperties(raw, tc.packetType)
			if err != nil {
				t.Fatalf("decodeProperties: %v", err)
			}
			if len(rest) != 0 {
				t.Fatalf("decodeProperties left %d bytes", len(rest))
			}
			if !reflect.DeepEqual(got, tc.properties) {
				t.Fatalf("round trip mismatch\n got: %+v\nwant: %+v", got, tc.properties)
			}
		})
	}
}

// TestPropertiesEmptyBlock pins the zero-length property block and the trailing
// bytes it returns.
func TestPropertiesEmptyBlock(t *testing.T) {
	p, rest, err := decodeProperties([]byte{0x00, 0xaa}, TypePublish)
	if err != nil {
		t.Fatalf("decodeProperties: %v", err)
	}
	if !reflect.DeepEqual(p, Properties{}) {
		t.Fatalf("properties = %+v, want the zero value", p)
	}
	if !bytes.Equal(rest, []byte{0xaa}) {
		t.Fatalf("rest = % x, want aa", rest)
	}
	raw, err := encodeProperties(nil, Properties{}, TypePublish)
	if err != nil {
		t.Fatalf("encodeProperties: %v", err)
	}
	if !bytes.Equal(raw, []byte{0x00}) {
		t.Fatalf("empty block = % x, want 00", raw)
	}
}

// TestPropertiesEncodeOrder pins the canonical ascending identifier order so the
// same Properties value always produces the same bytes.
func TestPropertiesEncodeOrder(t *testing.T) {
	p := Properties{
		ReceiveMaximum:        2,
		SessionExpiryInterval: 1,
		UserProperties:        []UserProperty{{Key: "k", Value: "v"}},
	}
	got, err := encodeProperties(nil, p, TypeConnect)
	if err != nil {
		t.Fatalf("encodeProperties: %v", err)
	}
	want := []byte{
		0x0f,                         // property length 15
		0x11, 0x00, 0x00, 0x00, 0x01, // session expiry 1
		0x21, 0x00, 0x02, // receive maximum 2
		0x26, 0x00, 0x01, 'k', 0x00, 0x01, 'v', // user property k=v
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("encodeProperties = % x, want % x", got, want)
	}
}

// TestPropertiesRejectWrongPacket checks that a property defined for one packet
// type is refused in another, in both directions.
func TestPropertiesRejectWrongPacket(t *testing.T) {
	cases := []struct {
		name       string
		packetType byte
		raw        []byte
	}{
		{"session expiry in PUBLISH", TypePublish, []byte{0x05, 0x11, 0x00, 0x00, 0x00, 0x01}},
		{"receive maximum in SUBACK", TypeSuback, []byte{0x03, 0x21, 0x00, 0x0a}},
		{"topic alias in CONNECT", TypeConnect, []byte{0x03, 0x23, 0x00, 0x01}},
		{"reason string in PUBLISH", TypePublish, []byte{0x03, 0x1f, 0x00, 0x00}},
		{"correlation data in CONNACK", TypeConnack, []byte{0x03, 0x09, 0x00, 0x00}},
		{"will delay in CONNECT", TypeConnect, []byte{0x05, 0x18, 0x00, 0x00, 0x00, 0x01}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := decodeProperties(tc.raw, tc.packetType); !errors.Is(err, ErrMalformedPacket) {
				t.Fatalf("decodeProperties err = %v, want ErrMalformedPacket", err)
			}
		})
	}

	encodeCases := []struct {
		name       string
		packetType byte
		properties Properties
	}{
		{"session expiry in PUBLISH", TypePublish, Properties{SessionExpiryInterval: 1}},
		{"receive maximum in SUBACK", TypeSuback, Properties{ReceiveMaximum: 1}},
		{"topic alias in CONNECT", TypeConnect, Properties{TopicAlias: 1}},
		{"reason string in PUBLISH", TypePublish, Properties{ReasonString: "r"}},
		{"correlation data in CONNACK", TypeConnack, Properties{CorrelationData: []byte{1}}},
	}
	for _, tc := range encodeCases {
		t.Run("encode/"+tc.name, func(t *testing.T) {
			if _, err := encodeProperties(nil, tc.properties, tc.packetType); !errors.Is(err, ErrMalformedPacket) {
				t.Fatalf("encodeProperties err = %v, want ErrMalformedPacket", err)
			}
		})
	}
}

// TestPropertiesRejectUnknownAndDuplicate covers the decoder's property table
// checks.
func TestPropertiesRejectUnknownAndDuplicate(t *testing.T) {
	cases := []struct {
		name       string
		packetType byte
		raw        []byte
	}{
		{"unknown identifier", TypeConnect, []byte{0x02, 0x04, 0x00}},
		{"duplicate receive maximum", TypeConnect, []byte{0x06, 0x21, 0x00, 0x0a, 0x21, 0x00, 0x0b}},
		{"duplicate session expiry", TypeConnack, []byte{0x0a, 0x11, 0x00, 0x00, 0x00, 0x01, 0x11, 0x00, 0x00, 0x00, 0x02}},
		{"duplicate reason string", TypeSuback, []byte{0x06, 0x1f, 0x00, 0x00, 0x1f, 0x00, 0x00}},
		{"duplicate subscription identifier in SUBSCRIBE", TypeSubscribe, []byte{0x04, 0x0b, 0x01, 0x0b, 0x02}},
		{"non-minimal subscription identifier", TypePublish, []byte{0x03, 0x0b, 0x85, 0x00}},
		{"zero subscription identifier", TypePublish, []byte{0x02, 0x0b, 0x00}},
		{"truncated value", TypeConnect, []byte{0x03, 0x21, 0x00}},
		{"property length past the buffer", TypeConnect, []byte{0x05, 0x21}},
		{"receive maximum zero", TypeConnect, []byte{0x03, 0x21, 0x00, 0x00}},
		{"maximum packet size zero", TypeConnack, []byte{0x05, 0x27, 0x00, 0x00, 0x00, 0x00}},
		{"topic alias zero", TypePublish, []byte{0x03, 0x23, 0x00, 0x00}},
		{"payload format indicator 2", TypePublish, []byte{0x02, 0x01, 0x02}},
		{"maximum QoS 2", TypeConnack, []byte{0x02, 0x24, 0x02}},
		{"retain available 2", TypeConnack, []byte{0x02, 0x25, 0x02}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := decodeProperties(tc.raw, tc.packetType)
			if !errors.Is(err, ErrMalformedPacket) {
				t.Fatalf("decodeProperties(% x) err = %v, want ErrMalformedPacket", tc.raw, err)
			}
		})
	}
}

// TestPropertiesSubscriptionIdentifierRepeat pins the one property whose
// multiplicity depends on the packet.
func TestPropertiesSubscriptionIdentifierRepeat(t *testing.T) {
	raw := []byte{0x04, 0x0b, 0x01, 0x0b, 0x02}
	p, _, err := decodeProperties(raw, TypePublish)
	if err != nil {
		t.Fatalf("decodeProperties: %v", err)
	}
	if !reflect.DeepEqual(p.SubscriptionIdentifiers, []uint32{1, 2}) {
		t.Fatalf("identifiers = %v, want [1 2]", p.SubscriptionIdentifiers)
	}
	if _, _, err := decodeProperties(raw, TypeSubscribe); !errors.Is(err, ErrMalformedPacket) {
		t.Fatalf("duplicate identifier in SUBSCRIBE err = %v, want ErrMalformedPacket", err)
	}
}

// TestPropertiesUserPropertyRepeat pins that user properties may repeat.
func TestPropertiesUserPropertyRepeat(t *testing.T) {
	raw, err := encodeProperties(nil, Properties{
		UserProperties: []UserProperty{{Key: "a", Value: "1"}, {Key: "a", Value: "2"}},
	}, TypePublish)
	if err != nil {
		t.Fatalf("encodeProperties: %v", err)
	}
	p, _, err := decodeProperties(raw, TypePublish)
	if err != nil {
		t.Fatalf("decodeProperties: %v", err)
	}
	if len(p.UserProperties) != 2 || p.UserProperties[1].Value != "2" {
		t.Fatalf("user properties = %+v", p.UserProperties)
	}
}

// TestPropertiesPacketWithoutBlock pins that PINGREQ and PINGRESP reject a
// property block outright.
func TestPropertiesPacketWithoutBlock(t *testing.T) {
	for _, packetType := range []byte{TypePingreq, TypePingresp} {
		if _, _, err := decodeProperties([]byte{0x00}, packetType); !errors.Is(err, ErrMalformedPacket) {
			t.Fatalf("%s decode err = %v, want ErrMalformedPacket", packetName(packetType), err)
		}
		if _, err := encodeProperties(nil, Properties{}, packetType); !errors.Is(err, ErrMalformedPacket) {
			t.Fatalf("%s encode err = %v, want ErrMalformedPacket", packetName(packetType), err)
		}
	}
}

// TestPropertiesDoNotAliasInput pins the ownership rule: decoded property bytes
// are copied out of the caller's buffer.
func TestPropertiesDoNotAliasInput(t *testing.T) {
	raw := []byte{0x06, 0x09, 0x00, 0x03, 'x', 'y', 'z'}
	p, _, err := decodeProperties(raw, TypePublish)
	if err != nil {
		t.Fatalf("decodeProperties: %v", err)
	}
	p.CorrelationData[0] = 'A'
	if raw[4] != 'x' {
		t.Fatalf("decoded property aliases the input buffer")
	}
}

// TestWillProperties checks the will property block, which is not a packet of
// its own.
func TestWillProperties(t *testing.T) {
	want := &Connect{
		MessageHeader: MessageHeader{Level: Version5},
		ClientID:      []byte("c"),
		Will: &Will{
			Topic:   "w",
			Payload: []byte("p"),
			QoS:     1,
			Properties: Properties{
				PayloadFormat:         bytePtr(0),
				MessageExpiryInterval: 5,
				ContentType:           "wt",
				ResponseTopic:         "wr",
				CorrelationData:       []byte{5},
				WillDelayInterval:     10,
				UserProperties:        []UserProperty{{Key: "w", Value: "x"}},
			},
		},
	}
	var buf bytes.Buffer
	if err := Encode(&buf, want); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got := decodeBytes(t, Version5, buf.Bytes())
	if !reflect.DeepEqual(got, Message(want)) {
		t.Fatalf("round trip mismatch\n got: %s\nwant: %s", describe(got), describe(want))
	}

	// A CONNECT property that is not a will property must be rejected inside the
	// will block.
	invalid := []byte{
		0x10, 0x1a, // CONNECT, remaining length 26
		0x00, 0x04, 'M', 'Q', 'T', 'T', // protocol name
		0x05,       // protocol level 5
		0x06,       // clean start | will flag
		0x00, 0x00, // keep alive
		0x00,            // CONNECT property length 0
		0x00, 0x01, 'c', // client id
		0x05, 0x11, 0x00, 0x00, 0x00, 0x01, // will properties carrying session expiry
		0x00, 0x01, 'w', // will topic
		0x00, 0x01, 'p', // will payload
	}
	if _, err := (&Decoder{}).Decode(bufio.NewReader(bytes.NewReader(invalid))); !errors.Is(err, ErrMalformedPacket) {
		t.Fatalf("decode err = %v, want ErrMalformedPacket", err)
	}
}

// TestShortBodyDoesNotConsumeNextPacket pins that a body whose declared length
// cannot hold its fields is rejected without reading past the packet boundary.
func TestShortBodyDoesNotConsumeNextPacket(t *testing.T) {
	// A CONNACK that stops after the reason code, followed by a complete PINGREQ.
	raw := []byte{0x20, 0x02, 0x00, 0x00, 0xc0, 0x00}
	r := bufio.NewReader(bytes.NewReader(raw))
	if _, err := (&Decoder{Version: Version5}).Decode(r); !errors.Is(err, ErrMalformedPacket) {
		t.Fatalf("decode err = %v, want ErrMalformedPacket", err)
	}
	rest, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read remainder: %v", err)
	}
	if !bytes.Equal(rest, []byte{0xc0, 0x00}) {
		t.Fatalf("decoder consumed bytes past the body, remainder = % x", rest)
	}
}

// TestPropertiesErrorKinds pins the classification of property failures so
// callers can distinguish them from incomplete input.
func TestPropertiesErrorKinds(t *testing.T) {
	_, _, err := decodeProperties([]byte{0x05, 0x21}, TypeConnect)
	if wire.KindOf(err) != wire.KindMalformed {
		t.Fatalf("kind = %v, want %v (err %v)", wire.KindOf(err), wire.KindMalformed, err)
	}
	if !errors.Is(err, ErrMalformedPacket) {
		t.Fatalf("err = %v, want it to wrap ErrMalformedPacket", err)
	}
}
