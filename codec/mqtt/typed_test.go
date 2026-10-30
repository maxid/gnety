package mqtt

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

// bytePtr returns a pointer to v, for the optional byte properties.
func bytePtr(v byte) *byte { return &v }

// boolPtr returns a pointer to v, for the optional boolean properties.
func boolPtr(v bool) *bool { return &v }

// uint16Ptr returns a pointer to v, for the optional uint16 properties.
func uint16Ptr(v uint16) *uint16 { return &v }

// decodeBytes decodes exactly one packet and fails when bytes are left over.
func decodeBytes(t *testing.T, version byte, raw []byte) Message {
	t.Helper()
	d := &Decoder{Version: version, MaxPacketSize: 1 << 16}
	r := bufio.NewReader(bytes.NewReader(raw))
	m, err := d.Decode(r)
	if err != nil {
		t.Fatalf("Decode(% x) failed: %v", raw, err)
	}
	if r.Buffered() != 0 {
		t.Fatalf("Decode(% x) left %d bytes unread", raw, r.Buffered())
	}
	return m
}

// roundTripCases covers every control packet type in both protocol versions.
func roundTripCases() []struct {
	name string
	msg  Message
} {
	v4 := MessageHeader{Level: Version311}
	v5 := MessageHeader{Level: Version5}
	return []struct {
		name string
		msg  Message
	}{
		{
			"v4/connect",
			&Connect{
				MessageHeader: v4,
				ClientID:      []byte("client-1"),
				Username:      []byte("user"),
				Password:      []byte("pass"),
				KeepAlive:     30,
				CleanSession:  true,
				Will: &Will{
					Topic:   "will/topic",
					Payload: []byte("bye"),
					QoS:     1,
					Retain:  true,
				},
			},
		},
		{
			"v5/connect",
			&Connect{
				MessageHeader: v5,
				ClientID:      []byte("client-1"),
				KeepAlive:     60,
				CleanSession:  true,
				Will: &Will{
					Topic:   "will/topic",
					Payload: []byte("bye"),
					QoS:     2,
					Properties: Properties{
						WillDelayInterval:     5,
						ContentType:           "text/plain",
						PayloadFormat:         bytePtr(1),
						MessageExpiryInterval: 30,
					},
				},
				Properties: Properties{
					SessionExpiryInterval: 300,
					ReceiveMaximum:        20,
					MaximumPacketSize:     4096,
					TopicAliasMaximum:     10,
					AuthenticationMethod:  "SCRAM-SHA-1",
					AuthenticationData:    []byte{1, 2, 3},
					RequestProblemInfo:    boolPtr(false),
					RequestResponseInfo:   boolPtr(true),
					UserProperties:        []UserProperty{{Key: "k", Value: "v"}},
				},
			},
		},
		{"v4/connack", &Connack{MessageHeader: v4, SessionPresent: true}},
		{"v4/connack-refused", &Connack{MessageHeader: v4, ReasonCode: 5}},
		{
			"v5/connack",
			&Connack{
				MessageHeader: v5,
				ReasonCode:    0,
				Properties: Properties{
					SessionExpiryInterval:           60,
					ReceiveMaximum:                  10,
					MaximumQoS:                      bytePtr(1),
					RetainAvailable:                 boolPtr(true),
					MaximumPacketSize:               8192,
					TopicAliasMaximum:               5,
					AssignedClientID:                "auto-1",
					ServerKeepAlive:                 uint16Ptr(45),
					ResponseInfo:                    "resp",
					ServerReference:                 "other:1883",
					ReasonString:                    "welcome",
					AuthenticationMethod:            "SCRAM-SHA-1",
					AuthenticationData:              []byte{4, 5},
					WildcardSubscriptionAvailable:   boolPtr(false),
					SubscriptionIdentifierAvailable: boolPtr(true),
					SharedSubscriptionAvailable:     boolPtr(true),
					UserProperties:                  []UserProperty{{Key: "a", Value: "b"}, {Key: "a", Value: "c"}},
				},
			},
		},
		{
			"v4/publish-qos0",
			&Publish{MessageHeader: v4, Topic: "sensors/1/temp", Payload: []byte("21.5"), Retain: true},
		},
		{
			"v4/publish-qos1",
			&Publish{MessageHeader: v4, Topic: "sensors/1/temp", Payload: []byte("21.5"), QoS: 1, PacketID: 0x1234, Dup: true},
		},
		{
			"v4/publish-qos2",
			&Publish{MessageHeader: v4, Topic: "a/b", Payload: []byte{1, 2, 3}, QoS: 2, PacketID: 7},
		},
		{
			"v5/publish-qos0",
			&Publish{
				MessageHeader: v5,
				Topic:         "sensors/1/temp",
				Payload:       []byte("21.5"),
				Properties: Properties{
					PayloadFormat:         bytePtr(1),
					MessageExpiryInterval: 30,
					ContentType:           "text/plain",
					ResponseTopic:         "reply/1",
					CorrelationData:       []byte{9, 9},
					UserProperties:        []UserProperty{{Key: "k", Value: "v"}},
				},
			},
		},
		{
			"v5/publish-qos1-alias",
			&Publish{
				MessageHeader: v5,
				Topic:         "sensors/1/temp",
				Payload:       []byte("21.5"),
				QoS:           1,
				PacketID:      10,
				Retain:        true,
				Properties: Properties{
					TopicAlias:              3,
					SubscriptionIdentifiers: []uint32{1, 268435455},
				},
			},
		},
		{
			"v5/publish-qos2-empty-payload",
			&Publish{MessageHeader: v5, Topic: "a/b", Payload: []byte{}, QoS: 2, PacketID: 99},
		},
		{"v4/puback", &Puback{MessageHeader: v4, PacketID: 0x1234}},
		{"v5/puback-reason", &Puback{MessageHeader: v5, PacketID: 0x1234, ReasonCode: 0x10, Properties: Properties{ReasonString: "nobody listening"}}},
		{"v4/pubrec", &Pubrec{MessageHeader: v4, PacketID: 0x1234}},
		{"v5/pubrec-reason", &Pubrec{MessageHeader: v5, PacketID: 0x1234, ReasonCode: 0x92}},
		{"v4/pubrel", &Pubrel{MessageHeader: v4, PacketID: 0x1234}},
		{"v5/pubrel", &Pubrel{MessageHeader: v5, PacketID: 0x1234, Properties: Properties{UserProperties: []UserProperty{{Key: "k", Value: "v"}}}}},
		{"v4/pubcomp", &Pubcomp{MessageHeader: v4, PacketID: 0x1234}},
		{"v5/pubcomp-reason", &Pubcomp{MessageHeader: v5, PacketID: 0x1234, ReasonCode: 0x92}},
		{
			"v4/subscribe",
			&Subscribe{
				MessageHeader: v4,
				PacketID:      1,
				Subscriptions: []Subscription{{Filter: "a/+", QoS: 1}, {Filter: "b/#", QoS: 2}},
			},
		},
		{
			"v5/subscribe-options",
			&Subscribe{
				MessageHeader: v5,
				PacketID:      1,
				Properties:    Properties{SubscriptionIdentifiers: []uint32{5}},
				Subscriptions: []Subscription{
					{Filter: "a/+", QoS: 1, NoLocal: true, RetainAsPublished: true, RetainHandling: 2},
					{Filter: "b/#", QoS: 0},
				},
			},
		},
		{"v4/suback", &Suback{MessageHeader: v4, PacketID: 1, ReasonCodes: []byte{0, 2}}},
		{
			"v5/suback",
			&Suback{
				MessageHeader: v5,
				PacketID:      1,
				ReasonCodes:   []byte{0, 1, 0x80},
				Properties:    Properties{ReasonString: "partial"},
			},
		},
		{
			"v4/unsubscribe",
			&Unsubscribe{MessageHeader: v4, PacketID: 2, Filters: []string{"a/+", "b/#"}},
		},
		{
			"v5/unsubscribe",
			&Unsubscribe{
				MessageHeader: v5,
				PacketID:      2,
				Filters:       []string{"a/+"},
				Properties:    Properties{UserProperties: []UserProperty{{Key: "k", Value: "v"}}},
			},
		},
		{"v4/unsuback", &Unsuback{MessageHeader: v4, PacketID: 2}},
		{"v5/unsuback", &Unsuback{MessageHeader: v5, PacketID: 2, ReasonCodes: []byte{0x00, 0x11}}},
		{"v4/pingreq", &Pingreq{MessageHeader: v4}},
		{"v5/pingreq", &Pingreq{MessageHeader: v5}},
		{"v4/pingresp", &Pingresp{MessageHeader: v4}},
		{"v5/pingresp", &Pingresp{MessageHeader: v5}},
		{"v4/disconnect", &Disconnect{MessageHeader: v4}},
		{
			"v5/disconnect",
			&Disconnect{
				MessageHeader: v5,
				ReasonCode:    0x8e,
				Properties: Properties{
					SessionExpiryInterval: 1,
					ServerReference:       "other:1883",
					ReasonString:          "taken over",
				},
			},
		},
	}
}

