package tlv

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

// TestRoundTrip is the round trip the LwM2M server library has always relied
// on: an object instance over a resource with a value and a multiple resource
// with two resource instances.
func TestRoundTrip(t *testing.T) {
	entries := []Entry{
		{Type: ObjectInstance, ID: 0, Children: []Entry{
			{Type: ResourceValue, ID: 5700, Value: []byte("21.5")},
			{Type: MultipleResource, ID: 6, Children: []Entry{
				{Type: ResourceInstance, ID: 0, Value: []byte{1}},
				{Type: ResourceInstance, ID: 1, Value: []byte{2}},
			}},
		}},
	}
	raw, err := Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != ObjectInstance || len(got[0].Children) != 2 {
		t.Fatalf("got %+v", got)
	}
	// Marshal emits children in identifier order, so resource 6 comes before
	// resource 5700: look them up by identifier instead of by position.
	resource := func(entries []Entry, id uint16) Entry {
		t.Helper()
		for _, e := range entries {
			if e.ID == id {
				return e
			}
		}
		t.Fatalf("resource %d is missing from %+v", id, entries)
		return Entry{}
	}
	if value := resource(got[0].Children, 5700).Value; string(value) != "21.5" {
		t.Fatalf("resource 5700 = %q", value)
	}
	if inner := resource(got[0].Children, 6); len(inner.Children) != 2 || inner.Children[1].ID != 1 {
		t.Fatalf("multiple resource mismatch: %+v", inner)
	}
}

func TestInlineAndExtendedLengths(t *testing.T) {
	for _, value := range [][]byte{
		nil,
		[]byte("x"),
		bytes.Repeat([]byte("a"), 7),
		bytes.Repeat([]byte("a"), 20),
		bytes.Repeat([]byte("b"), 300),
	} {
		raw, err := Marshal([]Entry{{Type: ResourceValue, ID: 1, Value: value}})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Unmarshal(raw)
		if err != nil || len(got) != 1 || !bytes.Equal(got[0].Value, value) {
			t.Fatalf("len=%d err=%v", len(value), err)
		}
	}
}

// TestShortestLengthEncoding pins the length field for every width the format
// has: inline, 8-bit, 16-bit and 24-bit.
func TestShortestLengthEncoding(t *testing.T) {
	cases := []struct {
		length int
		header byte
		extra  []byte
	}{
		{0, 0xc0, nil},                          // C0: resource with value, 8-bit id, inline 0
		{7, 0xc7, nil},                          // inline 7 is the largest inline length
		{8, 0xc8, []byte{0x08}},                 // 8 no longer fits the three inline bits
		{255, 0xc8, []byte{0xff}},               // still an 8-bit length
		{256, 0xd0, []byte{0x01, 0x00}},         // 16-bit from 256 on
		{65535, 0xd0, []byte{0xff, 0xff}},       // largest 16-bit length
		{65536, 0xd8, []byte{0x01, 0x00, 0x00}}, // 24-bit from 65536 on
	}
	for _, tc := range cases {
		raw, err := Marshal([]Entry{{Type: ResourceValue, ID: 1, Value: make([]byte, tc.length)}})
		if err != nil {
			t.Fatalf("len=%d: %v", tc.length, err)
		}
		want := 2 + len(tc.extra) + tc.length // header, id, length field, value
		if len(raw) != want {
			t.Fatalf("len=%d: encoded %d bytes, want %d", tc.length, len(raw), want)
		}
		if raw[0] != tc.header {
			t.Fatalf("len=%d: header %#02x, want %#02x", tc.length, raw[0], tc.header)
		}
		if raw[1] != 0x01 {
			t.Fatalf("len=%d: id %#02x, want 0x01", tc.length, raw[1])
		}
		if !bytes.Equal(raw[2:2+len(tc.extra)], tc.extra) {
			t.Fatalf("len=%d: length field % x, want % x", tc.length, raw[2:2+len(tc.extra)], tc.extra)
		}
	}
}

// ascii returns n copies of b, the payload the length-class fixtures use.
func ascii(n int, b byte) []byte { return bytes.Repeat([]byte{b}, n) }

