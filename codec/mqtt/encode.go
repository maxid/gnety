package mqtt

import (
	"io"
	"reflect"

	"github.com/maxid/gnety/codec/wire"
)

// errNilMessage reports a nil message, or a typed nil pointer, passed to Encode.
var errNilMessage = malformed("nil message")

// Encode writes m to w as one MQTT control packet.
//
// The protocol level comes from m.Version(): MQTT 5 packets carry property
// blocks and optional reason codes, MQTT 3.1.1 packets carry neither and omit
// the Properties field and the MQTT 5 subscription options. The packet is built
// in memory and written with a single Write call.
//
// Both values and pointers satisfy Message; Decode returns pointers.
func Encode(w io.Writer, m Message) error {
	if w == nil {
		return malformed("nil writer")
	}
	if m == nil {
		return errNilMessage
	}
	if rv := reflect.ValueOf(m); rv.Kind() == reflect.Pointer && rv.IsNil() {
		// Version is a promoted method, so calling it on a typed nil pointer
		// would panic before encodeBody could reject the message.
		return errNilMessage
	}
	version := m.Version()
	if version != Version311 && version != Version5 {
		return unsupported("protocol level %d", version)
	}
	body, flags, err := encodeBody(m, version)
	if err != nil {
		return err
	}
	if len(body) > MaxRemainingLength {
		return tooLarge("%s body of %d bytes exceeds the maximum %d", packetName(m.Type()), len(body), MaxRemainingLength)
	}
	frame := make([]byte, 0, 1+wire.VarintLen(uint64(len(body)))+len(body))
	frame = append(frame, m.Type()<<4|flags)
	frame = AppendRemainingLength(frame, len(body))
	frame = append(frame, body...)
	n, err := w.Write(frame)
	if err != nil {
		return err
	}
	if n != len(frame) {
		return io.ErrShortWrite
	}
	return nil
}

// encodeBody encodes the variable header and payload of one packet and reports
// the fixed-header flags that belong with it.
func encodeBody(m Message, version byte) (body []byte, flags byte, err error) {
	switch v := m.(type) {
	case *Connect:
		body, err = encodeConnect(*v, version)
		return body, 0, err
	case Connect:
		body, err = encodeConnect(v, version)
		return body, 0, err
	case *Connack:
		body, err = encodeConnack(*v, version)
		return body, 0, err
	case Connack:
		body, err = encodeConnack(v, version)
		return body, 0, err
	case *Publish:
		return encodePublish(*v, version)
	case Publish:
		return encodePublish(v, version)
	case *Puback:
		body, err = encodeAck(v.PacketID, v.ReasonCode, v.Properties, version, TypePuback)
		return body, 0, err
	case Puback:
		body, err = encodeAck(v.PacketID, v.ReasonCode, v.Properties, version, TypePuback)
		return body, 0, err
	case *Pubrec:
		body, err = encodeAck(v.PacketID, v.ReasonCode, v.Properties, version, TypePubrec)
		return body, 0, err
	case Pubrec:
		body, err = encodeAck(v.PacketID, v.ReasonCode, v.Properties, version, TypePubrec)
		return body, 0, err
	case *Pubrel:
		body, err = encodeAck(v.PacketID, v.ReasonCode, v.Properties, version, TypePubrel)
		return body, 0x02, err
	case Pubrel:
		body, err = encodeAck(v.PacketID, v.ReasonCode, v.Properties, version, TypePubrel)
		return body, 0x02, err
	case *Pubcomp:
		body, err = encodeAck(v.PacketID, v.ReasonCode, v.Properties, version, TypePubcomp)
		return body, 0, err
	case Pubcomp:
		body, err = encodeAck(v.PacketID, v.ReasonCode, v.Properties, version, TypePubcomp)
		return body, 0, err
	case *Subscribe:
		body, err = encodeSubscribe(*v, version)
		return body, 0x02, err
	case Subscribe:
		body, err = encodeSubscribe(v, version)
		return body, 0x02, err
	case *Suback:
		body, err = encodeSuback(*v, version)
		return body, 0, err
	case Suback:
		body, err = encodeSuback(v, version)
		return body, 0, err
	case *Unsubscribe:
		body, err = encodeUnsubscribe(*v, version)
		return body, 0x02, err
	case Unsubscribe:
		body, err = encodeUnsubscribe(v, version)
		return body, 0x02, err
	case *Unsuback:
		body, err = encodeUnsuback(*v, version)
		return body, 0, err
	case Unsuback:
		body, err = encodeUnsuback(v, version)
		return body, 0, err
	case *Pingreq:
		return nil, 0, nil
	case Pingreq:
		return nil, 0, nil
	case *Pingresp:
		return nil, 0, nil
	case Pingresp:
		return nil, 0, nil
	case *Disconnect:
		body, err = encodeDisconnect(*v, version)
		return body, 0, err
	case Disconnect:
		body, err = encodeDisconnect(v, version)
		return body, 0, err
	case *Auth:
		body, err = encodeAuth(*v, version)
		return body, 0, err
	case Auth:
		body, err = encodeAuth(v, version)
		return body, 0, err
	default:
		return nil, 0, malformed("unsupported message %T", m)
	}
}

