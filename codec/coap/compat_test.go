package coap

// This file keeps the pre-typed-layer implementations of Marshal and Parse so
// the compatibility tests can prove that the typed refactor did not change what
// encoders emit or what decoders accept. The code is copied verbatim from the
// package as it stood before the typed layer landed; do not modernize it.
//
// Remove the file only together with the promise that Parse and Marshal stay
// byte-compatible for gcoap.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// legacyMarshal is the Marshal implementation of codec/coap before the typed
// layer landed.
func legacyMarshal(m Message) ([]byte, error) {
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
		out = legacyAppendOption(out, delta, opt.Value)
		last = int(opt.Number)
	}
	if len(m.Payload) > 0 {
		out = append(out, 0xFF)
		out = append(out, m.Payload...)
	}
	return out, nil
}

func legacyAppendOption(dst []byte, delta int, value []byte) []byte {
	dn, dext := legacyOptionNibble(delta)
	ln, lext := legacyOptionNibble(len(value))
	dst = append(dst, dn<<4|ln)
	dst = append(dst, dext...)
	dst = append(dst, lext...)
	return append(dst, value...)
}

func legacyOptionNibble(v int) (byte, []byte) {
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

// legacyParse is the Parse implementation of codec/coap before the typed layer
// landed.
func legacyParse(data []byte) (Message, error) {
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
		delta, length, n, err := legacyParseOptionHeader(rest)
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

func legacyParseOptionHeader(b []byte) (delta, length, consumed int, err error) {
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

// randomMessage builds a message that exercises every field, including values
// the strict encoder rejects.
func randomMessage(rng *rand.Rand) Message {
	versions := []uint8{0, Version, 2}
	m := Message{
		Version:   versions[rng.Intn(len(versions))],
		Type:      Type(rng.Intn(5)),
		Code:      Code(rng.Intn(256)),
		MessageID: uint16(rng.Intn(1 << 16)),
	}
	if n := rng.Intn(11); n > 0 {
		m.Token = make([]byte, n)
		rng.Read(m.Token)
	}
	numbers := []uint16{0, 1, 4, 5, 6, 8, 11, 12, 14, 15, 17, 20, 23, 27, 28, 60, 300, 2048}
	for i := 0; i < rng.Intn(7); i++ {
		number := numbers[rng.Intn(len(numbers))]
		if rng.Intn(3) == 0 {
			number = uint16(rng.Intn(400))
		}
		value := make([]byte, rng.Intn(31))
		rng.Read(value)
		m.Options = append(m.Options, Option{Number: number, Value: value})
	}
	if rng.Intn(2) == 0 {
		m.Payload = make([]byte, rng.Intn(21))
		rng.Read(m.Payload)
	}
	return m
}

// randomDatagram builds a byte string that starts with a plausible header so the
// option parser is reached often.
func randomDatagram(rng *rand.Rand) []byte {
	raw := make([]byte, 4+rng.Intn(40))
	rng.Read(raw)
	raw[0] = Version<<6 | byte(rng.Intn(4))<<4 | byte(rng.Intn(16))
	if rng.Intn(2) == 0 {
		raw[0] &^= 0x0f
	}
	return raw
}

func TestMarshalMatchesLegacy(t *testing.T) {
	edge := []Message{
		{},
		{Code: CodeGET, MessageID: 1},
		{Version: Version, Type: Reset, Code: CodeEmpty, MessageID: 0xffff},
		{Code: CodeContent, Token: []byte{1, 2, 3, 4, 5, 6, 7, 8}, Payload: []byte{0}},
		{Token: make([]byte, 9)},
		{Version: 2},
		{Type: Type(4), Code: CodeGET},
		{Options: []Option{{Number: 23}, {Number: 6}, {Number: 12}}},
		{Options: []Option{{Number: 12, Value: []byte{0x32}}, {Number: 12, Value: []byte{0x32}}}},
		{Options: []Option{{Number: 300, Value: bytes.Repeat([]byte{'x'}, 300)}}},
	}
	for i, msg := range edge {
		want, wantErr := legacyMarshal(msg)
		got, gotErr := msg.Marshal()
		if (wantErr != nil) != (gotErr != nil) {
			t.Fatalf("edge %d: legacy error %v, new error %v", i, wantErr, gotErr)
		}
		if wantErr == nil && !bytes.Equal(want, got) {
			t.Fatalf("edge %d: legacy Marshal = % x, new Marshal = % x", i, want, got)
		}
	}

	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 5000; i++ {
		msg := randomMessage(rng)
		want, wantErr := legacyMarshal(msg)
		got, gotErr := msg.Marshal()
		if (wantErr != nil) != (gotErr != nil) {
			t.Fatalf("message %d: legacy error %v, new error %v", i, wantErr, gotErr)
		}
		if wantErr == nil && !bytes.Equal(want, got) {
			t.Fatalf("message %d: legacy Marshal = % x, new Marshal = % x\nmessage: %+v", i, want, got, msg)
		}
	}
}

func TestParseMatchesLegacy(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	compare := func(raw []byte) {
		t.Helper()
		want, wantErr := legacyParse(raw)
		got, gotErr := Parse(raw)
		if (wantErr != nil) != (gotErr != nil) {
			t.Fatalf("datagram % x: legacy error %v, new error %v", raw, wantErr, gotErr)
		}
		if wantErr == nil && !reflect.DeepEqual(want, got) {
			t.Fatalf("datagram % x:\nlegacy Parse = %+v\nnew Parse = %+v", raw, want, got)
		}
	}
	for i := 0; i < 20000; i++ {
		if i%3 == 0 {
			msg := randomMessage(rng)
			if raw, err := legacyMarshal(msg); err == nil {
				compare(raw)
			}
			continue
		}
		compare(randomDatagram(rng))
	}
}
