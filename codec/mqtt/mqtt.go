// Package mqtt implements MQTT 3.1.1 and MQTT 5 wire-format encoding and
// decoding.
//
// It converts bytes to packets and packets to bytes only. Session state (QoS,
// retained messages, subscriptions, keepalive, topic aliases) and any device or
// product semantics belong to the protocol library built on top, never here.
package mqtt

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// ErrMalformedPacket indicates an invalid MQTT packet.
var ErrMalformedPacket = errors.New("mqtt: malformed packet")

// MaxRemainingLength is the largest MQTT remaining length (256 MB - 1).
const MaxRemainingLength = 268435455

// ReadPacket reads one MQTT control packet and returns its fixed header byte and
// body (variable header plus payload). A non-positive maxSize disables the size
// limit.
func ReadPacket(r *bufio.Reader, maxSize int) (byte, []byte, error) {
	header, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	remaining, err := ReadRemainingLength(r)
	if err != nil {
		return 0, nil, err
	}
	if maxSize > 0 && remaining > maxSize {
		return 0, nil, fmt.Errorf("%w: packet too large", ErrMalformedPacket)
	}
	body := make([]byte, remaining)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return header, body, nil
}

// WritePacket encodes one MQTT control packet and writes it in a single call.
func WritePacket(w io.Writer, header byte, body []byte) error {
	buf := make([]byte, 0, 1+4+len(body))
	buf = append(buf, header)
	buf = AppendRemainingLength(buf, len(body))
	buf = append(buf, body...)
	_, err := w.Write(buf)
	return err
}

// ReadRemainingLength decodes the variable-length "remaining length" field.
func ReadRemainingLength(r *bufio.Reader) (int, error) {
	multiplier := 1
	value := 0
	for i := 0; i < 4; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		value += int(b&127) * multiplier
		if b&128 == 0 {
			if value > MaxRemainingLength {
				return 0, fmt.Errorf("%w: remaining length overflow", ErrMalformedPacket)
			}
			return value, nil
		}
		multiplier *= 128
	}
	return 0, fmt.Errorf("%w: malformed remaining length", ErrMalformedPacket)
}

// AppendRemainingLength encodes a "remaining length" field onto dst.
func AppendRemainingLength(dst []byte, n int) []byte {
	for {
		encoded := byte(n % 128)
		n /= 128
		if n > 0 {
			encoded |= 128
		}
		dst = append(dst, encoded)
		if n == 0 {
			return dst
		}
	}
}

// ReadUint16 reads a big-endian uint16 and returns the remaining bytes.
func ReadUint16(b []byte) (uint16, []byte, error) {
	if len(b) < 2 {
		return 0, nil, ErrMalformedPacket
	}
	return binary.BigEndian.Uint16(b[:2]), b[2:], nil
}

// ReadString reads a length-prefixed UTF-8 string and returns the remaining bytes.
func ReadString(b []byte) (string, []byte, error) {
	if len(b) < 2 {
		return "", nil, ErrMalformedPacket
	}
	n := int(binary.BigEndian.Uint16(b[:2]))
	if len(b) < 2+n {
		return "", nil, ErrMalformedPacket
	}
	return string(b[2 : 2+n]), b[2+n:], nil
}

// AppendString encodes a length-prefixed UTF-8 string onto dst.
func AppendString(dst []byte, s string) []byte {
	dst = append(dst, byte(len(s)>>8), byte(len(s)))
	return append(dst, s...)
}

// ReadBytes reads a length-prefixed binary blob and returns the remaining bytes.
func ReadBytes(b []byte) ([]byte, []byte, error) {
	if len(b) < 2 {
		return nil, nil, ErrMalformedPacket
	}
	n := int(binary.BigEndian.Uint16(b[:2]))
	if len(b) < 2+n {
		return nil, nil, ErrMalformedPacket
	}
	return b[2 : 2+n], b[2+n:], nil
}

// ReadVariableByteInt decodes an MQTT variable byte integer and returns the
// remaining bytes.
func ReadVariableByteInt(b []byte) (int, []byte, error) {
	multiplier := 1
	value := 0
	for i := 0; i < 4; i++ {
		if len(b) < 1 {
			return 0, nil, ErrMalformedPacket
		}
		encoded := b[0]
		b = b[1:]
		value += int(encoded&127) * multiplier
		if encoded&128 == 0 {
			if value > MaxRemainingLength {
				return 0, nil, fmt.Errorf("%w: variable byte integer overflow", ErrMalformedPacket)
			}
			return value, b, nil
		}
		multiplier *= 128
	}
	return 0, nil, fmt.Errorf("%w: malformed variable byte integer", ErrMalformedPacket)
}

// SkipProperties reads an MQTT 5 property length and skips the property bytes,
// returning the remaining bytes.
func SkipProperties(b []byte) ([]byte, error) {
	n, rest, err := ReadVariableByteInt(b)
	if err != nil {
		return nil, err
	}
	if len(rest) < n {
		return nil, ErrMalformedPacket
	}
	return rest[n:], nil
}
