package mqtt

import (
	"bufio"
	"encoding/binary"
	"fmt"

	"github.com/maxid/gnety/codec/wire"
)

// DefaultMaxPacketSize bounds a packet body when Decoder.MaxPacketSize is not
// set. It matches codec/wire's default frame budget.
const DefaultMaxPacketSize = wire.DefaultMaxFrame

// malformed reports a packet that violates the wire format. It satisfies
// errors.Is(err, ErrMalformedPacket) like every other decoding failure in this
// package, and carries wire.KindMalformed so codec/wire's classification works
// across packages.
func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %w", ErrMalformedPacket, wire.Errorf(wire.KindMalformed, format, args...))
}

// tooLarge reports a packet that is well formed but exceeds a configured limit.
func tooLarge(format string, args ...any) error {
	return fmt.Errorf("%w: %w", ErrMalformedPacket, wire.Errorf(wire.KindTooLarge, format, args...))
}

// unsupported reports a packet this package cannot represent, such as a
// protocol level other than 4 or 5.
func unsupported(format string, args ...any) error {
	return fmt.Errorf("%w: %w", ErrMalformedPacket, wire.Errorf(wire.KindUnsupported, format, args...))
}

// wrapErr adds the package sentinel to an error from codec/wire. Incomplete
// input keeps its own classification so the caller can wait for more bytes
// instead of treating the peer as broken.
func wrapErr(err error) error {
	switch wire.KindOf(err) {
	case wire.KindIncomplete, wire.KindUnknown:
		return err
	default:
		return fmt.Errorf("%w: %w", ErrMalformedPacket, err)
	}
}

// Decoder decodes MQTT control packets from a byte stream.
//
// The zero value decodes MQTT 3.1.1 packets of up to DefaultMaxPacketSize bytes.
// A server decodes CONNECT first, which refreshes Version from the packet; a
// client that knows the negotiated level sets Version before decoding CONNACK.
//
// Decode reads exactly one packet per call and never allocates from an
// unvalidated length: the declared remaining length is checked against
// MaxPacketSize before any body buffer exists. A packet whose body does not
// consume its declared remaining length is rejected, which is one way an MQTT 5
// property block inside a 3.1.1 packet is caught.
type Decoder struct {
	// Version is the MQTT protocol level used for packets that do not carry one
	// themselves: Version311 or Version5. Zero means Version311. Decoding
	// CONNECT overwrites it with the level the client asked for.
	Version byte
	// MaxPacketSize bounds the body (remaining length) of one packet in bytes. A
	// non-positive value means DefaultMaxPacketSize.
	MaxPacketSize int
}

// Decode reads and decodes one control packet.
//
// A clean end of stream before the fixed header is reported as io.EOF. A stream
// that ends inside a packet is reported as wire.ErrIncomplete (still satisfying
// errors.Is(err, io.EOF)), and a peer that sends something the wire format does
// not allow is reported as ErrMalformedPacket. After a malformed packet the
// stream position is undefined; the caller closes the connection.
func (d *Decoder) Decode(r *bufio.Reader) (Message, error) {
	header, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	packetType := header >> 4
	flags := header & 0x0f
	if packetType < TypeConnect || packetType > TypeAuth {
		return nil, malformed("unknown control packet type %d", packetType)
	}
	remaining, err := ReadRemainingLength(r)
	if err != nil {
		return nil, err
	}
	maxSize := d.MaxPacketSize
	if maxSize <= 0 {
		maxSize = DefaultMaxPacketSize
	}
	if remaining > maxSize {
		return nil, tooLarge("%s body of %d bytes exceeds MaxPacketSize %d", packetName(packetType), remaining, maxSize)
	}
	if err := checkFlags(packetType, flags); err != nil {
		return nil, err
	}
	version := d.Version
	if version == 0 {
		version = Version311
	}
	if version != Version311 && version != Version5 {
		return nil, unsupported("protocol level %d", version)
	}

	// The body reader is bounded by the remaining length, so every field read is
	// budget-checked and a truncated packet reports ErrIncomplete. A zero
	// remaining length is a real budget of zero: any read at all then fails
	// instead of blocking on the next packet's bytes.
	rd := wire.NewReader(r, remaining)
	m, err := decodeBody(rd, packetType, flags, version)
	if err != nil {
		return nil, err
	}
	if consumed := remaining - rd.Budget(); consumed != remaining {
		return nil, malformed("%s decodes %d body bytes but its remaining length is %d", packetName(packetType), consumed, remaining)
	}
	if c, ok := m.(*Connect); ok {
		d.Version = c.Version()
	}
	return m, nil
}

