package mqtt

// Control packet types, encoded in the high nibble of the fixed header byte.
const (
	// TypeConnect is CONNECT (1): a client opens a session.
	TypeConnect byte = 1
	// TypeConnack is CONNACK (2): the server answers CONNECT.
	TypeConnack byte = 2
	// TypePublish is PUBLISH (3): one application message, either direction.
	TypePublish byte = 3
	// TypePuback is PUBACK (4): the QoS 1 acknowledgement.
	TypePuback byte = 4
	// TypePubrec is PUBREC (5): the first QoS 2 acknowledgement.
	TypePubrec byte = 5
	// TypePubrel is PUBREL (6): the second QoS 2 acknowledgement.
	TypePubrel byte = 6
	// TypePubcomp is PUBCOMP (7): the last QoS 2 acknowledgement.
	TypePubcomp byte = 7
	// TypeSubscribe is SUBSCRIBE (8): a client requests one or more filters.
	TypeSubscribe byte = 8
	// TypeSuback is SUBACK (9): the server answers SUBSCRIBE.
	TypeSuback byte = 9
	// TypeUnsubscribe is UNSUBSCRIBE (10): a client drops filters.
	TypeUnsubscribe byte = 10
	// TypeUnsuback is UNSUBACK (11): the server answers UNSUBSCRIBE.
	TypeUnsuback byte = 11
	// TypePingreq is PINGREQ (12).
	TypePingreq byte = 12
	// TypePingresp is PINGRESP (13).
	TypePingresp byte = 13
	// TypeDisconnect is DISCONNECT (14).
	TypeDisconnect byte = 14
)

// Protocol levels understood by the typed codec. MQTT 3.1 (level 3, protocol
// name "MQIsdp") is deliberately not modeled: it differs from 3.1.1 in ways no
// consumer of this package needs.
const (
	// Version311 is MQTT 3.1.1, protocol level 4.
	Version311 byte = 4
	// Version5 is MQTT 5.0, protocol level 5.
	Version5 byte = 5
)

// MessageHeader records the protocol level a packet belongs to. Every packet
// type embeds it, which is what makes the packet types satisfy Message.
//
// The level is part of the packet because it decides the wire layout: MQTT 5
// adds property blocks and optional reason codes, MQTT 3.1.1 has neither. A
// zero Level means Version311, so a packet built without a header always
// encodes as 3.1.1.
type MessageHeader struct {
	// Level is the MQTT protocol level: Version311 or Version5.
	Level byte
}

// Version reports the MQTT protocol level of the packet, defaulting to
// Version311.
func (h MessageHeader) Version() byte {
	if h.Level == 0 {
		return Version311
	}
	return h.Level
}

// Message is one MQTT control packet.
//
// The typed layer describes packets only. Ordering, duplicate suppression,
// retransmission, sessions, retained messages and every other protocol state
// machine live in the MQTT protocol library that consumes this package.
type Message interface {
	// Type reports the control packet type (TypeConnect and friends).
	Type() byte
	// Version reports the MQTT protocol level: Version311 or Version5.
	Version() byte
}

// Will is the last message a client asks the server to publish for it.
type Will struct {
	// Topic is the UTF-8 topic name the will is published to.
	Topic string
	// Payload is the message body.
	Payload []byte
	// QoS is the requested delivery quality, 0 to 2.
	QoS byte
	// Retain asks the server to store the will as the retained message of Topic.
	Retain bool
	// Properties holds the MQTT 5 will properties. It is empty and ignored on
	// MQTT 3.1.1.
	Properties Properties
}

// Connect is a CONNECT packet.
type Connect struct {
	MessageHeader
	// ClientID is the UTF-8 client identifier. MQTT 5 allows an empty one and
	// lets the server assign it.
	ClientID []byte
	// Username is the UTF-8 user name, or nil when absent.
	Username []byte
	// Password is the binary password, or nil when absent. MQTT requires a
	// password to be accompanied by a user name.
	Password []byte
	// KeepAlive is the keep-alive interval in seconds.
	KeepAlive uint16
	// CleanSession is the MQTT 3.1.1 clean-session flag; MQTT 5 calls the same
	// bit Clean Start.
	CleanSession bool
	// Will is the client's last will, or nil when it has none.
	Will *Will
	// Properties holds the MQTT 5 CONNECT properties.
	Properties Properties
}