// goldenFixtures pins the exact bytes of every shape the format has. The same
// table decodes back, so a fixture cannot drift in one direction only, and
// compat_test.go checks every fixture against the implementation glwm2m shipped.
//
// The entries are in canonical order so that the old encoder, which emitted the
// caller's order, produces the same bytes.
var goldenFixtures = []struct {
	name    string
	entries []Entry
	want    []byte
}{
	{
		name:    "empty resource with inline length",
		entries: []Entry{{Type: ResourceValue, ID: 0}},
		want:    []byte{0xc0, 0x00},
	},
	{
		name:    "resource with inline length",
		entries: []Entry{{Type: ResourceValue, ID: 1, Value: []byte("A")}},
		want:    []byte{0xc1, 0x01, 'A'},
	},
	{
		name:    "resource with a 16-bit identifier",
		entries: []Entry{{Type: ResourceValue, ID: 0x0102, Value: []byte("A")}},
		want:    []byte{0xe1, 0x01, 0x02, 'A'},
	},
	{
		name:    "resource with an 8-bit length",
		entries: []Entry{{Type: ResourceValue, ID: 3, Value: ascii(20, 'a')}},
		want:    append([]byte{0xc8, 0x03, 20}, ascii(20, 'a')...),
	},
	{
		name:    "resource with a 16-bit length",
		entries: []Entry{{Type: ResourceValue, ID: 3, Value: ascii(300, 'b')}},
		want:    append([]byte{0xd0, 0x03, 0x01, 0x2c}, ascii(300, 'b')...),
	},
	{
		name: "multiple resources",
		entries: []Entry{{Type: MultipleResource, ID: 6, Children: []Entry{
			{Type: ResourceInstance, ID: 0, Value: []byte{1}},
			{Type: ResourceInstance, ID: 1, Value: []byte{2}},
		}}},
		want: []byte{0x86, 0x06, 0x41, 0x00, 0x01, 0x41, 0x01, 0x02},
	},
	{
		name: "object instance over a resource and a multiple resource",
		entries: []Entry{{Type: ObjectInstance, ID: 0, Children: []Entry{
			{Type: MultipleResource, ID: 6, Children: []Entry{
				{Type: ResourceInstance, ID: 0, Value: []byte{1}},
				{Type: ResourceInstance, ID: 1, Value: []byte{2}},
			}},
			{Type: ResourceValue, ID: 5700, Value: []byte("21.5")},
		}}},
		want: []byte{
			0x08, 0x00, 0x0f, // object instance, id 0, 8-bit length 15
			0x86, 0x06, 0x41, 0x00, 0x01, 0x41, 0x01, 0x02, // resource 6, children in id order
			0xe4, 0x16, 0x44, '2', '1', '.', '5', // resource 5700
		},
	},
	{
		name:    "empty payload",
		entries: nil,
		want:    nil,
	},
}

func TestGoldenFixtures(t *testing.T) {
	for _, tc := range goldenFixtures {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Marshal(tc.entries)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("Marshal = % x, want % x", got, tc.want)
			}
			decoded, err := Unmarshal(tc.want)
			if err != nil {
				t.Fatalf("Unmarshal(% x): %v", tc.want, err)
			}
			if !reflect.DeepEqual(decoded, tc.entries) {
				// An empty payload decodes to nil entries and a container
				// with no children decodes to nil children; DeepEqual treats
				// the empty and nil slice as different, so compare through a
				// second encode instead.
				again, err := Marshal(decoded)
				if err != nil || !bytes.Equal(again, tc.want) {
					t.Fatalf("Unmarshal(% x) = %+v, re-encoded % x (err %v)", tc.want, decoded, again, err)
				}
			}
		})
	}
}

// TestWideLengths checks the 24-bit length field and the largest identifier,
// which cannot be decoded through Unmarshal's default budget.
func TestWideLengths(t *testing.T) {
	value := bytes.Repeat([]byte{'c'}, 70000)
	raw, err := Marshal([]Entry{{Type: ResourceValue, ID: MaxID, Value: value}})
	if err != nil {
		t.Fatal(err)
	}
	wantHeader := []byte{0xf8, 0xff, 0xff, 0x01, 0x11, 0x70} // 16-bit id 65535, 24-bit length 70000
	if !bytes.HasPrefix(raw, wantHeader) {
		t.Fatalf("Marshal prefix = % x, want % x", raw[:len(wantHeader)], wantHeader)
	}
	if _, err := Unmarshal(raw); wire.KindOf(err) != wire.KindTooLarge {
		t.Fatalf("Unmarshal of a 70 kB payload: kind %s, want too large", wire.KindOf(err))
	}
	got, err := ParseLimit(raw, len(raw))
	if err != nil {
		t.Fatalf("ParseLimit: %v", err)
	}
	if len(got) != 1 || got[0].ID != MaxID || !bytes.Equal(got[0].Value, value) {
		t.Fatalf("ParseLimit round trip mismatch: %+v", got)
	}
}

