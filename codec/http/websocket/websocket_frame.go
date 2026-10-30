package websocket

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	continuationFrame = 0x0
	textFrame         = 0x1
	binaryFrame       = 0x2
	closeFrame        = 0x8
	pingFrame         = 0x9
	pongFrame         = 0xa
)

type protocolError struct {
	status uint16
	text   string
}

func (e *protocolError) Error() string { return "gnety: websocket: " + e.text }

func newProtocolError(format string, args ...any) error {
	return &protocolError{status: 1002, text: fmt.Sprintf(format, args...)}
}

type frameHeader struct {
	fin        bool
	opcode     byte
	length     uint64
	masked     bool
	maskingKey [4]byte
}

func readFrameHeader(r *bufio.Reader) (frameHeader, error) {
	var h frameHeader
	var first [2]byte
	if _, err := io.ReadFull(r, first[:]); err != nil {
		return h, err
	}
	h.fin = first[0]&0x80 != 0
	if first[0]&0x70 != 0 {
		return h, newProtocolError("RSV bits require a negotiated extension")
	}
	h.opcode = first[0] & 0x0f
	switch h.opcode {
	case continuationFrame, textFrame, binaryFrame,
		closeFrame, pingFrame, pongFrame:
	default:
		return h, newProtocolError("reserved opcode %#x", h.opcode)
	}
	control := h.opcode&0x08 != 0
	if control && !h.fin {
		return h, newProtocolError("fragmented control frame")
	}

	h.masked = first[1]&0x80 != 0
	length := first[1] & 0x7f
	switch length {
	case 126:
		var extended [2]byte
		if _, err := io.ReadFull(r, extended[:]); err != nil {
			return h, err
		}
		h.length = uint64(binary.BigEndian.Uint16(extended[:]))
		if h.length < 126 {
			return h, newProtocolError("non-minimal 16-bit payload length")
		}
	case 127:
		var extended [8]byte
		if _, err := io.ReadFull(r, extended[:]); err != nil {
			return h, err
		}
		if extended[0]&0x80 != 0 {
			return h, newProtocolError("payload length exceeds 63 bits")
		}
		h.length = binary.BigEndian.Uint64(extended[:])
		if h.length < 65536 {
			return h, newProtocolError("non-minimal 64-bit payload length")
		}
	default:
		h.length = uint64(length)
	}
	if control && h.length > 125 {
		return h, newProtocolError("control frame payload exceeds 125 bytes")
	}
	if h.masked {
		if _, err := io.ReadFull(r, h.maskingKey[:]); err != nil {
			return h, err
		}
	}
	return h, nil
}

func writeFrame(w *bufio.Writer, masked, fin bool, opcode byte, payload []byte) error {
	var header [14]byte
	first := opcode
	if fin {
		first |= 0x80
	}
	header[0] = first
	maskBit := byte(0)
	if masked {
		maskBit = 0x80
	}
	headerLen := 2
	switch {
	case len(payload) <= 125:
		header[1] = maskBit | byte(len(payload))
	case len(payload) <= 65535:
		header[1] = maskBit | 126
		binary.BigEndian.PutUint16(header[2:4], uint16(len(payload)))
		headerLen = 4
	default:
		header[1] = maskBit | 127
		binary.BigEndian.PutUint64(header[2:10], uint64(len(payload)))
		headerLen = 10
	}
	var maskingKey [4]byte
	if masked {
		if _, err := io.ReadFull(rand.Reader, maskingKey[:]); err != nil {
			return err
		}
		copy(header[headerLen:headerLen+4], maskingKey[:])
		headerLen += 4
	}
	if _, err := w.Write(header[:headerLen]); err != nil {
		return err
	}
	if masked {
		var buf [32 * 1024]byte
		for len(payload) > 0 {
			n := len(payload)
			if n > len(buf) {
				n = len(buf)
			}
			copy(buf[:n], payload[:n])
			applyMask(buf[:n], maskingKey, 0)
			if _, err := w.Write(buf[:n]); err != nil {
				return err
			}
			payload = payload[n:]
		}
	} else if _, err := w.Write(payload); err != nil {
		return err
	}
	return w.Flush()
}

func applyMask(payload []byte, key [4]byte, offset uint64) {
	for i := range payload {
		payload[i] ^= key[(offset+uint64(i))%4]
	}
}
