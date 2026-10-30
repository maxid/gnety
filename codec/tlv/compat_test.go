package tlv

// This file keeps the implementation the LwM2M server library shipped before
// the wire format moved into gnety, so the compatibility tests can prove that
// the port changed neither the bytes an encoder emits nor the entries a decoder
// produces. The code is copied from github.com/maxid/glwm2m/tlv with only the
// error sentinel and the helper names adjusted; do not modernize it, its value
// is being the old behavior.
//
// Remove the file only together with the promise that Marshal and Unmarshal stay
// byte-compatible with what glwm2m's tlv package emitted and accepted.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

// legacyErrMalformed is the sentinel the old package returned.
var legacyErrMalformed = errors.New("lwm2m tlv: malformed")

// legacyMarshal encodes entries the way the old package did: caller order, no
// duplicate check and no depth limit.
func legacyMarshal(entries []Entry) ([]byte, error) {
	var out []byte
	for _, e := range entries {
		b, err := legacyEncodeEntry(e)
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return out, nil
}

func legacyEncodeEntry(e Entry) ([]byte, error) {
	payload := e.Value
	if e.Type == ObjectInstance || e.Type == MultipleResource {
		var err error
		payload, err = legacyMarshal(e.Children)
		if err != nil {
			return nil, err
		}
	}
	if e.Type == ResourceValue || e.Type == ResourceInstance {
		if len(e.Children) > 0 {
			return nil, fmt.Errorf("%w: leaf has children", legacyErrMalformed)
		}
	}
	length := len(payload)
	if length > 0xFFFFFF {
		return nil, fmt.Errorf("%w: value too large", legacyErrMalformed)
	}

	header := byte(e.Type << 6)
	if e.ID > 0xFF {
		header |= 0x20
	}
	switch {
	case length < 8:
		// inline 3-bit length
	case length < 0x100:
		header |= 0x08
	case length < 0x10000:
		header |= 0x10
	default:
		header |= 0x18
	}
	if length < 8 {
		header |= byte(length)
	}
	out := []byte{header}
	if e.ID > 0xFF {
		out = append(out, byte(e.ID>>8), byte(e.ID))
	} else {
		out = append(out, byte(e.ID))
	}
	switch {
	case length < 8:
	case length < 0x100:
		out = append(out, byte(length))
	case length < 0x10000:
		out = append(out, byte(length>>8), byte(length))
	default:
		out = append(out, byte(length>>16), byte(length>>8), byte(length))
	}
	return append(out, payload...), nil
}

// legacyUnmarshal decodes a TLV payload the way the old package did.
func legacyUnmarshal(data []byte) ([]Entry, error) {
	entries, rest, err := legacyDecodeAll(data)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, legacyErrMalformed
	}
	return entries, nil
}

func legacyDecodeAll(data []byte) ([]Entry, []byte, error) {
	var out []Entry
	for len(data) > 0 {
		e, rest, err := legacyDecodeOne(data)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, e)
		data = rest
	}
	return out, data, nil
}

func legacyDecodeOne(data []byte) (Entry, []byte, error) {
	var e Entry
	if len(data) < 2 {
		return e, nil, legacyErrMalformed
	}
	header := data[0]
	e.Type = Type(header >> 6)
	if e.Type > ResourceValue {
		return e, nil, legacyErrMalformed
	}
	pos := 1
	if header&0x20 != 0 {
		if len(data) < pos+2 {
			return e, nil, legacyErrMalformed
		}
		e.ID = binary.BigEndian.Uint16(data[pos:])
		pos += 2
	} else {
		e.ID = uint16(data[pos])
		pos++
	}
	lengthType := header & 0x18
	length := 0
	switch lengthType {
	case 0x00:
		length = int(header & 0x07)
	case 0x08:
		if len(data) < pos+1 {
			return e, nil, legacyErrMalformed
		}
		length = int(data[pos])
		pos++
	case 0x10:
		if len(data) < pos+2 {
			return e, nil, legacyErrMalformed
		}
		length = int(binary.BigEndian.Uint16(data[pos:]))
		pos += 2
	case 0x18:
		if len(data) < pos+3 {
			return e, nil, legacyErrMalformed
		}
		length = int(data[pos])<<16 | int(binary.BigEndian.Uint16(data[pos+1:]))
		pos += 3
	}
	if length < 0 || len(data) < pos+length {
		return e, nil, legacyErrMalformed
	}
	payload := data[pos : pos+length]
	rest := data[pos+length:]
	switch e.Type {
	case ResourceValue, ResourceInstance:
		e.Value = append([]byte(nil), payload...)
	case ObjectInstance, MultipleResource:
		children, innerRest, err := legacyDecodeAll(payload)
		if err != nil {
			return e, nil, err
		}
		if len(innerRest) != 0 {
			return e, nil, legacyErrMalformed
		}
		e.Children = children
	}
	return e, rest, nil
}