func TestMalformed(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		kind wire.Kind
	}{
		{"legacy case 1", []byte{0xff}, wire.KindMalformed},
		{"legacy case 2", []byte{0xc0, 0x01, 0x05, 0x00}, wire.KindMalformed},
		{"one byte", []byte{0x00}, wire.KindMalformed},
		{"truncated 16-bit identifier", []byte{0xe1, 0x01}, wire.KindMalformed},
		{"truncated 24-bit length", []byte{0xd8, 0x01, 0x00, 0x00}, wire.KindMalformed},
		{"inline length past the entry", []byte{0xc1, 0x01}, wire.KindMalformed},
		{"16-bit length past the entry", []byte{0xd0, 0x01, 0x00, 0x04, 'A'}, wire.KindMalformed},
		{"24-bit length past the entry", []byte{0xd8, 0x01, 0x00, 0x00, 0x02, 'A'}, wire.KindMalformed},
		{"child length past its parent", []byte{
			0x08, 0x00, 0x03, // object instance, 3 bytes of children
			0xc5, 0x01, 'A', // resource 1 declares 5 bytes, its parent has 1
		}, wire.KindMalformed},
		{"duplicate identifier", []byte{0xc1, 0x01, 'A', 0xc1, 0x01, 'B'}, wire.KindMalformed},
		{"duplicate identifier across types", []byte{
			0xc1, 0x01, 'A', // resource 1
			0x41, 0x01, 'B', // resource instance 1, same scope
		}, wire.KindMalformed},
		{"duplicate identifier in a child scope", []byte{
			0x06, 0x00, // object instance 0, inline length 6
			0xc1, 0x01, 'A',
			0xc1, 0x01, 'B',
		}, wire.KindMalformed},
		{"nesting deeper than MaxDepth", nestedContainers(MaxDepth + 1), wire.KindMalformed},
		{"oversized payload", bytes.Repeat([]byte{0xc0, 0x00}, DefaultMaxSize), wire.KindTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Unmarshal(tc.data)
			if err == nil {
				t.Fatalf("Unmarshal(% x) accepted the payload", tc.data)
			}
			if kind := wire.KindOf(err); kind != tc.kind {
				t.Fatalf("Unmarshal(% x) kind = %s (%v), want %s", tc.data, kind, err, tc.kind)
			}
			if _, err := Marshal(entriesFromBytes(tc.data)); err != nil && wire.KindOf(err) == wire.KindUnknown {
				t.Fatalf("Marshal error is unclassified: %v", err)
			}
		})
	}
}

// entriesFromBytes decodes whatever it can and returns an empty list when the
// payload is malformed; TestMalformed uses it to make sure every rejection is
// classified in both directions.
func entriesFromBytes(data []byte) []Entry {
	entries, err := Unmarshal(data)
	if err != nil {
		return nil
	}
	return entries
}

// TestTruncatedPrefixesAreMalformed walks every prefix of a valid payload. A
// TLV payload is a datagram, so a short prefix is malformed and never reports
// "needs more bytes".
func TestTruncatedPrefixesAreMalformed(t *testing.T) {
	raw, err := Marshal([]Entry{
		{Type: ObjectInstance, ID: 300, Children: []Entry{
			{Type: ResourceValue, ID: 5700, Value: []byte("21.5")},
			{Type: MultipleResource, ID: 6, Children: []Entry{
				{Type: ResourceInstance, ID: 1, Value: []byte{2}},
			}},
		}},
		{Type: ResourceValue, ID: 1, Value: bytes.Repeat([]byte("d"), 200)},
	})
	if err != nil {
		t.Fatal(err)
	}
	rejected := 0
	for i := 0; i < len(raw); i++ {
		// A cut that lands on an entry boundary is a complete, shorter
		// payload and is accepted; every other prefix must be rejected, and
		// rejected as malformed rather than as "needs more bytes".
		if _, err := Unmarshal(raw[:i]); err != nil {
			if kind := wire.KindOf(err); kind != wire.KindMalformed && kind != wire.KindTooLarge {
				t.Fatalf("prefix of %d bytes: kind %s (%v)", i, kind, err)
			}
			rejected++
		}
	}
	if rejected == 0 {
		t.Fatal("no truncated prefix was rejected")
	}
}