// TestRoundTrip encodes every control packet type and decodes it back.
func TestRoundTrip(t *testing.T) {
	for _, tc := range roundTripCases() {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Encode(&buf, tc.msg); err != nil {
				t.Fatalf("Encode: %v", err)
			}
			got := decodeBytes(t, tc.msg.Version(), buf.Bytes())
			if !reflect.DeepEqual(got, tc.msg) {
				t.Fatalf("round trip mismatch\n got: %s\nwant: %s", describe(got), describe(tc.msg))
			}
		})
	}
}

// describe renders a message for failure messages without depending on field
// layout details in the test body.
func describe(m Message) string {
	if m == nil {
		return "<nil>"
	}
	v := reflect.ValueOf(m)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	return fmt.Sprintf("%s%+v", packetName(m.Type()), v.Interface())
}

// TestRoundTripValues checks that packet values, not only pointers, encode.
func TestRoundTripValues(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, Pingreq{}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), []byte{0xc0, 0x00}) {
		t.Fatalf("Encode = % x", buf.Bytes())
	}
	connack := Connack{MessageHeader: MessageHeader{Level: Version311}}
	buf.Reset()
	if err := Encode(&buf, &connack); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got := decodeBytes(t, Version311, buf.Bytes())
	if !reflect.DeepEqual(got, &connack) {
		t.Fatalf("got %s, want %s", describe(got), describe(&connack))
	}
}