// randomTree builds a tree the old encoder accepted: unique identifiers inside
// each scope, ascending identifier order, and leaves and containers kept apart.
func randomTree(rng *rand.Rand, depth int) []Entry {
	var entries []Entry
	used := make(map[uint16]bool, 3)
	for i, n := 0, rng.Intn(4); i < n; i++ {
		var id uint16
		for {
			switch rng.Intn(3) {
			case 0:
				id = uint16(rng.Intn(8))
			case 1:
				id = uint16(rng.Intn(256))
			default:
				id = uint16(rng.Intn(1 << 16))
			}
			if !used[id] {
				break
			}
		}
		used[id] = true

		e := Entry{ID: id}
		if depth < 3 && rng.Intn(3) == 0 {
			if rng.Intn(2) == 0 {
				e.Type = ObjectInstance
			} else {
				e.Type = MultipleResource
			}
			e.Children = randomTree(rng, depth+1)
			entries = append(entries, e)
			continue
		}
		if rng.Intn(2) == 0 {
			e.Type = ResourceValue
		} else {
			e.Type = ResourceInstance
		}
		var length int
		switch rng.Intn(6) {
		case 0:
			length = 0
		case 1:
			length = rng.Intn(8) // inline length
		case 2:
			length = 8 + rng.Intn(248) // 8-bit length
		case 3:
			length = 256 + rng.Intn(1000) // 16-bit length
		default:
			length = rng.Intn(300)
		}
		e.Value = make([]byte, length)
		rng.Read(e.Value)
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries
}

// TestMarshalMatchesLegacy proves the encoder emits the same bytes as the old
// implementation for every length class, identifier width and nesting.
func TestMarshalMatchesLegacy(t *testing.T) {
	edge := [][]Entry{
		nil,
		{{Type: ResourceValue, ID: 0}},
		{{Type: ResourceValue, ID: 0xff, Value: []byte("A")}},
		{{Type: ResourceValue, ID: 0x100, Value: []byte("A")}},
		{{Type: ResourceInstance, ID: 1, Value: []byte{0}}},
		{{Type: ObjectInstance, ID: 0, Children: []Entry{{Type: ResourceValue, ID: 5700, Value: []byte("21.5")}}}},
		{{Type: MultipleResource, ID: 6, Children: []Entry{
			{Type: ResourceInstance, ID: 0, Value: []byte{1}},
			{Type: ResourceInstance, ID: 1, Value: []byte{2}},
		}}},
		// The boundaries of the four length encodings.
		{{Type: ResourceValue, ID: 1, Value: make([]byte, 7)}},
		{{Type: ResourceValue, ID: 1, Value: make([]byte, 8)}},
		{{Type: ResourceValue, ID: 1, Value: make([]byte, 255)}},
		{{Type: ResourceValue, ID: 1, Value: make([]byte, 256)}},
		{{Type: ResourceValue, ID: 1, Value: make([]byte, 0xffff)}},
		{{Type: ResourceValue, ID: 1, Value: make([]byte, 0x10000)}},
	}
	for i, tree := range edge {
		want, wantErr := legacyMarshal(tree)
		got, gotErr := Marshal(tree)
		if (wantErr != nil) != (gotErr != nil) {
			t.Fatalf("edge %d: legacy error %v, new error %v", i, wantErr, gotErr)
		}
		if wantErr == nil && !bytes.Equal(want, got) {
			t.Fatalf("edge %d: legacy Marshal = % x, new Marshal = % x", i, want, got)
		}
	}

	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		tree := randomTree(rng, 0)
		want, wantErr := legacyMarshal(tree)
		got, gotErr := Marshal(tree)
		if (wantErr != nil) != (gotErr != nil) {
			t.Fatalf("tree %d: legacy error %v, new error %v\ntree: %+v", i, wantErr, gotErr, tree)
		}
		if wantErr == nil && !bytes.Equal(want, got) {
			t.Fatalf("tree %d: legacy Marshal = % x, new Marshal = % x\ntree: %+v", i, want, got, tree)
		}
	}
}