// checkFlags rejects fixed-header flags the specification reserves, including
// the mandatory 0b0010 of PUBREL, SUBSCRIBE and UNSUBSCRIBE.
func checkFlags(packetType, flags byte) error {
	switch packetType {
	case TypePublish:
		qos := (flags >> 1) & 0x03
		if qos > 2 {
			return malformed("PUBLISH QoS %d is reserved", qos)
		}
		if qos == 0 && flags&0x08 != 0 {
			return malformed("PUBLISH with QoS 0 must not set DUP")
		}
	case TypePubrel, TypeSubscribe, TypeUnsubscribe:
		if flags != 0x02 {
			return malformed("%s reserved flags 0x%x, want 0x2", packetName(packetType), flags)
		}
	default:
		if flags != 0 {
			return malformed("%s reserved flags 0x%x, want 0x0", packetName(packetType), flags)
		}
	}
	return nil
}

// decodeBody dispatches to the decoder of one control packet type.
func decodeBody(rd *wire.Reader, packetType, flags, version byte) (Message, error) {
	switch packetType {
	case TypeConnect:
		return decodeConnect(rd)
	case TypeConnack:
		return decodeConnack(rd, version)
	case TypePublish:
		return decodePublish(rd, flags, version)
	case TypePuback:
		return decodePuback(rd, version)
	case TypePubrec:
		return decodePubrec(rd, version)
	case TypePubrel:
		return decodePubrel(rd, version)
	case TypePubcomp:
		return decodePubcomp(rd, version)
	case TypeSubscribe:
		return decodeSubscribe(rd, version)
	case TypeSuback:
		return decodeSuback(rd, version)
	case TypeUnsubscribe:
		return decodeUnsubscribe(rd, version)
	case TypeUnsuback:
		return decodeUnsuback(rd, version)
	case TypePingreq:
		return &Pingreq{MessageHeader: MessageHeader{Level: version}}, nil
	case TypePingresp:
		return &Pingresp{MessageHeader: MessageHeader{Level: version}}, nil
	case TypeDisconnect:
		return decodeDisconnect(rd, version)
	case TypeAuth:
		return decodeAuth(rd, version)
	default:
		return nil, malformed("unknown control packet type %d", packetType)
	}
}

// readString reads one UTF-8 string field.
func readString(rd *wire.Reader) (string, error) {
	s, err := rd.String16(binary.BigEndian)
	if err != nil {
		return "", wrapErr(err)
	}
	if err := checkString(s); err != nil {
		return "", err
	}
	return s, nil
}

// readPropertyLength reads the property length prefix of a property block.
//
// It consumes the variable byte integer one byte at a time through the
// budget-checked reader, so a body that ends inside the prefix reports
// ErrIncomplete instead of reading into the next packet, and then applies the
// shared shortest-form rule through takeVarint.
func readPropertyLength(rd *wire.Reader) (int, error) {
	var buf [wire.MaxVarintBytes]byte
	for n := 1; n <= len(buf); n++ {
		b, err := rd.Byte()
		if err != nil {
			return 0, wrapErr(err)
		}
		buf[n-1] = b
		if b&0x80 == 0 {
			length, _, err := takeVarint(buf[:n])
			if err != nil {
				return 0, err
			}
			return length, nil
		}
	}
	return 0, malformed("property length is wider than %d bytes", wire.MaxVarintBytes)
}

// readProperties reads one property block from rd.
func readProperties(rd *wire.Reader, ctx propCtx, where string) (Properties, error) {
	n, err := readPropertyLength(rd)
	if err != nil {
		return Properties{}, err
	}
	if n > rd.Budget() {
		return Properties{}, malformed("%s declares %d property bytes but its body carries %d", where, n, rd.Budget())
	}
	block, err := rd.ReadN(n)
	if err != nil {
		return Properties{}, wrapErr(err)
	}
	return decodePropertyBlock(block, ctx, where)
}