// Type reports CONNECT.
func (m Connect) Type() byte { return TypeConnect }

// Connack is a CONNACK packet.
type Connack struct {
	MessageHeader
	// SessionPresent reports whether the server resumed an existing session.
	SessionPresent bool
	// ReasonCode is the MQTT 5 reason code, or the 3.1.1 connect return code
	// (the value ranges agree for 0 to 5).
	ReasonCode byte
	// Properties holds the MQTT 5 CONNACK properties.
	Properties Properties
}

// Type reports CONNACK.
func (m Connack) Type() byte { return TypeConnack }

// Publish is a PUBLISH packet.
type Publish struct {
	MessageHeader
	// Topic is the UTF-8 topic name. Wildcard matching is the receiver's job.
	Topic string
	// Payload is the application message.
	Payload []byte
	// QoS is the delivery quality, 0 to 2.
	QoS byte
	// PacketID identifies the message for QoS 1 and 2. It is zero and not
	// encoded on QoS 0.
	PacketID uint16
	// Retain marks the message as retained.
	Retain bool
	// Dup marks a re-delivery of an earlier PUBLISH.
	Dup bool
	// Properties holds the MQTT 5 PUBLISH properties.
	Properties Properties
}

// Type reports PUBLISH.
func (m Publish) Type() byte { return TypePublish }

// Puback is a PUBACK packet.
type Puback struct {
	MessageHeader
	// PacketID identifies the PUBLISH being acknowledged.
	PacketID uint16
	// ReasonCode is the MQTT 5 reason code; MQTT 3.1.1 has no reason code.
	ReasonCode byte
	// Properties holds the MQTT 5 PUBACK properties.
	Properties Properties
}

// Type reports PUBACK.
func (m Puback) Type() byte { return TypePuback }

// Pubrec is a PUBREC packet.
type Pubrec struct {
	MessageHeader
	// PacketID identifies the PUBLISH being acknowledged.
	PacketID uint16
	// ReasonCode is the MQTT 5 reason code; MQTT 3.1.1 has no reason code.
	ReasonCode byte
	// Properties holds the MQTT 5 PUBREC properties.
	Properties Properties
}

// Type reports PUBREC.
func (m Pubrec) Type() byte { return TypePubrec }

// Pubrel is a PUBREL packet.
type Pubrel struct {
	MessageHeader
	// PacketID identifies the PUBLISH being released.
	PacketID uint16
	// ReasonCode is the MQTT 5 reason code; MQTT 3.1.1 has no reason code.
	ReasonCode byte
	// Properties holds the MQTT 5 PUBREL properties.
	Properties Properties
}

// Type reports PUBREL.
func (m Pubrel) Type() byte { return TypePubrel }

// Pubcomp is a PUBCOMP packet.
type Pubcomp struct {
	MessageHeader
	// PacketID identifies the PUBLISH being completed.
	PacketID uint16
	// ReasonCode is the MQTT 5 reason code; MQTT 3.1.1 has no reason code.
	ReasonCode byte
	// Properties holds the MQTT 5 PUBCOMP properties.
	Properties Properties
}

// Type reports PUBCOMP.
func (m Pubcomp) Type() byte { return TypePubcomp }

// Subscription is one filter of a SUBSCRIBE packet.
type Subscription struct {
	// Filter is the UTF-8 topic filter, wildcards included.
	Filter string
	// QoS is the maximum delivery quality the client accepts, 0 to 2.
	QoS byte
	// NoLocal asks the server not to echo the client's own publications. MQTT 5
	// only.
	NoLocal bool
	// RetainAsPublished asks the server to keep the retain flag as published.
	// MQTT 5 only.
	RetainAsPublished bool
	// RetainHandling selects how retained messages are sent: 0 always, 1 only
	// on a new subscription, 2 never. MQTT 5 only.
	RetainHandling byte
}

