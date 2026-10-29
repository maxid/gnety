// Package gnety is a small TCP/UDP transport framework for custom device
// protocols. It provides framing codecs, sessions and lifecycle handlers, but
// deliberately avoids a Netty-style annotation/pipeline clone.
//
// Scope: custom binary/text protocols over TCP and UDP. MQTT stays in gmqx,
// HTTP stays in net/http.
package gnety

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// ErrFrameTooLarge is returned when a decoded frame exceeds MaxFrameSize.
var ErrFrameTooLarge = errors.New("gnety: frame too large")

// Codec decodes/encodes stream frames for TCP.
type Codec interface {
	Decode(r *bufio.Reader) (any, error)
	Encode(w io.Writer, msg any) error
}

// DatagramCodec decodes/encodes datagrams for UDP.
type DatagramCodec interface {
	Decode(data []byte) (any, error)
	Encode(msg any) ([]byte, error)
}

// LengthFieldCodec encodes []byte payloads with a length prefix.
//
// The zero value is a 4-byte big-endian length prefix followed by the payload.
// Optional fields cover magic headers, length-field offsets/sizes, endianness
// and length fields that include the header bytes.
type LengthFieldCodec struct {
	MaxFrameSize         int
	Magic                []byte // optional magic bytes at the start
	LengthFieldOffset    int    // bytes before the length field (default 0)
	LengthFieldSize      int    // 1, 2 or 4 (default 4)
	LittleEndian         bool
	LengthIncludesHeader bool
}

func (c LengthFieldCodec) size() int {
	switch c.LengthFieldSize {
	case 1, 2, 4:
		return c.LengthFieldSize
	default:
		return 4
	}
}

func (c LengthFieldCodec) order() binary.ByteOrder {
	if c.LittleEndian {
		return binary.LittleEndian
	}
	return binary.BigEndian
}

func (c LengthFieldCodec) max() int {
	if c.MaxFrameSize > 0 {
		return c.MaxFrameSize
	}
	return 4 << 20
}

// Decode reads one length-prefixed frame.
func (c LengthFieldCodec) Decode(r *bufio.Reader) (any, error) {
	offset := c.LengthFieldOffset
	if offset < len(c.Magic) {
		return nil, fmt.Errorf("gnety: LengthFieldOffset %d smaller than magic", offset)
	}
	if len(c.Magic) > 0 {
		magic := make([]byte, len(c.Magic))
		if _, err := io.ReadFull(r, magic); err != nil {
			return nil, err
		}
		if string(magic) != string(c.Magic) {
			return nil, fmt.Errorf("gnety: magic mismatch")
		}
	}
	if pad := offset - len(c.Magic); pad > 0 {
		if _, err := io.CopyN(io.Discard, r, int64(pad)); err != nil {
			return nil, err
		}
	}
	size := c.size()
	lenBuf := make([]byte, size)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, err
	}
	var frameLen int
	switch size {
	case 1:
		frameLen = int(lenBuf[0])
	case 2:
		frameLen = int(c.order().Uint16(lenBuf))
	case 4:
		frameLen = int(c.order().Uint32(lenBuf))
	}
	payloadLen := frameLen
	if c.LengthIncludesHeader {
		payloadLen -= offset + size
	}
	if payloadLen <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrFrameTooLarge, frameLen)
	}
	if payloadLen > c.max() {
		return nil, fmt.Errorf("%w: %d", ErrFrameTooLarge, payloadLen)
	}
	buf := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// Encode writes one length-prefixed frame.
func (c LengthFieldCodec) Encode(w io.Writer, msg any) error {
	b, ok := msg.([]byte)
	if !ok {
		return fmt.Errorf("gnety: LengthFieldCodec expects []byte, got %T", msg)
	}
	offset := c.LengthFieldOffset
	if offset < len(c.Magic) {
		return fmt.Errorf("gnety: LengthFieldOffset %d smaller than magic", offset)
	}
	size := c.size()
	headerLen := offset + size
	frameLen := len(b)
	if c.LengthIncludesHeader {
		frameLen += headerLen
	}
	var header bytes.Buffer
	header.Write(c.Magic)
	header.Write(make([]byte, offset-len(c.Magic)))
	switch size {
	case 1:
		header.WriteByte(byte(frameLen))
	case 2:
		var b2 [2]byte
		c.order().PutUint16(b2[:], uint16(frameLen))
		header.Write(b2[:])
	case 4:
		var b4 [4]byte
		c.order().PutUint32(b4[:], uint32(frameLen))
		header.Write(b4[:])
	}
	if _, err := w.Write(header.Bytes()); err != nil {
		return err
	}
	_, err := w.Write(b)
	return err
}

// DelimiterCodec splits frames by a single delimiter byte (delimiter excluded
// from the payload).
type DelimiterCodec struct {
	Delimiter    byte
	MaxFrameSize int
}

// Decode reads until the delimiter.
func (c DelimiterCodec) Decode(r *bufio.Reader) (any, error) {
	max := c.MaxFrameSize
	if max <= 0 {
		max = 4 << 20
	}
	var out []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == c.Delimiter {
			return out, nil
		}
		out = append(out, b)
		if len(out) > max {
			return nil, ErrFrameTooLarge
		}
	}
}

// Encode writes payload + delimiter.
func (c DelimiterCodec) Encode(w io.Writer, msg any) error {
	b, ok := msg.([]byte)
	if !ok {
		return fmt.Errorf("gnety: DelimiterCodec expects []byte, got %T", msg)
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	_, err := w.Write([]byte{c.Delimiter})
	return err
}

// BytesDatagramCodec is a passthrough datagram codec.
type BytesDatagramCodec struct{}

// Decode returns the datagram as []byte.
func (BytesDatagramCodec) Decode(data []byte) (any, error) { return append([]byte(nil), data...), nil }

// Encode returns the payload bytes.
func (BytesDatagramCodec) Encode(msg any) ([]byte, error) {
	b, ok := msg.([]byte)
	if !ok {
		return nil, fmt.Errorf("gnety: BytesDatagramCodec expects []byte, got %T", msg)
	}
	return b, nil
}