// decodeConnect decodes a CONNECT body. CONNECT carries its own protocol level,
// so it does not take the level from the Decoder.
func decodeConnect(rd *wire.Reader) (*Connect, error) {
	name, err := readString(rd)
	if err != nil {
		return nil, err
	}
	if name != "MQTT" {
		return nil, unsupported("CONNECT protocol name %q", name)
	}
	level, err := rd.Byte()
	if err != nil {
		return nil, wrapErr(err)
	}
	if level != Version311 && level != Version5 {
		return nil, unsupported("CONNECT protocol level %d", level)
	}
	raw, err := rd.Byte()
	if err != nil {
		return nil, wrapErr(err)
	}
	if raw&0x01 != 0 {
		return nil, malformed("CONNECT reserved connect flag is set")
	}
	willFlag := raw&0x04 != 0
	willQoS := (raw >> 3) & 0x03
	willRetain := raw&0x20 != 0
	hasPassword := raw&0x40 != 0
	hasUsername := raw&0x80 != 0
	if willQoS > 2 {
		return nil, malformed("CONNECT will QoS %d is reserved", willQoS)
	}
	if !willFlag && (willQoS != 0 || willRetain) {
		return nil, malformed("CONNECT sets will QoS or will retain without the will flag")
	}
	if hasPassword && !hasUsername {
		return nil, malformed("CONNECT sets the password flag without the user name flag")
	}

	m := &Connect{MessageHeader: MessageHeader{Level: level}}
	m.CleanSession = raw&0x02 != 0
	if m.KeepAlive, err = rd.Uint16(binary.BigEndian); err != nil {
		return nil, wrapErr(err)
	}
	if level == Version5 {
		if m.Properties, err = readProperties(rd, ctxConnect, "CONNECT"); err != nil {
			return nil, err
		}
	}
	clientID, err := readString(rd)
	if err != nil {
		return nil, err
	}
	m.ClientID = []byte(clientID)
	if willFlag {
		will := &Will{QoS: willQoS, Retain: willRetain}
		if level == Version5 {
			if will.Properties, err = readProperties(rd, ctxWill, "will"); err != nil {
				return nil, err
			}
		}
		if will.Topic, err = readString(rd); err != nil {
			return nil, err
		}
		if will.Payload, err = rd.Raw16(binary.BigEndian); err != nil {
			return nil, wrapErr(err)
		}
		m.Will = will
	}
	if hasUsername {
		username, err := readString(rd)
		if err != nil {
			return nil, err
		}
		m.Username = []byte(username)
	}
	if hasPassword {
		if m.Password, err = rd.Raw16(binary.BigEndian); err != nil {
			return nil, wrapErr(err)
		}
	}
	return m, nil
}