// goldenCases pins the exact wire bytes of representative packets.
func goldenCases() []struct {
	name string
	msg  Message
	wire []byte
} {
	return []struct {
		name string
		msg  Message
		wire []byte
	}{
		{
			"connect-v4",
			&Connect{
				MessageHeader: MessageHeader{Level: Version311},
				ClientID:      []byte("client-1"),
				Username:      []byte("user"),
				Password:      []byte("pass"),
				KeepAlive:     30,
				CleanSession:  true,
			},
			[]byte{
				0x10, 0x20, // CONNECT, remaining length 32
				0x00, 0x04, 'M', 'Q', 'T', 'T', // protocol name
				0x04,       // protocol level 4
				0xc2,       // clean session | password | user name
				0x00, 0x1e, // keep alive 30
				0x00, 0x08, 'c', 'l', 'i', 'e', 'n', 't', '-', '1', // client id
				0x00, 0x04, 'u', 's', 'e', 'r', // user name
				0x00, 0x04, 'p', 'a', 's', 's', // password
			},
		},
		{
			"connect-v5",
			&Connect{
				MessageHeader: MessageHeader{Level: Version5},
				ClientID:      []byte("c1"),
				KeepAlive:     60,
				CleanSession:  true,
				Properties: Properties{
					SessionExpiryInterval: 300,
					ReceiveMaximum:        20,
					TopicAliasMaximum:     10,
					UserProperties:        []UserProperty{{Key: "k", Value: "v"}},
					MaximumPacketSize:     4096,
				},
			},
			[]byte{
				0x10, 0x26, // CONNECT, remaining length 38
				0x00, 0x04, 'M', 'Q', 'T', 'T',
				0x05,       // protocol level 5
				0x02,       // clean start
				0x00, 0x3c, // keep alive 60
				0x17,                         // property length 23
				0x11, 0x00, 0x00, 0x01, 0x2c, // session expiry 300
				0x21, 0x00, 0x14, // receive maximum 20
				0x22, 0x00, 0x0a, // topic alias maximum 10
				0x26, 0x00, 0x01, 'k', 0x00, 0x01, 'v', // user property k=v
				0x27, 0x00, 0x00, 0x10, 0x00, // maximum packet size 4096
				0x00, 0x02, 'c', '1', // client id
			},
		},
		{
			"publish-qos0-v4",
			&Publish{
				MessageHeader: MessageHeader{Level: Version311},
				Topic:         "a/b",
				Payload:       []byte("hi"),
			},
			[]byte{
				0x30, 0x07, // PUBLISH, remaining length 7
				0x00, 0x03, 'a', '/', 'b', // topic
				'h', 'i', // payload
			},
		},
		{
			"publish-qos1-v4",
			&Publish{
				MessageHeader: MessageHeader{Level: Version311},
				Topic:         "a/b",
				Payload:       []byte("hi"),
				QoS:           1,
				PacketID:      10,
			},
			[]byte{
				0x32, 0x09, // PUBLISH QoS 1, remaining length 9
				0x00, 0x03, 'a', '/', 'b', // topic
				0x00, 0x0a, // packet id 10
				'h', 'i', // payload
			},
		},
		{
			"publish-qos1-v5",
			&Publish{
				MessageHeader: MessageHeader{Level: Version5},
				Topic:         "a/b",
				Payload:       []byte("hi"),
				QoS:           1,
				PacketID:      10,
				Properties:    Properties{UserProperties: []UserProperty{{Key: "k", Value: "v"}}},
			},
			[]byte{
				0x32, 0x11, // PUBLISH QoS 1, remaining length 17
				0x00, 0x03, 'a', '/', 'b', // topic
				0x00, 0x0a, // packet id 10
				0x07,                                   // property length 7
				0x26, 0x00, 0x01, 'k', 0x00, 0x01, 'v', // user property k=v
				'h', 'i', // payload
			},
		},
		{
			"publish-qos0-v5-minimal",
			&Publish{
				MessageHeader: MessageHeader{Level: Version5},
				Topic:         "a",
				Payload:       []byte("x"),
			},
			[]byte{
				0x30, 0x05, // PUBLISH, remaining length 5
				0x00, 0x01, 'a', // topic
				0x00, // empty property block
				'x',  // payload
			},
		},
		{
			"subscribe-v5",
			&Subscribe{
				MessageHeader: MessageHeader{Level: Version5},
				PacketID:      1,
				Properties:    Properties{SubscriptionIdentifiers: []uint32{5}},
				Subscriptions: []Subscription{
					{Filter: "a/+", QoS: 1, NoLocal: true, RetainAsPublished: true, RetainHandling: 2},
				},
			},
			[]byte{
				0x82, 0x0b, // SUBSCRIBE, remaining length 11
				0x00, 0x01, // packet id 1
				0x02,       // property length 2
				0x0b, 0x05, // subscription identifier 5
				0x00, 0x03, 'a', '/', '+', // topic filter
				0x2d, // QoS 1 | no local | retain as published | retain handling 2
			},
		},
		{
			"puback-v5-reason",
			&Puback{MessageHeader: MessageHeader{Level: Version5}, PacketID: 10, ReasonCode: 0x10},
			[]byte{
				0x40, 0x03, // PUBACK, remaining length 3
				0x00, 0x0a, // packet id 10
				0x10, // no matching subscribers
			},
		},
		{
			"puback-v5-minimal",
			&Puback{MessageHeader: MessageHeader{Level: Version5}, PacketID: 10},
			[]byte{
				0x40, 0x02, // PUBACK, remaining length 2: reason code and properties omitted
				0x00, 0x0a,
			},
		},
		{
			"pingreq-v4",
			&Pingreq{MessageHeader: MessageHeader{Level: Version311}},
			[]byte{0xc0, 0x00},
		},
		{
			"pingresp-v5",
			&Pingresp{MessageHeader: MessageHeader{Level: Version5}},
			[]byte{0xd0, 0x00},
		},
		{
			"disconnect-v5-reason",
			&Disconnect{MessageHeader: MessageHeader{Level: Version5}, ReasonCode: 0x8e},
			[]byte{
				0xe0, 0x01, // DISCONNECT, remaining length 1
				0x8e, // session taken over
			},
		},
		{
			"disconnect-v5-properties",
			&Disconnect{
				MessageHeader: MessageHeader{Level: Version5},
				Properties:    Properties{SessionExpiryInterval: 1},
			},
			[]byte{
				0xe0, 0x07, // DISCONNECT, remaining length 7
				0x00,                         // normal disconnection
				0x05,                         // property length 5
				0x11, 0x00, 0x00, 0x00, 0x01, // session expiry 1
			},
		},
		{
			"disconnect-v4",
			&Disconnect{MessageHeader: MessageHeader{Level: Version311}},
			[]byte{0xe0, 0x00},
		},
	}
}

