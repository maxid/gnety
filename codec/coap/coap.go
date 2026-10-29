// Package coap implements the CoAP (RFC 7252) wire format.
//
// It is a wire-format codec only: no retransmission, observe, block-wise or
// registration state machines. Those belong to protocol libraries (gcoap,
// glwm2m) built on gnety.
package coap

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

// Version is the CoAP version implemented by this package.
const Version = 1

// Type is the CoAP message type.
type Type uint8

const (
	Confirmable Type = iota
	NonConfirmable
	Acknowledgement
	Reset
)

// Code is the CoAP request/response code (class<<5 | detail).
type Code uint8

// Request codes.
const (
	CodeEmpty  Code = 0
	CodeGET    Code = 1
	CodePOST   Code = 2
	CodePUT    Code = 3
	CodeDELETE Code = 4
)

// Response codes.
const (
	CodeContinue            Code = 95  // 2.31 Continue
	CodeCreated             Code = 65  // 2.01
	CodeDeleted             Code = 66  // 2.02
	CodeValid               Code = 67  // 2.03
	CodeChanged             Code = 68  // 2.04
	CodeContent             Code = 69  // 2.05
	CodeBadRequest          Code = 128 // 4.00
	CodeUnauthorized        Code = 129 // 4.01
	CodeNotFound            Code = 132 // 4.04
	CodeMethodNotAllowed    Code = 133 // 4.05
	CodeInternalServerError Code = 160 // 5.00
	CodeNotImplemented      Code = 161 // 5.01
	CodeServiceUnavailable  Code = 163 // 5.03
)

// Class returns the code class (0-7).
func (c Code) Class() uint8 { return uint8(c) >> 5 }

// Detail returns the code detail (0-31).
func (c Code) Detail() uint8 { return uint8(c) & 0x1f }

// String renders the code as "c.dd".
func (c Code) String() string { return fmt.Sprintf("%d.%02d", c.Class(), c.Detail()) }

// Option numbers used by common CoAP features.
const (
	OptionObserve       uint16 = 6
	OptionURIPath       uint16 = 11
	OptionContentFormat uint16 = 12
	OptionURIQuery      uint16 = 15
	OptionAccept        uint16 = 17
	OptionBlock2        uint16 = 23
	OptionBlock1        uint16 = 27
)

// Option is a CoAP option.
type Option struct {
	Number uint16
	Value  []byte
}

// Message is a CoAP message.
type Message struct {
	Version   uint8
	Type      Type
	Code      Code
	MessageID uint16
	Token     []byte
	Options   []Option
	Payload   []byte
}

var (
	// ErrMalformed is returned for invalid CoAP bytes.
	ErrMalformed = errors.New("coap: malformed message")
)

// Marshal encodes the message.
func (m Message) Marshal() ([]byte, error) {
	if len(m.Token) > 8 {
		return nil, fmt.Errorf("%w: token too long", ErrMalformed)
	}
	ver := m.Version
	if ver == 0 {
		ver = Version
	}
	if ver != Version {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrMalformed, ver)
	}
	out := make([]byte, 4, 16+len(m.Payload))
	out[0] = ver<<6 | byte(m.Type&0x3)<<4 | byte(len(m.Token))
	out[1] = byte(m.Code)
	binary.BigEndian.PutUint16(out[2:4], m.MessageID)
	out = append(out, m.Token...)

	opts := append([]Option(nil), m.Options...)
	sort.SliceStable(opts, func(i, j int) bool { return opts[i].Number < opts[j].Number })
	last := 0
	for _, opt := range opts {
		delta := int(opt.Number) - last
		if delta < 0 {
			return nil, fmt.Errorf("%w: negative option delta", ErrMalformed)
		}
		out = appendOption(out, delta, opt.Value)
		last = int(opt.Number)
	}
	if len(m.Payload) > 0 {
		out = append(out, 0xFF)
		out = append(out, m.Payload...)
	}
	return out, nil
}

func appendOption(dst []byte, delta int, value []byte) []byte {
	dn, dext := optionNibble(delta)
	ln, lext := optionNibble(len(value))
	dst = append(dst, dn<<4|ln)
	dst = append(dst, dext...)
	dst = append(dst, lext...)
	return append(dst, value...)
}

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

// Parse decodes a CoAP message.
func Parse(data []byte) (Message, error) {
	var m Message
	if len(data) < 4 {
		return m, ErrMalformed
	}
	m.Version = data[0] >> 6
	if m.Version != Version {
		return m, fmt.Errorf("%w: unsupported version %d", ErrMalformed, m.Version)
	}
	m.Type = Type((data[0] >> 4) & 0x3)
	tokenLen := int(data[0] & 0x0f)
	if tokenLen > 8 || len(data) < 4+tokenLen {
		return m, ErrMalformed
	}
	m.Code = Code(data[1])
	m.MessageID = binary.BigEndian.Uint16(data[2:4])
	rest := data[4:]
	m.Token = append([]byte(nil), rest[:tokenLen]...)
	rest = rest[tokenLen:]

	number := 0
	for len(rest) > 0 {
		if rest[0] == 0xFF {
			if len(rest) == 1 {
				return m, fmt.Errorf("%w: empty payload marker", ErrMalformed)
			}
			m.Payload = append([]byte(nil), rest[1:]...)
			return m, nil
		}
		delta, length, n, err := parseOptionHeader(rest)
		if err != nil {
			return m, err
		}
		rest = rest[n:]
		if len(rest) < length {
			return m, ErrMalformed
		}
		number += delta
		if number > 65535 {
			return m, fmt.Errorf("%w: option number overflow", ErrMalformed)
		}
		m.Options = append(m.Options, Option{Number: uint16(number), Value: append([]byte(nil), rest[:length]...)})
		rest = rest[length:]
	}
	return m, nil
}

func parseOptionHeader(b []byte) (delta, length, consumed int, err error) {
	if len(b) < 1 {
		return 0, 0, 0, ErrMalformed
	}
	dn := b[0] >> 4
	ln := b[0] & 0x0f
	consumed = 1
	readExt := func(nibble byte) (int, error) {
		switch nibble {
		case 13:
			if len(b) < consumed+1 {
				return 0, ErrMalformed
			}
			v := int(b[consumed]) + 13
			consumed++
			return v, nil
		case 14:
			if len(b) < consumed+2 {
				return 0, ErrMalformed
			}
			v := int(binary.BigEndian.Uint16(b[consumed:consumed+2])) + 269
			consumed += 2
			return v, nil
		case 15:
			return 0, fmt.Errorf("%w: reserved option nibble", ErrMalformed)
		default:
			return int(nibble), nil
		}
	}
	delta, err = readExt(dn)
	if err != nil {
		return 0, 0, 0, err
	}
	length, err = readExt(ln)
	if err != nil {
		return 0, 0, 0, err
	}
	return delta, length, consumed, nil
}
