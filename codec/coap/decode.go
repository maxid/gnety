package coap

import (
	"encoding/binary"
	"fmt"

	"github.com/maxid/gnety/codec/wire"
)

// DefaultMaxDatagram is the datagram size bound used by Decoder, Codec and
// ParseLimit when the caller sets none. It is larger than the biggest possible
// UDP payload, so accepting a datagram is decided by the wire format, not by
// this constant.
const DefaultMaxDatagram = 64 << 10

// malformed reports a message that violates the wire format. It wraps
// ErrMalformed, so callers that predate codec/wire keep working, and it carries
// wire.KindMalformed so wire.KindOf classifies it across packages.
func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %w", ErrMalformed, wire.Errorf(wire.KindMalformed, format, args...))
}

// tooLarge reports a datagram that exceeds a configured size limit.
func tooLarge(format string, args ...any) error {
	return fmt.Errorf("%w: %w", ErrMalformed, wire.Errorf(wire.KindTooLarge, format, args...))
}

// unsupported reports a message this package cannot represent, such as a CoAP
// version other than 1.
func unsupported(format string, args ...any) error {
	return fmt.Errorf("%w: %w", ErrMalformed, wire.Errorf(wire.KindUnsupported, format, args...))
}

// Decoder decodes CoAP datagrams into typed messages.
//
// The zero value rejects datagrams larger than DefaultMaxDatagram. Unlike a
// stream codec, a datagram codec cannot wait for more bytes: each datagram is a
// complete frame, so a datagram that ends inside a field is reported as
// malformed rather than incomplete.
type Decoder struct {
	// MaxSize bounds one datagram in bytes. A non-positive value means
	// DefaultMaxDatagram.
	MaxSize int
}

// Decode decodes one datagram.
func (d Decoder) Decode(data []byte) (Message, error) {
	maxSize := d.MaxSize
	if maxSize <= 0 {
		maxSize = DefaultMaxDatagram
	}
	return ParseLimit(data, maxSize)
}

// ParseLimit decodes one datagram and rejects a datagram larger than maxSize
// bytes before any field is copied. A non-positive maxSize means
// DefaultMaxDatagram; Parse applies no bound at all.
func ParseLimit(data []byte, maxSize int) (Message, error) {
	if maxSize <= 0 {
		maxSize = DefaultMaxDatagram
	}
	if len(data) > maxSize {
		return Message{}, tooLarge("datagram of %d bytes exceeds the limit of %d", len(data), maxSize)
	}
	return parse(data)
}

// Parse decodes one CoAP datagram.
//
// Every error wraps ErrMalformed and carries a codec/wire kind: wire.KindMalformed
// for bytes the wire format does not allow and wire.KindUnsupported for a
// version other than 1. Use ParseLimit or Decoder for a size bound.
//
// Decoded tokens, option values and payloads are freshly allocated, so the
// caller may reuse the datagram buffer while keeping the message.
func Parse(data []byte) (Message, error) { return parse(data) }

// parse decodes one datagram without a size limit.
func parse(data []byte) (Message, error) {
	var m Message
	if len(data) < 4 {
		return m, malformed("datagram of %d bytes is shorter than the 4-byte header", len(data))
	}
	m.Version = data[0] >> 6
	if m.Version != Version {
		return m, unsupported("version %d", m.Version)
	}
	m.Type = Type((data[0] >> 4) & 0x03)
	tokenLen := int(data[0] & 0x0f)
	if tokenLen > 8 {
		return m, malformed("token length %d is reserved", tokenLen)
	}
	if len(data) < 4+tokenLen {
		return m, malformed("datagram of %d bytes is shorter than the 4-byte header and %d-byte token", len(data), tokenLen)
	}
	m.Code = Code(data[1])
	m.MessageID = binary.BigEndian.Uint16(data[2:4])
	m.Token = append([]byte(nil), data[4:4+tokenLen]...)

	rest := data[4+tokenLen:]
	number := 0
	for len(rest) > 0 {
		if rest[0] == 0xff {
			if len(rest) == 1 {
				return m, malformed("payload marker with no payload")
			}
			m.Payload = append([]byte(nil), rest[1:]...)
			return m, nil
		}
		delta, length, consumed, err := parseOptionHeader(rest)
		if err != nil {
			return m, err
		}
		rest = rest[consumed:]
		if length > len(rest) {
			return m, malformed("option %d declares %d value bytes but the datagram carries %d", number+delta, length, len(rest))
		}
		number += delta
		if number > 0xffff {
			return m, malformed("option number %d exceeds 65535", number)
		}
		m.Options = append(m.Options, Option{Number: uint16(number), Value: append([]byte(nil), rest[:length]...)})
		rest = rest[length:]
	}
	return m, nil
}

// parseOptionHeader decodes the delta and length nibbles of one option,
// including their extended forms, and reports how many header bytes it used.
//
// An extended delta cannot be non-canonical: nibble 13 covers the deltas 13 to
// 268 and nibble 14 covers 269 to 65804, two disjoint ranges, so every delta
// has exactly one encoding. The same holds for lengths. Nibble 15 is reserved
// in both fields; the payload marker is 0xff as a whole byte and is handled by
// the caller.
func parseOptionHeader(b []byte) (delta, length, consumed int, err error) {
	if len(b) < 1 {
		return 0, 0, 0, malformed("truncated option header")
	}
	deltaNibble := b[0] >> 4
	lengthNibble := b[0] & 0x0f
	consumed = 1
	readExtended := func(field string, nibble byte) (int, error) {
		switch nibble {
		case 13:
			if len(b) < consumed+1 {
				return 0, malformed("truncated %s extension", field)
			}
			value := int(b[consumed]) + 13
			consumed++
			return value, nil
		case 14:
			if len(b) < consumed+2 {
				return 0, malformed("truncated %s extension", field)
			}
			value := int(binary.BigEndian.Uint16(b[consumed:consumed+2])) + 269
			consumed += 2
			return value, nil
		case 15:
			return 0, malformed("option %s nibble 15 is reserved", field)
		default:
			return int(nibble), nil
		}
	}
	if delta, err = readExtended("delta", deltaNibble); err != nil {
		return 0, 0, 0, err
	}
	if length, err = readExtended("length", lengthNibble); err != nil {
		return 0, 0, 0, err
	}
	return delta, length, consumed, nil
}
