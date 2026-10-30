package wire

import (
	"bufio"
)

// MaxVarintBytes is the largest varint width this package encodes: 4 bytes
// carry 28 bits, the MQTT remaining-length limit (256 MB - 1).
const MaxVarintBytes = 4

// MaxVarintValue is the largest value a 4-byte varint can carry.
const MaxVarintValue = 1<<(7*4) - 1

// ReadVarint reads a base-128 varint of at most maxBytes bytes (0 means
// MaxVarintBytes).
//
// A non-minimal encoding (a continuation whose payload is zero) and a width
// beyond maxBytes are both malformed: MQTT requires the shortest form, and
// silently accepting padded forms is a parser-differential hazard.
func ReadVarint(r *bufio.Reader, maxBytes int) (uint64, error) {
	v, _, err := readVarint(r, maxBytes)
	return v, err
}

func readVarint(r *bufio.Reader, maxBytes int) (uint64, int, error) {
	if maxBytes <= 0 {
		maxBytes = MaxVarintBytes
	}
	var value uint64
	for i := 0; i < maxBytes; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, i, incomplete(err)
		}
		payload := uint64(b & 0x7f)
		if i == maxBytes-1 && b&0x80 != 0 {
			return 0, i + 1, Errorf(KindMalformed, "varint longer than %d bytes", maxBytes)
		}
		if i > 0 && b == 0x00 {
			return 0, i + 1, Errorf(KindMalformed, "varint is not minimally encoded")
		}
		value |= payload << (7 * i)
		if b&0x80 == 0 {
			return value, i + 1, nil
		}
	}
	return 0, maxBytes, Errorf(KindMalformed, "varint longer than %d bytes", maxBytes)
}

// AppendVarint appends the minimal base-128 encoding of v. Values above
// MaxVarintValue are truncated to their low 28 bits, matching the width this
// package supports.
func AppendVarint(dst []byte, v uint64) []byte {
	if v > MaxVarintValue {
		v = MaxVarintValue
	}
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v > 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if v == 0 {
			return dst
		}
	}
}

// VarintLen returns the encoded width of v.
func VarintLen(v uint64) int {
	n := 1
	for v >= 0x80 {
		v >>= 7
		n++
	}
	return n
}