// TestGolden encodes the fixed fixtures and decodes them back.
func TestGolden(t *testing.T) {
	for _, tc := range goldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Encode(&buf, tc.msg); err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if !bytes.Equal(buf.Bytes(), tc.wire) {
				t.Fatalf("Encode mismatch\n got: % x\nwant: % x", buf.Bytes(), tc.wire)
			}
			got := decodeBytes(t, tc.msg.Version(), tc.wire)
			if !reflect.DeepEqual(got, tc.msg) {
				t.Fatalf("decode mismatch\n got: %s\nwant: %s", describe(got), describe(tc.msg))
			}
		})
	}
}

// TestDecodeErrors covers the packets the decoder must reject.
func TestDecodeErrors(t *testing.T) {
	cases := []struct {
		name    string
		version byte
		max     int
		wire    []byte
		kind    wire.Kind
	}{
		{
			name: "packet type 0",
			wire: []byte{0x00, 0x00},
			kind: wire.KindMalformed,
		},
		{
			name: "packet type 15",
			wire: []byte{0xf0, 0x00},
			kind: wire.KindMalformed,
		},
		{
			name: "reserved flags on PINGREQ",
			wire: []byte{0xc1, 0x00},
			kind: wire.KindMalformed,
		},
		{
			name: "reserved flags on PUBREL",
			wire: []byte{0x60, 0x02, 0x00, 0x01},
			kind: wire.KindMalformed,
		},
		{
			name: "reserved flags on SUBSCRIBE",
			wire: []byte{0x80, 0x08, 0x00, 0x01, 0x00, 0x03, 'a', '/', '+', 0x02},
			kind: wire.KindMalformed,
		},
		{
			name: "publish QoS 3",
			wire: []byte{0x36, 0x00},
			kind: wire.KindMalformed,
		},
		{
			name: "publish QoS 0 with DUP",
			wire: []byte{0x38, 0x03, 0x00, 0x01, 'a'},
			kind: wire.KindMalformed,
		},
		{
			name: "publish QoS 1 with packet id 0",
			wire: []byte{0x32, 0x07, 0x00, 0x03, 'a', '/', 'b', 0x00, 0x00},
			kind: wire.KindMalformed,
		},
		{
			name: "oversized packet",
			max:  4,
			wire: []byte{0x30, 0x05, 0x00, 0x01, 'a', 'b', 'c'},
			kind: wire.KindTooLarge,
		},
		{
			name: "truncated body",
			wire: []byte{0x30, 0x05, 0x00, 0x01, 'a'},
			kind: wire.KindIncomplete,
		},
		{
			name: "trailing bytes after PINGREQ",
			wire: []byte{0xc0, 0x01, 0x00},
			kind: wire.KindMalformed,
		},
		{
			name: "trailing bytes after CONNECT",
			wire: []byte{
				0x10, 0x0f,
				0x00, 0x04, 'M', 'Q', 'T', 'T', 0x04, 0x02, 0x00, 0x00,
				0x00, 0x01, 'c',
				0xff,
			},
			kind: wire.KindMalformed,
		},
		{
			name:    "v5-shaped PUBACK decoded as 3.1.1",
			version: Version311,
			wire:    []byte{0x40, 0x04, 0x00, 0x0a, 0x00, 0x00},
			kind:    wire.KindMalformed,
		},
		{
			name: "reserved CONNECT flag",
			wire: []byte{
				0x10, 0x0e,
				0x00, 0x04, 'M', 'Q', 'T', 'T', 0x04, 0x03, 0x00, 0x00,
				0x00, 0x01, 'c',
			},
			kind: wire.KindMalformed,
		},
		{
			name: "CONNECT password without user name",
			wire: []byte{
				0x10, 0x0e,
				0x00, 0x04, 'M', 'Q', 'T', 'T', 0x04, 0x42, 0x00, 0x00,
				0x00, 0x01, 'c',
			},
			kind: wire.KindMalformed,
		},
		{
			name: "CONNECT will QoS without will flag",
			wire: []byte{
				0x10, 0x0e,
				0x00, 0x04, 'M', 'Q', 'T', 'T', 0x04, 0x0a, 0x00, 0x00,
				0x00, 0x01, 'c',
			},
			kind: wire.KindMalformed,
		},
		{
			name: "unsupported protocol name",
			wire: []byte{
				0x10, 0x10,
				0x00, 0x06, 'M', 'Q', 'I', 's', 'd', 'p', 0x03, 0x02, 0x00, 0x00,
				0x00, 0x01, 'c',
			},
			kind: wire.KindUnsupported,
		},
		{
			name: "unsupported protocol level",
			wire: []byte{
				0x10, 0x0e,
				0x00, 0x04, 'M', 'Q', 'T', 'T', 0x03, 0x02, 0x00, 0x00,
				0x00, 0x01, 'c',
			},
			kind: wire.KindUnsupported,
		},
		{
			name: "CONNACK reserved acknowledge flags",
			wire: []byte{0x20, 0x02, 0x02, 0x00},
			kind: wire.KindMalformed,
		},
		{
			name: "SUBSCRIBE 3.1.1 options above QoS",
			wire: []byte{0x82, 0x08, 0x00, 0x01, 0x00, 0x03, 'a', '/', '+', 0x04},
			kind: wire.KindMalformed,
		},
		{
			name:    "SUBSCRIBE 5 retain handling 3",
			version: Version5,
			wire:    []byte{0x82, 0x09, 0x00, 0x01, 0x00, 0x00, 0x03, 'a', '/', '+', 0x30},
			kind:    wire.KindMalformed,
		},
		{
			name: "SUBSCRIBE without filters",
			wire: []byte{0x82, 0x02, 0x00, 0x01},
			kind: wire.KindMalformed,
		},
		{
			name: "SUBACK without reason codes",
			wire: []byte{0x90, 0x02, 0x00, 0x01},
			kind: wire.KindMalformed,
		},
		{
			name:    "UNSUBACK 5 without reason codes",
			version: Version5,
			wire:    []byte{0xb0, 0x03, 0x00, 0x01, 0x00},
			kind:    wire.KindMalformed,
		},
		{
			name:    "PUBACK 5 property invalid for the packet",
			version: Version5,
			wire:    []byte{0x40, 0x09, 0x00, 0x0a, 0x00, 0x05, 0x11, 0x00, 0x00, 0x00, 0x01},
			kind:    wire.KindMalformed,
		},
		{
			name:    "CONNECT 5 duplicate property",
			version: Version5,
			wire: []byte{
				0x10, 0x15,
				0x00, 0x04, 'M', 'Q', 'T', 'T', 0x05, 0x02, 0x00, 0x00,
				0x06, 0x21, 0x00, 0x14, 0x21, 0x00, 0x14,
				0x00, 0x02, 'c', '1',
			},
			kind: wire.KindMalformed,
		},
		{
			name:    "CONNECT 5 unknown property",
			version: Version5,
			wire: []byte{
				0x10, 0x11,
				0x00, 0x04, 'M', 'Q', 'T', 'T', 0x05, 0x02, 0x00, 0x00,
				0x02, 0x04, 0x00,
				0x00, 0x02, 'c', '1',
			},
			kind: wire.KindMalformed,
		},
		{
			name:    "PUBLISH 5 property length past the body",
			version: Version5,
			wire:    []byte{0x30, 0x06, 0x00, 0x01, 'a', 0x04, 0x09, 0x00},
			kind:    wire.KindMalformed,
		},
		{
			name:    "PUBLISH 5 property value too short",
			version: Version5,
			wire:    []byte{0x30, 0x06, 0x00, 0x01, 'a', 0x02, 0x09, 0x00},
			kind:    wire.KindMalformed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &Decoder{Version: tc.version, MaxPacketSize: tc.max}
			_, err := d.Decode(bufio.NewReader(bytes.NewReader(tc.wire)))
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

// TestDecodeIncompleteKeepsIOError pins that a truncated packet is reported as
// incomplete input rather than as a broken peer.
func TestDecodeIncompleteKeepsIOError(t *testing.T) {
	_, err := (&Decoder{}).Decode(bufio.NewReader(bytes.NewReader([]byte{0x30, 0x05, 0x00, 0x01, 'a'})))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want it to wrap io.EOF", err)
	}
}

// TestDecodeEOF pins that a clean end of stream stays io.EOF.
func TestDecodeEOF(t *testing.T) {
	_, err := (&Decoder{}).Decode(bufio.NewReader(bytes.NewReader(nil)))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

// TestDecoderTracksConnectVersion checks that CONNECT refreshes Decoder.Version
// so later packets are parsed with the negotiated layout.
func TestDecoderTracksConnectVersion(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, &Connect{MessageHeader: MessageHeader{Level: Version5}, KeepAlive: 10}); err != nil {
		t.Fatal(err)
	}
	if err := Encode(&buf, &Publish{MessageHeader: MessageHeader{Level: Version5}, Topic: "a", Payload: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	d := &Decoder{}
	r := bufio.NewReader(&buf)
	first, err := d.Decode(r)
	if err != nil {
		t.Fatalf("Decode CONNECT: %v", err)
	}
	if d.Version != Version5 {
		t.Fatalf("Decoder.Version = %d after CONNECT, want %d", d.Version, Version5)
	}
	second, err := d.Decode(r)
	if err != nil {
		t.Fatalf("Decode PUBLISH: %v", err)
	}
	if second.Version() != Version5 {
		t.Fatalf("PUBLISH version = %d, want %d", second.Version(), Version5)
	}
	if first.Type() != TypeConnect || second.Type() != TypePublish {
		t.Fatalf("types = %d, %d", first.Type(), second.Type())
	}
}

// TestEncodeErrors covers messages the encoder must refuse.
func TestEncodeErrors(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{"nil message", nil},
		{"nil pointer", (*Connect)(nil)},
		{"unsupported level", &Connect{MessageHeader: MessageHeader{Level: 3}}},
		{"publish QoS 3", &Publish{Topic: "a", QoS: 3}},
		{"publish QoS 1 without packet id", &Publish{Topic: "a", QoS: 1}},
		{"connect password without user name", &Connect{Password: []byte("p")}},
		{"connect will QoS 3", &Connect{Will: &Will{Topic: "w", QoS: 3}}},
		{"subscribe without filters", &Subscribe{PacketID: 1}},
		{"subscribe without packet id", &Subscribe{Subscriptions: []Subscription{{Filter: "a"}}}},
		{"subscribe retain handling 3", &Subscribe{
			MessageHeader: MessageHeader{Level: Version5},
			PacketID:      1,
			Subscriptions: []Subscription{{Filter: "a", RetainHandling: 3}},
		}},
		{"suback without reason codes", &Suback{PacketID: 1}},
		{"unsubscribe without filters", &Unsubscribe{PacketID: 1}},
		{"unsuback 5 without reason codes", &Unsuback{MessageHeader: MessageHeader{Level: Version5}, PacketID: 1}},
		{"property invalid for publish", &Publish{
			MessageHeader: MessageHeader{Level: Version5},
			Topic:         "a",
			Properties:    Properties{SessionExpiryInterval: 10},
		}},
		{"maximum QoS 2", &Connack{
			MessageHeader: MessageHeader{Level: Version5},
			Properties:    Properties{MaximumQoS: bytePtr(2)},
		}},
		{"zero subscription identifier", &Publish{
			MessageHeader: MessageHeader{Level: Version5},
			Topic:         "a",
			Properties:    Properties{SubscriptionIdentifiers: []uint32{0}},
		}},
		{"invalid UTF-8 topic", &Publish{Topic: "\xff"}},
		{"over-long string", &Publish{Topic: string(make([]byte, 0x10000))}},
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
			if errors.Is(err, ErrMalformedPacket) {
				return
			}
			if wire.KindOf(err) == wire.KindUnsupported {
				return
			}
			t.Fatalf("err = %v, want a classified error", err)
		})
	}
}

// TestCodecAdapter pins the stream codec shape used by gnety's pipeline.
func TestCodecAdapter(t *testing.T) {
	var _ interface {
		Decode(r *bufio.Reader) (any, error)
		Encode(w io.Writer, msg any) error
	} = (*Codec)(nil)

	c := &Codec{Decoder: Decoder{MaxPacketSize: 1 << 16}}
	var buf bytes.Buffer
	if err := c.Encode(&buf, &Pingreq{}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	raw, err := c.Decode(bufio.NewReader(&buf))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	m, ok := raw.(Message)
	if !ok {
		t.Fatalf("Decode returned %T, want a Message", raw)
	}
	if m.Type() != TypePingreq {
		t.Fatalf("type = %d, want %d", m.Type(), TypePingreq)
	}
	if err := c.Encode(io.Discard, "not a message"); !errors.Is(err, ErrMalformedPacket) {
		t.Fatalf("Encode(non-message) err = %v, want ErrMalformedPacket", err)
	}
}

// FuzzDecode checks that no input makes the decoder or the encoder panic.
func FuzzDecode(f *testing.F) {
	for _, tc := range goldenCases() {
		f.Add(tc.wire)
	}
	for _, tc := range roundTripCases() {
		var buf bytes.Buffer
		if err := Encode(&buf, tc.msg); err != nil {
			f.Fatalf("Encode(%s): %v", tc.name, err)
		}
		f.Add(buf.Bytes())
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		d := &Decoder{MaxPacketSize: 1 << 12}
		r := bufio.NewReader(bytes.NewReader(data))
		for i := 0; i < 6; i++ {
			m, err := d.Decode(r)
			if err != nil {
				return
			}
			if m == nil {
				t.Fatalf("Decode returned no message and no error for % x", data)
			}
			if m.Type() < TypeConnect || m.Type() > TypeDisconnect {
				t.Fatalf("decoded packet type %d", m.Type())
			}
			// A decoded packet must always be encodable.
			if err := Encode(io.Discard, m); err != nil {
				t.Fatalf("Encode(%s) after successful Decode: %v", packetName(m.Type()), err)
			}
		}
	})
}
