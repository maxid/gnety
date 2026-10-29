// Package gnety is a small TCP/UDP transport framework for custom device
// protocols. It provides framing codecs, sessions and lifecycle handlers, but
// deliberately avoids a Netty-style annotation/pipeline clone.
//
// Scope: custom binary/text protocols over TCP and UDP. MQTT stays in gmqx,
// HTTP stays in net/http.
package gnety

import (
	"bufio"
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

// LengthFieldCodec encodes []byte payloads with a big-endian length prefix.
// The format is a simple big-endian length prefix followed by the payload.
type LengthFieldCodec struct {
	MaxFrameSize int
}

// Decode reads one length-prefixed frame.
func (c LengthFieldCodec) Decode(r *bufio.Reader) (any, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint32(lenBuf[:]))
	max := c.MaxFrameSize
	if max <= 0 {
		max = 4 << 20
	}
	if n <= 0 || n > max {
		return nil, fmt.Errorf("%w: %d", ErrFrameTooLarge, n)
	}
	buf := make([]byte, n)
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
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(b)))
	if _, err := w.Write(lenBuf[:]); err != nil {
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