// decodeConnack decodes a CONNACK body.
func decodeConnack(rd *wire.Reader, version byte) (*Connack, error) {
	raw, err := rd.Byte()
	if err != nil {
		return nil, wrapErr(err)
	}
	if raw&0xfe != 0 {
		return nil, malformed("CONNACK reserved acknowledge flags 0x%02x", raw&0xfe)
	}
	m := &Connack{MessageHeader: MessageHeader{Level: version}, SessionPresent: raw&0x01 != 0}
	if m.ReasonCode, err = rd.Byte(); err != nil {
		return nil, wrapErr(err)
	}
	if version == Version5 {
		if m.Properties, err = readProperties(rd, ctxConnack, "CONNACK"); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// decodePublish decodes a PUBLISH body. The fixed header flags select QoS, DUP
// and RETAIN.
func decodePublish(rd *wire.Reader, flags, version byte) (*Publish, error) {
	qos := (flags >> 1) & 0x03
	m := &Publish{
		MessageHeader: MessageHeader{Level: version},
		QoS:           qos,
		Dup:           flags&0x08 != 0,
		Retain:        flags&0x01 != 0,
	}
	var err error
	if m.Topic, err = readString(rd); err != nil {
		return nil, err
	}
	// QoS 0 has no packet identifier on the wire: PacketID stays zero and every
	// remaining byte belongs to the payload.
	if qos > 0 {
		if m.PacketID, err = rd.Uint16(binary.BigEndian); err != nil {
			return nil, wrapErr(err)
		}
		if m.PacketID == 0 {
			return nil, malformed("PUBLISH with QoS %d carries packet id 0", qos)
		}
	}
	if version == Version5 {
		if m.Properties, err = readProperties(rd, ctxPublish, "PUBLISH"); err != nil {
			return nil, err
		}
	}
	if m.Payload, err = rd.ReadN(rd.Budget()); err != nil {
		return nil, wrapErr(err)
	}
	return m, nil
}

// decodeAckFields decodes the variable header shared by the PUBACK family:
// packet identifier, and on MQTT 5 the optional reason code and properties.
func decodeAckFields(rd *wire.Reader, version byte, packetType byte) (uint16, byte, Properties, error) {
	packetID, err := rd.Uint16(binary.BigEndian)
	if err != nil {
		return 0, 0, Properties{}, wrapErr(err)
	}
	if packetID == 0 {
		return 0, 0, Properties{}, malformed("%s carries packet id 0", packetName(packetType))
	}
	if version != Version5 || rd.Budget() == 0 {
		// MQTT 3.1.1 acks stop after the packet identifier, and MQTT 5 allows
		// the reason code and the properties to be omitted when both are at
		// their defaults.
		return packetID, 0, Properties{}, nil
	}
	reason, err := rd.Byte()
	if err != nil {
		return 0, 0, Properties{}, wrapErr(err)
	}
	var props Properties
	if rd.Budget() > 0 {
		if props, err = readProperties(rd, ctxPuback, packetName(packetType)); err != nil {
			return 0, 0, Properties{}, err
		}
	}
	return packetID, reason, props, nil
}

// decodePuback decodes a PUBACK body.
func decodePuback(rd *wire.Reader, version byte) (*Puback, error) {
	id, reason, props, err := decodeAckFields(rd, version, TypePuback)
	if err != nil {
		return nil, err
	}
	return &Puback{
		MessageHeader: MessageHeader{Level: version},
		PacketID:      id,
		ReasonCode:    reason,
		Properties:    props,
	}, nil
}

// decodePubrec decodes a PUBREC body.
func decodePubrec(rd *wire.Reader, version byte) (*Pubrec, error) {
	id, reason, props, err := decodeAckFields(rd, version, TypePubrec)
	if err != nil {
		return nil, err
	}
	return &Pubrec{
		MessageHeader: MessageHeader{Level: version},
		PacketID:      id,
		ReasonCode:    reason,
		Properties:    props,
	}, nil
}

// decodePubrel decodes a PUBREL body.
func decodePubrel(rd *wire.Reader, version byte) (*Pubrel, error) {
	id, reason, props, err := decodeAckFields(rd, version, TypePubrel)
	if err != nil {
		return nil, err
	}
	return &Pubrel{
		MessageHeader: MessageHeader{Level: version},
		PacketID:      id,
		ReasonCode:    reason,
		Properties:    props,
	}, nil
}

// decodePubcomp decodes a PUBCOMP body.
func decodePubcomp(rd *wire.Reader, version byte) (*Pubcomp, error) {
	id, reason, props, err := decodeAckFields(rd, version, TypePubcomp)
	if err != nil {
		return nil, err
	}
	return &Pubcomp{
		MessageHeader: MessageHeader{Level: version},
		PacketID:      id,
		ReasonCode:    reason,
		Properties:    props,
	}, nil
}

// decodeSubscribe decodes a SUBSCRIBE body: packet identifier, properties and
// at least one filter with its subscription options.
func decodeSubscribe(rd *wire.Reader, version byte) (*Subscribe, error) {
	m := &Subscribe{MessageHeader: MessageHeader{Level: version}}
	var err error
	if m.PacketID, err = rd.Uint16(binary.BigEndian); err != nil {
		return nil, wrapErr(err)
	}
	if m.PacketID == 0 {
		return nil, malformed("SUBSCRIBE carries packet id 0")
	}
	if version == Version5 {
		if m.Properties, err = readProperties(rd, ctxSubscribe, "SUBSCRIBE"); err != nil {
			return nil, err
		}
	}
	for rd.Budget() > 0 {
		filter, err := readString(rd)
		if err != nil {
			return nil, err
		}
		raw, err := rd.Byte()
		if err != nil {
			return nil, wrapErr(err)
		}
		s := Subscription{Filter: filter, QoS: raw & 0x03}
		if s.QoS > 2 {
			return nil, malformed("SUBSCRIBE filter %q requests reserved QoS %d", filter, s.QoS)
		}
		if version == Version5 {
			if raw&0xc0 != 0 {
				return nil, malformed("SUBSCRIBE filter %q sets reserved option bits 0x%02x", filter, raw&0xc0)
			}
			s.NoLocal = raw&0x04 != 0
			s.RetainAsPublished = raw&0x08 != 0
			s.RetainHandling = (raw >> 4) & 0x03
			if s.RetainHandling == 3 {
				return nil, malformed("SUBSCRIBE filter %q sets reserved retain handling 3", filter)
			}
		} else if raw&0xfc != 0 {
			// MQTT 3.1.1 reserves everything above the QoS bits.
			return nil, malformed("SUBSCRIBE filter %q sets 3.1.1 option bits 0x%02x", filter, raw&0xfc)
		}
		m.Subscriptions = append(m.Subscriptions, s)
	}
	if len(m.Subscriptions) == 0 {
		return nil, malformed("SUBSCRIBE carries no topic filters")
	}
	return m, nil
}

// decodeSuback decodes a SUBACK body.
func decodeSuback(rd *wire.Reader, version byte) (*Suback, error) {
	m := &Suback{MessageHeader: MessageHeader{Level: version}}
	var err error
	if m.PacketID, err = rd.Uint16(binary.BigEndian); err != nil {
		return nil, wrapErr(err)
	}
	if m.PacketID == 0 {
		return nil, malformed("SUBACK carries packet id 0")
	}
	if version == Version5 {
		if m.Properties, err = readProperties(rd, ctxSuback, "SUBACK"); err != nil {
			return nil, err
		}
	}
	if m.ReasonCodes, err = rd.ReadN(rd.Budget()); err != nil {
		return nil, wrapErr(err)
	}
	if len(m.ReasonCodes) == 0 {
		return nil, malformed("SUBACK carries no reason codes")
	}
	return m, nil
}

// decodeUnsubscribe decodes an UNSUBSCRIBE body.
func decodeUnsubscribe(rd *wire.Reader, version byte) (*Unsubscribe, error) {
	m := &Unsubscribe{MessageHeader: MessageHeader{Level: version}}
	var err error
	if m.PacketID, err = rd.Uint16(binary.BigEndian); err != nil {
		return nil, wrapErr(err)
	}
	if m.PacketID == 0 {
		return nil, malformed("UNSUBSCRIBE carries packet id 0")
	}
	if version == Version5 {
		if m.Properties, err = readProperties(rd, ctxUnsubscribe, "UNSUBSCRIBE"); err != nil {
			return nil, err
		}
	}
	for rd.Budget() > 0 {
		filter, err := readString(rd)
		if err != nil {
			return nil, err
		}
		m.Filters = append(m.Filters, filter)
	}
	if len(m.Filters) == 0 {
		return nil, malformed("UNSUBSCRIBE carries no topic filters")
	}
	return m, nil
}

// decodeUnsuback decodes an UNSUBACK body.
func decodeUnsuback(rd *wire.Reader, version byte) (*Unsuback, error) {
	m := &Unsuback{MessageHeader: MessageHeader{Level: version}}
	var err error
	if m.PacketID, err = rd.Uint16(binary.BigEndian); err != nil {
		return nil, wrapErr(err)
	}
	if m.PacketID == 0 {
		return nil, malformed("UNSUBACK carries packet id 0")
	}
	if version != Version5 {
		// MQTT 3.1.1 UNSUBACK has no payload; leftover bytes are caught by the
		// trailing-byte check in Decode.
		return m, nil
	}
	if m.Properties, err = readProperties(rd, ctxUnsuback, "UNSUBACK"); err != nil {
		return nil, err
	}
	if m.ReasonCodes, err = rd.ReadN(rd.Budget()); err != nil {
		return nil, wrapErr(err)
	}
	if len(m.ReasonCodes) == 0 {
		return nil, malformed("UNSUBACK carries no reason codes")
	}
	return m, nil
}

// decodeDisconnect decodes a DISCONNECT body.
func decodeDisconnect(rd *wire.Reader, version byte) (*Disconnect, error) {
	m := &Disconnect{MessageHeader: MessageHeader{Level: version}}
	if version != Version5 || rd.Budget() == 0 {
		// MQTT 3.1.1 DISCONNECT has no body; leftover bytes are caught by the
		// trailing-byte check in Decode.
		return m, nil
	}
	var err error
	if m.ReasonCode, err = rd.Byte(); err != nil {
		return nil, wrapErr(err)
	}
	if rd.Budget() > 0 {
		if m.Properties, err = readProperties(rd, ctxDisconnect, "DISCONNECT"); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// decodeAuth decodes an AUTH body.
//
// MQTT 5 lets the reason code and the property block be omitted when both are at
// their defaults, so a zero-length body is success with no properties and a
// one-byte body is a bare reason code.
func decodeAuth(rd *wire.Reader, version byte) (*Auth, error) {
	if version != Version5 {
		return nil, unsupported("AUTH in protocol level %d", version)
	}
	m := &Auth{MessageHeader: MessageHeader{Level: version}}
	if rd.Budget() == 0 {
		return m, nil
	}
	var err error
	if m.ReasonCode, err = rd.Byte(); err != nil {
		return nil, wrapErr(err)
	}
	if rd.Budget() > 0 {
		if m.Properties, err = readProperties(rd, ctxAuth, "AUTH"); err != nil {
			return nil, err
		}
	}
	return m, nil
}