// TestNestedScopeIsExact checks that a child scope consumes exactly its
// declared length: a nested payload that is one byte short is malformed even
// when the following bytes would complete it.
func TestNestedScopeIsExact(t *testing.T) {
	// Object instance 0 declares 3 bytes of children, so the trailing entry
	// belongs to the top-level scope and not to the instance.
	data := []byte{0x03, 0x00, 0xc1, 0x01, 'A', 0xc1, 0x02, 'B'}
	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got) != 2 || got[0].Type != ObjectInstance || len(got[0].Children) != 1 || got[1].ID != 2 {
		t.Fatalf("scope boundaries are wrong: %+v", got)
	}
}

func TestDepthLimit(t *testing.T) {
	ok := nestedContainers(MaxDepth)
	if _, err := Unmarshal(ok); err != nil {
		t.Fatalf("Unmarshal of %d nested containers: %v", MaxDepth, err)
	}
	if _, err := Marshal(entriesNested(MaxDepth)); err != nil {
		t.Fatalf("Marshal of %d nested containers: %v", MaxDepth, err)
	}
	if _, err := Marshal(entriesNested(MaxDepth + 1)); wire.KindOf(err) != wire.KindMalformed {
		t.Fatalf("Marshal of %d nested containers: kind %s, want malformed", MaxDepth+1, wire.KindOf(err))
	}
}

// nestedContainers builds n nested object instances around one resource value.
func nestedContainers(n int) []byte {
	payload := []byte{0xc0, 0x01} // resource with value, id 1, no value
	for i := 0; i < n; i++ {
		// A 24-bit length keeps any depth encodable.
		header := []byte{
			byte(ObjectInstance)<<6 | len24,
			0x00,
			byte(len(payload) >> 16), byte(len(payload) >> 8), byte(len(payload)),
		}
		payload = append(header, payload...)
	}
	return payload
}

// entriesNested builds the entry tree nestedContainers encodes.
func entriesNested(n int) []Entry {
	entries := []Entry{{Type: ResourceValue, ID: 1}}
	for i := 0; i < n; i++ {
		entries = []Entry{{Type: ObjectInstance, ID: 0, Children: entries}}
	}
	return entries
}

func TestMarshalRejections(t *testing.T) {
	cases := []struct {
		name    string
		entries []Entry
		kind    wire.Kind
	}{
		{"unknown type", []Entry{{Type: Type(4), ID: 1}}, wire.KindUnsupported},
		{"container with a value", []Entry{{Type: ObjectInstance, ID: 1, Value: []byte{1}}}, wire.KindMalformed},
		{"multiple resource with a value", []Entry{{Type: MultipleResource, ID: 1, Value: []byte{1}}}, wire.KindMalformed},
		{"leaf with children", []Entry{{Type: ResourceValue, ID: 1, Children: []Entry{{Type: ResourceValue, ID: 2}}}}, wire.KindMalformed},
		{"duplicate identifiers", []Entry{{Type: ResourceValue, ID: 1}, {Type: ResourceValue, ID: 1}}, wire.KindMalformed},
		{"nesting too deep", entriesNested(MaxDepth + 1), wire.KindMalformed},
		{"value longer than the 24-bit length", []Entry{{
			Type: ResourceValue, ID: 1, Value: make([]byte, MaxValueLen+1),
		}}, wire.KindTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Marshal(tc.entries)
			if err == nil {
				t.Fatal("Marshal accepted the tree")
			}
			if kind := wire.KindOf(err); kind != tc.kind {
				t.Fatalf("kind = %s (%v), want %s", kind, err, tc.kind)
			}
		})
	}
}