// encodeConnect encodes a CONNECT body.
func encodeConnect(m Connect, version byte) ([]byte, error) {
	if m.Will != nil && m.Will.QoS > 2 {
		return nil, malformed("CONNECT will QoS %d is reserved", m.Will.QoS)
	}
	if m.Password != nil && m.Username == nil {
		return nil, malformed("CONNECT carries a password without a user name")
	}

	var flags byte
	if m.CleanSession {
		flags |= 0x02
	}
	if m.Will != nil {
		flags |= 0x04 | m.Will.QoS<<3
		if m.Will.Retain {
			flags |= 0x20
		}
	}
	if m.Password != nil {
		flags |= 0x40
	}
	if m.Username != nil {
		flags |= 0x80
	}

	body := make([]byte, 0, 32+len(m.ClientID))
	body = append(body, 0x00, 0x04, 'M', 'Q', 'T', 'T', version, flags)
	body = appendUint16Raw(body, m.KeepAlive)

	var err error
	if version == Version5 {
		if body, err = encodeProperties(body, m.Properties, TypeConnect); err != nil {
			return nil, err
		}
	}
	if body, err = appendUTF16(body, string(m.ClientID)); err != nil {
		return nil, err
	}
	if m.Will != nil {
		if version == Version5 {
			if body, err = appendProperties(body, m.Will.Properties, ctxWill, "will"); err != nil {
				return nil, err
			}
		}
		if body, err = appendUTF16(body, m.Will.Topic); err != nil {
			return nil, err
		}
		if body, err = appendRaw16(body, m.Will.Payload); err != nil {
			return nil, err
		}
	}
	if m.Username != nil {
		if body, err = appendUTF16(body, string(m.Username)); err != nil {
			return nil, err
		}
	}
	if m.Password != nil {
		if body, err = appendRaw16(body, m.Password); err != nil {
			return nil, err
		}
	}
	return body, nil
}