// Subscribe is a SUBSCRIBE packet.
type Subscribe struct {
	MessageHeader
	// PacketID identifies the request; the SUBACK echoes it.
	PacketID uint16
	// Subscriptions holds at least one filter.
	Subscriptions []Subscription
	// Properties holds the MQTT 5 SUBSCRIBE properties.
	Properties Properties
}

// Type reports SUBSCRIBE.
func (m Subscribe) Type() byte { return TypeSubscribe }

// Suback is a SUBACK packet.
type Suback struct {
	MessageHeader
	// PacketID echoes the SUBSCRIBE being answered.
	PacketID uint16
	// ReasonCodes holds one MQTT 5 reason code, or one 3.1.1 return code, per
	// requested filter.
	ReasonCodes []byte
	// Properties holds the MQTT 5 SUBACK properties.
	Properties Properties
}

// Type reports SUBACK.
func (m Suback) Type() byte { return TypeSuback }

// Unsubscribe is an UNSUBSCRIBE packet.
type Unsubscribe struct {
	MessageHeader
	// PacketID identifies the request; the UNSUBACK echoes it.
	PacketID uint16
	// Filters holds at least one UTF-8 topic filter.
	Filters []string
	// Properties holds the MQTT 5 UNSUBSCRIBE properties.
	Properties Properties
}

// Type reports UNSUBSCRIBE.
func (m Unsubscribe) Type() byte { return TypeUnsubscribe }

// Unsuback is an UNSUBACK packet.
type Unsuback struct {
	MessageHeader
	// PacketID echoes the UNSUBSCRIBE being answered.
	PacketID uint16
	// ReasonCodes holds one MQTT 5 reason code per requested filter. MQTT 3.1.1
	// carries no reason codes and the field is empty.
	ReasonCodes []byte
	// Properties holds the MQTT 5 UNSUBACK properties.
	Properties Properties
}

// Type reports UNSUBACK.
func (m Unsuback) Type() byte { return TypeUnsuback }

// Pingreq is a PINGREQ packet. It carries nothing beyond a fixed header.
type Pingreq struct {
	MessageHeader
}

// Type reports PINGREQ.
func (m Pingreq) Type() byte { return TypePingreq }

// Pingresp is a PINGRESP packet. It carries nothing beyond a fixed header.
type Pingresp struct {
	MessageHeader
}

// Type reports PINGRESP.
func (m Pingresp) Type() byte { return TypePingresp }

// Disconnect is a DISCONNECT packet.
type Disconnect struct {
	MessageHeader
	// ReasonCode is the MQTT 5 reason code; MQTT 3.1.1 has no reason code.
	ReasonCode byte
	// Properties holds the MQTT 5 DISCONNECT properties.
	Properties Properties
}

// Type reports DISCONNECT.
func (m Disconnect) Type() byte { return TypeDisconnect }

// packetName returns the specification name of a control packet type for error
// messages.
func packetName(packetType byte) string {
	switch packetType {
	case TypeConnect:
		return "CONNECT"
	case TypeConnack:
		return "CONNACK"
	case TypePublish:
		return "PUBLISH"
	case TypePuback:
		return "PUBACK"
	case TypePubrec:
		return "PUBREC"
	case TypePubrel:
		return "PUBREL"
	case TypePubcomp:
		return "PUBCOMP"
	case TypeSubscribe:
		return "SUBSCRIBE"
	case TypeSuback:
		return "SUBACK"
	case TypeUnsubscribe:
		return "UNSUBSCRIBE"
	case TypeUnsuback:
		return "UNSUBACK"
	case TypePingreq:
		return "PINGREQ"
	case TypePingresp:
		return "PINGRESP"
	case TypeDisconnect:
		return "DISCONNECT"
	default:
		return "unknown packet"
	}
}