// TestMarshalIsDeterministicAndSorted proves the documented output order: the
// same tree produces the same bytes at every level, whatever order the caller
// built it in, and the entries of one scope are ordered by identifier.
func TestMarshalIsDeterministicAndSorted(t *testing.T) {
	build := func(reverse bool) []Entry {
		entries := []Entry{
			{Type: ResourceValue, ID: 0x1234, Value: []byte("b")},
			{Type: MultipleResource, ID: 6, Children: []Entry{
				{Type: ResourceInstance, ID: 2, Value: []byte{2}},
				{Type: ResourceInstance, ID: 0, Value: []byte{0}},
			}},
			{Type: ResourceValue, ID: 1, Value: []byte("a")},
		}
		if reverse {
			entries[1].Children[0], entries[1].Children[1] = entries[1].Children[1], entries[1].Children[0]
			entries = []Entry{entries[2], entries[1], entries[0]}
		}
		return entries
	}
	first, err := Marshal(build(false))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		raw, err := Marshal(build(i%2 == 0))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(raw, first) {
			t.Fatalf("Marshal is not deterministic: % x then % x", first, raw)
		}
	}
	want := []byte{
		0xc1, 0x01, 'a', // resource 1
		0x86, 0x06, 0x41, 0x00, 0x00, 0x41, 0x02, 0x02, // resource 6, children sorted
		0xe1, 0x12, 0x34, 'b', // resource 0x1234
	}
	if !bytes.Equal(first, want) {
		t.Fatalf("Marshal = % x, want % x", first, want)
	}
}

// TestMarshalDoesNotAliasOrReorderInput checks both halves of the ownership
// promise: the output never shares memory with the caller's slices and Marshal
// never reorders the slice it was given.
func TestMarshalDoesNotAliasOrReorderInput(t *testing.T) {
	value := []byte("21.5")
	entries := []Entry{
		{Type: ResourceValue, ID: 2, Value: []byte("second")},
		{Type: ResourceValue, ID: 1, Value: value},
	}
	raw, err := Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := append([]byte(nil), raw...)

	if entries[0].ID != 2 || entries[1].ID != 1 {
		t.Fatalf("Marshal reordered the caller's slice: %+v", entries)
	}

	value[0] = 'X'
	if !bytes.Equal(raw, snapshot) {
		t.Fatalf("the output aliases the caller's value: % x, want % x", raw, snapshot)
	}

	raw[len(raw)-1] = 'Y'
	if string(value) != "X1.5" {
		t.Fatalf("the caller's value changed with the output: %q", value)
	}
}

// TestUnmarshalDoesNotAliasInput checks that a decoded tree survives the caller
// reusing its buffer.
func TestUnmarshalDoesNotAliasInput(t *testing.T) {
	raw, err := Marshal([]Entry{
		{Type: ObjectInstance, ID: 0, Children: []Entry{
			{Type: ResourceValue, ID: 5700, Value: []byte("21.5")},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		raw[i] = 0
	}
	if string(got[0].Children[0].Value) != "21.5" {
		t.Fatalf("decoded value aliases the input buffer: %q", got[0].Children[0].Value)
	}
}

func TestEmptyPayload(t *testing.T) {
	for _, data := range [][]byte{nil, {}} {
		got, err := Unmarshal(data)
		if err != nil || len(got) != 0 {
			t.Fatalf("Unmarshal(%v) = %+v, %v", data, got, err)
		}
	}
}

func TestType(t *testing.T) {
	containers := map[Type]bool{ObjectInstance: true, MultipleResource: true, ResourceInstance: false, ResourceValue: false}
	for typ, want := range containers {
		if got := typ.IsContainer(); got != want {
			t.Fatalf("%d.IsContainer() = %v, want %v", typ, got, want)
		}
		if !typ.Valid() {
			t.Fatalf("%d.Valid() = false", typ)
		}
		if typ.String() == "unknown" {
			t.Fatalf("%d.String() = unknown", typ)
		}
	}
	if Type(4).Valid() {
		t.Fatal("Type(4).Valid() = true")
	}
	if Type(4).String() != "unknown" {
		t.Fatalf("Type(4).String() = %q", Type(4).String())
	}
	if !reflect.DeepEqual(Type(ObjectInstance).String(), "object instance") {
		t.Fatalf("Type(0).String() = %q", Type(ObjectInstance).String())
	}
}

// TestDatagramError keeps the mapping from a short read to a malformed error
// honest even though the scope checks above already prevent it.
func TestDatagramError(t *testing.T) {
	err := datagramError(wire.Errorf(wire.KindIncomplete, "gone"))
	if kind := wire.KindOf(err); kind != wire.KindMalformed {
		t.Fatalf("kind = %s, want malformed", kind)
	}
	original := wire.Errorf(wire.KindMalformed, "kept")
	if got := datagramError(original); got != original {
		t.Fatalf("a classified error was replaced: %v", got)
	}
}
