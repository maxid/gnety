package coap

import (
	"encoding/binary"
	"sort"
)

// Marshal encodes the message as one CoAP datagram.
//
// Marshal is the permissive encoder Parse has always been paired with: it sorts
// Options by number before encoding, so a hand-built list in any order produces
// the ascending order the wire format requires, and it does not reject a
// repeated option. Encode is the strict alternative.
//
// A zero Version is treated as Version, and Type is masked to its two bits,
// which is what the wire format carries. A token longer than 8 bytes and a
// version other than 1 are errors.
func (m Message) Marshal() ([]byte, error) {
	options := make([]Option, len(m.Options))
	copy(options, m.Options)
	sort.SliceStable(options, func(i, j int) bool { return options[i].Number < options[j].Number })
	return m.appendWire(options)
}

// Encode encodes the message as one CoAP datagram after validating it.
//
// Unlike Marshal, Encode requires Options to be sorted by number and rejects a
// second instance of an option that is not repeatable, an option value longer
// than its definition allows, a message type outside the two-bit field and a
// token longer than 8 bytes. Messages built with the typed accessors already
// satisfy these rules.
func Encode(m Message) ([]byte, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	return m.appendWire(m.Options)
}

// validate applies the rules that Encode enforces on top of the wire format.
func (m Message) validate() error {
	if m.Type > Reset {
		return malformed("message type %d is outside the two-bit field", uint8(m.Type))
	}
	last := -1
	seen := make(map[uint16]bool, len(m.Options))
	for _, opt := range m.Options {
		if int(opt.Number) < last {
			return malformed("option %d follows option %d; options must be in ascending number order", opt.Number, last)
		}
		if def, known := optionDefs[opt.Number]; known {
			if len(opt.Value) > def.maxLen {
				return malformed("%s value of %d bytes exceeds %d", def.name, len(opt.Value), def.maxLen)
			}
			if !def.repeatable && seen[opt.Number] {
				return malformed("%s is not repeatable but appears more than once", def.name)
			}
		}
		seen[opt.Number] = true
		last = int(opt.Number)
	}
	return nil
}

// appendWire encodes the header, token, the options in the order given and the
// payload.
func (m Message) appendWire(options []Option) ([]byte, error) {
	if len(m.Token) > 8 {
		return nil, malformed("token of %d bytes exceeds 8", len(m.Token))
	}
	version := m.Version
	if version == 0 {
		version = Version
	}
	if version != Version {
		return nil, unsupported("version %d", version)
	}
	out := make([]byte, 4, 16+len(m.Token)+len(m.Payload))
	out[0] = version<<6 | byte(m.Type&0x03)<<4 | byte(len(m.Token))
	out[1] = byte(m.Code)
	binary.BigEndian.PutUint16(out[2:4], m.MessageID)
	out = append(out, m.Token...)

	last := 0
	for _, opt := range options {
		delta := int(opt.Number) - last
		if delta < 0 {
			return nil, malformed("option %d is below the previous option %d", opt.Number, last)
		}
		out = appendOption(out, delta, opt.Value)
		last = int(opt.Number)
	}
	if len(m.Payload) > 0 {
		out = append(out, 0xff)
		out = append(out, m.Payload...)
	}
	return out, nil
}

// appendOption appends one option header and its value.
func appendOption(dst []byte, delta int, value []byte) []byte {
	deltaNibble, deltaExt := optionNibble(delta)
	lengthNibble, lengthExt := optionNibble(len(value))
	dst = append(dst, deltaNibble<<4|lengthNibble)
	dst = append(dst, deltaExt...)
	dst = append(dst, lengthExt...)
	return append(dst, value...)
}

// optionNibble returns the nibble and the extended bytes that encode v in the
// shortest form the wire format defines.
func optionNibble(v int) (byte, []byte) {
	switch {
	case v < 13:
		return byte(v), nil
	case v < 269:
		return 13, []byte{byte(v - 13)}
	default:
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(v-269))
		return 14, ext[:]
	}
}

// Codec adapts the typed codec to gnety's DatagramCodec shape: Decode turns one
// datagram into a Message and Encode turns a Message back into one.
type Codec struct {
	// MaxSize bounds an inbound datagram in bytes. A non-positive value means
	// DefaultMaxDatagram.
	MaxSize int
}

// Decode decodes one datagram.
func (c Codec) Decode(data []byte) (any, error) {
	return Decoder{MaxSize: c.MaxSize}.Decode(data)
}

// Encode encodes one message. Both Message and *Message are accepted.
func (c Codec) Encode(msg any) ([]byte, error) {
	switch v := msg.(type) {
	case Message:
		return Encode(v)
	case *Message:
		if v == nil {
			return nil, malformed("nil message")
		}
		return Encode(*v)
	default:
		return nil, unsupported("codec expects coap.Message, got %T", msg)
	}
}