// encodeConnack encodes a CONNACK body.
func encodeConnack(m Connack, version byte) ([]byte, error) {
	var flags byte
	if m.SessionPresent {
		flags = 0x01
	}
	body := []byte{flags, m.ReasonCode}
	if version != Version5 {
		return body, nil
	}
	body, err := encodeProperties(body, m.Properties, TypeConnack)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// encodePublish encodes a PUBLISH body and its fixed-header flags.
func encodePublish(m Publish, version byte) ([]byte, byte, error) {
	if m.QoS > 2 {
		return nil, 0, malformed("PUBLISH QoS %d is reserved", m.QoS)
	}
	if m.QoS > 0 && m.PacketID == 0 {
		return nil, 0, malformed("PUBLISH with QoS %d carries packet id 0", m.QoS)
	}

	var flags byte
	if m.Retain {
		flags |= 0x01
	}
	flags |= m.QoS << 1
	if m.Dup {
		flags |= 0x08
	}

	body := make([]byte, 0, len(m.Topic)+len(m.Payload)+8)
	var err error
	if body, err = appendUTF16(body, m.Topic); err != nil {
		return nil, 0, err
	}
	if m.QoS > 0 {
		body = appendUint16Raw(body, m.PacketID)
	}
	if version == Version5 {
		if body, err = encodeProperties(body, m.Properties, TypePublish); err != nil {
			return nil, 0, err
		}
	}
	body = append(body, m.Payload...)
	return body, flags, nil
}

// encodeAck encodes the variable header of the PUBACK family: packet
// identifier, and on MQTT 5 the optional reason code and property block. The
// reason code and properties are both omitted when they are at their defaults,
// which is why some MQTT 5 acknowledgements are two bytes long.
func encodeAck(packetID uint16, reason byte, props Properties, version byte, packetType byte) ([]byte, error) {
	if packetID == 0 {
		return nil, malformed("%s carries packet id 0", packetName(packetType))
	}
	body := make([]byte, 0, 8)
	body = appendUint16Raw(body, packetID)
	if version != Version5 {
		return body, nil
	}
	block, err := encodeProperties(nil, props, packetType)
	if err != nil {
		return nil, err
	}
	// An empty property block is encoded by its absence, which also allows the
	// reason code byte to be dropped when it is 0.
	empty := len(block) == 1
	if reason == 0 && empty {
		return body, nil
	}
	body = append(body, reason)
	if empty {
		return body, nil
	}
	return append(body, block...), nil
}

// encodeSubscribe encodes a SUBSCRIBE body.
func encodeSubscribe(m Subscribe, version byte) ([]byte, error) {
	if m.PacketID == 0 {
		return nil, malformed("SUBSCRIBE carries packet id 0")
	}
	if len(m.Subscriptions) == 0 {
		return nil, malformed("SUBSCRIBE carries no topic filters")
	}
	body := make([]byte, 0, 8+16*len(m.Subscriptions))
	body = appendUint16Raw(body, m.PacketID)
	var err error
	if version == Version5 {
		if body, err = encodeProperties(body, m.Properties, TypeSubscribe); err != nil {
			return nil, err
		}
	}
	for _, s := range m.Subscriptions {
		if s.QoS > 2 {
			return nil, malformed("SUBSCRIBE filter %q requests reserved QoS %d", s.Filter, s.QoS)
		}
		options := s.QoS
		if version == Version5 {
			if s.RetainHandling > 2 {
				return nil, malformed("SUBSCRIBE filter %q sets reserved retain handling %d", s.Filter, s.RetainHandling)
			}
			options |= s.RetainHandling << 4
			if s.NoLocal {
				options |= 0x04
			}
			if s.RetainAsPublished {
				options |= 0x08
			}
		}
		if body, err = appendUTF16(body, s.Filter); err != nil {
			return nil, err
		}
		body = append(body, options)
	}
	return body, nil
}

// encodeSuback encodes a SUBACK body.
func encodeSuback(m Suback, version byte) ([]byte, error) {
	if m.PacketID == 0 {
		return nil, malformed("SUBACK carries packet id 0")
	}
	if len(m.ReasonCodes) == 0 {
		return nil, malformed("SUBACK carries no reason codes")
	}
	body := make([]byte, 0, 4+len(m.ReasonCodes))
	body = appendUint16Raw(body, m.PacketID)
	if version == Version5 {
		var err error
		if body, err = encodeProperties(body, m.Properties, TypeSuback); err != nil {
			return nil, err
		}
	}
	return append(body, m.ReasonCodes...), nil
}

// encodeUnsubscribe encodes an UNSUBSCRIBE body.
func encodeUnsubscribe(m Unsubscribe, version byte) ([]byte, error) {
	if m.PacketID == 0 {
		return nil, malformed("UNSUBSCRIBE carries packet id 0")
	}
	if len(m.Filters) == 0 {
		return nil, malformed("UNSUBSCRIBE carries no topic filters")
	}
	body := make([]byte, 0, 8+16*len(m.Filters))
	body = appendUint16Raw(body, m.PacketID)
	var err error
	if version == Version5 {
		if body, err = encodeProperties(body, m.Properties, TypeUnsubscribe); err != nil {
			return nil, err
		}
	}
	for _, filter := range m.Filters {
		if body, err = appendUTF16(body, filter); err != nil {
			return nil, err
		}
	}
	return body, nil
}

// encodeUnsuback encodes an UNSUBACK body.
func encodeUnsuback(m Unsuback, version byte) ([]byte, error) {
	if m.PacketID == 0 {
		return nil, malformed("UNSUBACK carries packet id 0")
	}
	body := make([]byte, 0, 4+len(m.ReasonCodes))
	body = appendUint16Raw(body, m.PacketID)
	if version != Version5 {
		// MQTT 3.1.1 UNSUBACK ends after the packet identifier.
		return body, nil
	}
	if len(m.ReasonCodes) == 0 {
		return nil, malformed("UNSUBACK carries no reason codes")
	}
	var err error
	if body, err = encodeProperties(body, m.Properties, TypeUnsuback); err != nil {
		return nil, err
	}
	return append(body, m.ReasonCodes...), nil
}

// encodeDisconnect encodes a DISCONNECT body. MQTT 3.1.1 has none, and MQTT 5
// omits the reason code and properties when both are at their defaults.
func encodeDisconnect(m Disconnect, version byte) ([]byte, error) {
	if version != Version5 {
		return nil, nil
	}
	block, err := encodeProperties(nil, m.Properties, TypeDisconnect)
	if err != nil {
		return nil, err
	}
	empty := len(block) == 1
	if m.ReasonCode == 0 && empty {
		return nil, nil
	}
	body := make([]byte, 0, 1+len(block))
	body = append(body, m.ReasonCode)
	if empty {
		return body, nil
	}
	return append(body, block...), nil
}

// encodeAuth encodes an AUTH body. MQTT 5 omits the reason code and the
// property block when both are at their defaults, so a successful AUTH with no
// properties has no body at all and a reason code alone stays one byte.
func encodeAuth(m Auth, version byte) ([]byte, error) {
	if version != Version5 {
		return nil, unsupported("AUTH in protocol level %d", version)
	}
	block, err := encodeProperties(nil, m.Properties, TypeAuth)
	if err != nil {
		return nil, err
	}
	empty := len(block) == 1
	if m.ReasonCode == 0 && empty {
		return nil, nil
	}
	body := make([]byte, 0, 1+len(block))
	body = append(body, m.ReasonCode)
	if empty {
		return body, nil
	}
	return append(body, block...), nil
}

// appendUint16Raw appends a bare big-endian uint16.
func appendUint16Raw(dst []byte, v uint16) []byte {
	return append(dst, byte(v>>8), byte(v))
}

// appendUTF16 appends a length-prefixed UTF-8 string, validating it against the
// MQTT string rules and the 65535-byte length limit.
func appendUTF16(dst []byte, s string) ([]byte, error) {
	if err := checkString(s); err != nil {
		return nil, err
	}
	if len(s) > 0xffff {
		return nil, tooLarge("string of %d bytes exceeds 65535", len(s))
	}
	dst = append(dst, byte(len(s)>>8), byte(len(s)))
	return append(dst, s...), nil
}

// appendRaw16 appends a length-prefixed binary value.
func appendRaw16(dst []byte, b []byte) ([]byte, error) {
	if len(b) > 0xffff {
		return nil, tooLarge("binary value of %d bytes exceeds 65535", len(b))
	}
	dst = append(dst, byte(len(b)>>8), byte(len(b)))
	return append(dst, b...), nil
}