// TestGoldenFixturesMatchLegacy proves the golden fixtures are the bytes the old
// implementation produced, so they document compatibility rather than one
// implementation's habits.
func TestGoldenFixturesMatchLegacy(t *testing.T) {
	for _, tc := range goldenFixtures {
		want, err := legacyMarshal(tc.entries)
		if err != nil {
			t.Fatalf("%s: legacy Marshal: %v", tc.name, err)
		}
		if !bytes.Equal(want, tc.want) {
			t.Fatalf("%s: legacy Marshal = % x, fixture = % x", tc.name, want, tc.want)
		}
	}
}

// TestUnmarshalMatchesLegacy decodes the same bytes with both implementations.
// Whenever the new decoder accepts a payload it must produce exactly what the
// old one produced; whenever it rejects one, the payload must break one of the
// three documented stricter rules.
func TestUnmarshalMatchesLegacy(t *testing.T) {
	compare := func(raw []byte) {
		t.Helper()
		want, wantErr := legacyUnmarshal(raw)
		got, gotErr := Unmarshal(raw)
		if gotErr != nil {
			if kind := wire.KindOf(gotErr); kind != wire.KindMalformed && kind != wire.KindTooLarge {
				t.Fatalf("payload % x: kind %s (%v)", raw, kind, gotErr)
			}
			if wantErr == nil && !hasDuplicateID(want) && scopeDepth(want) <= MaxDepth+1 {
				t.Fatalf("payload % x is rejected by the new decoder but breaks no documented rule:\nlegacy Unmarshal = %+v",
					raw, want)
			}
			return
		}
		if wantErr != nil {
			t.Fatalf("payload % x: legacy error %v, new entries %+v", raw, wantErr, got)
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("payload % x:\nlegacy Unmarshal = %+v\nnew Unmarshal = %+v", raw, want, got)
		}
	}

	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 20000; i++ {
		switch i % 3 {
		case 0:
			// Bytes the old encoder produced: both must accept them.
			if raw, err := legacyMarshal(randomTree(rng, 0)); err == nil {
				compare(raw)
			}
		case 1:
			raw := make([]byte, rng.Intn(24))
			rng.Read(raw)
			compare(raw)
		default:
			// A plausible header, so the entry parser is reached often.
			raw := make([]byte, 1+rng.Intn(20))
			rng.Read(raw)
			raw[0] = byte(rng.Intn(4))<<6 | byte(rng.Intn(2))<<5 | byte(rng.Intn(4))<<3 | byte(rng.Intn(8))
			compare(raw)
		}
	}
}

// TestLegacyTolerancesNowRejected pins the two payloads the old decoder accepted
// and this one rejects on purpose.
func TestLegacyTolerancesNowRejected(t *testing.T) {
	duplicates := []Entry{{Type: ResourceValue, ID: 1, Value: []byte("A")}, {Type: ResourceValue, ID: 1, Value: []byte("B")}}
	deep := entriesNested(MaxDepth + 1)

	for name, tree := range map[string][]Entry{"duplicate identifier": duplicates, "nesting too deep": deep} {
		raw, err := legacyMarshal(tree)
		if err != nil {
			t.Fatalf("%s: legacy Marshal: %v", name, err)
		}
		if _, err := legacyUnmarshal(raw); err != nil {
			t.Fatalf("%s: legacy Unmarshal: %v", name, err)
		}
		if _, err := Unmarshal(raw); wire.KindOf(err) != wire.KindMalformed {
			t.Fatalf("%s: kind %s, want malformed", name, wire.KindOf(err))
		}
		if _, err := Marshal(tree); wire.KindOf(err) != wire.KindMalformed {
			t.Fatalf("%s: Marshal kind %s, want malformed", name, wire.KindOf(err))
		}
	}
}

// hasDuplicateID reports whether one scope of the tree repeats an identifier,
// which is the rule the new decoder adds.
func hasDuplicateID(entries []Entry) bool {
	seen := make(map[uint16]bool, len(entries))
	for _, e := range entries {
		if seen[e.ID] {
			return true
		}
		seen[e.ID] = true
		if hasDuplicateID(e.Children) {
			return true
		}
	}
	return false
}

// scopeDepth counts the levels of nesting, so a flat list is 1 and a container
// over a flat list is 2.
func scopeDepth(entries []Entry) int {
	maxDepth := 0
	for _, e := range entries {
		if d := scopeDepth(e.Children); d > maxDepth {
			maxDepth = d
		}
	}
	return 1 + maxDepth
}
