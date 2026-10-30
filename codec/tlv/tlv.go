// Package tlv implements the OMA LwM2M TLV wire format: the type nibble, the
// 8- or 16-bit identifier, the inline or 8/16/24-bit length and the value or
// nested entries that follow.
//
// Every entry starts with one header byte:
//
//	bits 7-6  type
//	bit  5    0: 8-bit identifier, 1: 16-bit identifier
//	bits 4-3  00: length in bits 2-0, 01: 8-bit, 10: 16-bit, 11: 24-bit length
//	bits 2-0  the length itself when bits 4-3 are 00
//
// which is followed by the identifier, by the extended length when bits 4-3 are
// not 00, and by the payload. A payload is either a value or the concatenation
// of the child entries.
//
// Two layers are offered. Marshal and Unmarshal convert a whole list of entries
// to bytes and back; ParseLimit adds the size-bounded entry point. It converts
// bytes to entries and entries to bytes only: the object model, resource
// multiplicity, registration state, execute semantics and observe state belong
// to the LwM2M server library built on gnety, never here.
//
// # Contract
//
// The package obeys the same rules as the rest of gnety's codec kit
// (codec/wire):
//
//   - A decode never allocates from an unvalidated declared length. A length
//     that runs past the bytes its parent carries, or past the datagram, is
//     rejected as malformed before any buffer is made.
//   - Decoded values are freshly allocated and never alias the read buffer, so
//     a caller may reuse its datagram. Marshal copies values instead of keeping
//     them, so the output never aliases the caller's slices either.
//   - A TLV payload is a datagram: it is complete when it arrives, so a payload
//     that ends inside an entry is malformed, not incomplete. There is nothing
//     to wait for.
//   - Errors are classified through codec/wire. Malformed bytes report
//     wire.KindMalformed, a value or datagram that exceeds a limit reports
//     wire.KindTooLarge, and an entry with a type the format does not define
//     reports wire.KindUnsupported. Use wire.KindOf instead of string matching.
//
// # Strictness
//
// Three rules are stricter than the permissive encoder/decoder glwm2m shipped
// before this package existed, because each of them describes bytes whose
// meaning is ambiguous:
//
//   - Duplicate identifiers inside one scope are rejected: two entries with the
//     same identifier in the same list cannot be told apart after decoding.
//   - Nesting is limited to MaxDepth scopes, so a hostile payload cannot drive
//     an unbounded recursion.
//   - A container (Object Instance, Multiple Resource) that carries a Value, a
//     leaf that carries Children and a Type outside 0 to 3 are rejected instead
//     of being silently dropped or truncated.
//
// # Determinism
//
// Marshal emits the entries of one scope in ascending identifier order, at
// every level. The order is stable and total — duplicate identifiers are
// rejected — so the same tree always produces the same bytes no matter in what
// order the caller built or appended it. Unmarshal preserves the order the
// datagram carries, the same split codec/coap makes between a permissive parser
// and a canonical encoder.
package tlv

import (
	"encoding/binary"
	"sort"
	"strconv"

	"github.com/maxid/gnety/codec/wire"
)

const (
	// DefaultMaxSize bounds one TLV payload when the caller sets no limit. A
	// TLV payload travels in a CoAP message, whose UDP datagram is far
	// smaller; the bound only stops a caller from handing this package a
	// buffer it should not have allocated.
	DefaultMaxSize = 64 << 10

	// MaxDepth is the deepest nesting Marshal and Unmarshal accept: 16 nested
	// scopes, which is far beyond the three levels the object model uses
	// (object instance, multiple resource, resource instance).
	MaxDepth = 16

	// MaxValueLen is the largest payload an entry can declare, set by the
	// 24-bit length field.
	MaxValueLen = 0xffffff

	// MaxID is the largest identifier an entry can carry, set by the 16-bit
	// identifier field.
	MaxID = 0xffff
)

// Type is the LwM2M TLV type, the two most significant bits of the header.
type Type uint8

// TLV types. The values are the two-bit field, so they are the wire value.
const (
	// ObjectInstance is type 00: a container whose children are the resources
	// (or resource instances) of one object instance.
	ObjectInstance Type = 0
	// ResourceInstance is type 01: a leaf holding one instance of a multiple
	// resource.
	ResourceInstance Type = 1
	// MultipleResource is type 10: a container whose children are the
	// ResourceInstance entries of one resource.
	MultipleResource Type = 2
	// ResourceValue is type 11: a leaf holding one single-resource value.
	ResourceValue Type = 3
)

// Valid reports whether t is a type the wire format defines.
func (t Type) Valid() bool { return t <= ResourceValue }

// IsContainer reports whether the type carries child entries instead of a
// value.
func (t Type) IsContainer() bool { return t == ObjectInstance || t == MultipleResource }

// String returns the specification name of the type.
func (t Type) String() string {
	switch t {
	case ObjectInstance:
		return "object instance"
	case ResourceInstance:
		return "resource instance"
	case MultipleResource:
		return "multiple resource"
	case ResourceValue:
		return "resource with value"
	default:
		return "unknown"
	}
}

// Entry is one TLV entry.
//
// A container type (ObjectInstance, MultipleResource) uses Children and leaves
// Value empty; a leaf type (ResourceValue, ResourceInstance) uses Value and
// leaves Children empty. Marshal copies both, so the caller keeps ownership of
// the slices it passes in.
//
// The tree is exactly two levels deep in LwM2M practice — object instance over
// resources, multiple resource over resource instances — but the format and
// this package allow MaxDepth nested scopes.
type Entry struct {
	// Type is the entry type, the two-bit wire field.
	Type Type
	// ID is the resource, resource-instance or object-instance identifier.
	ID uint16
	// Value is the payload of a leaf entry.
	Value []byte
	// Children are the entries of a container.
	Children []Entry
}

// String implements fmt.Stringer for logs that must not leak the payload.
func (e Entry) String() string {
	if e.Type.IsContainer() {
		return "tlv.Entry{type=" + e.Type.String() + " id=" + strconv.Itoa(int(e.ID)) +
			" children=" + strconv.Itoa(len(e.Children)) + "}"
	}
	return "tlv.Entry{type=" + e.Type.String() + " id=" + strconv.Itoa(int(e.ID)) +
		" value=" + strconv.Itoa(len(e.Value)) + " bytes}"
}

// Marshal encodes entries as one TLV payload.
//
// Entries are emitted in ascending identifier order at every level (see the
// package documentation), values are copied, the caller's slices are not
// modified and the returned slice does not alias them.
//
// A container that carries a Value, a leaf that carries Children, a duplicate
// identifier in one scope, nesting deeper than MaxDepth, a value longer than
// MaxValueLen and a type outside 0 to 3 are all rejected with a classified
// wire error.
func Marshal(entries []Entry) ([]byte, error) {
	w := wire.NewWriter(0)
	if err := encodeScope(w, entries, 0); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

// encodeScope appends one scope: the entries that share a parent. depth is the
// number of scopes enclosing this one, so the top level is 0.
func encodeScope(w *wire.Writer, entries []Entry, depth int) error {
	if err := checkUniqueIDs(entries); err != nil {
		return err
	}
	for _, e := range sortedEntries(entries) {
		if err := encodeEntry(w, e, depth); err != nil {
			return err
		}
	}
	return nil
}

// sortedEntries returns a copy of entries ordered by identifier. The copy keeps
// Marshal from reordering the caller's slice.
func sortedEntries(entries []Entry) []Entry {
	if len(entries) < 2 {
		return entries
	}
	out := make([]Entry, len(entries))
	copy(out, entries)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// checkUniqueIDs rejects two entries that share an identifier, which would make
// the output ordering ambiguous and the decoded tree lossy.
func checkUniqueIDs(entries []Entry) error {
	if len(entries) < 2 {
		return nil
	}
	seen := make(map[uint16]struct{}, len(entries))
	for _, e := range entries {
		if _, ok := seen[e.ID]; ok {
			return wire.Errorf(wire.KindMalformed, "tlv: duplicate identifier %d in one scope", e.ID)
		}
		seen[e.ID] = struct{}{}
	}
	return nil
}

// Header bit masks of the TLV type byte.
const (
	// flagID16 marks a 16-bit identifier.
	flagID16 byte = 0x20
	// maskLen selects the length-type bits.
	maskLen byte = 0x18
)

// Length-type field values, the bits 4-3 of the header.
const (
	lenInline byte = 0x00 // length in bits 2-0, values 0 to 7
	len8      byte = 0x08
	len16     byte = 0x10
	len24     byte = 0x18
)

// encodeEntry appends one entry, headers first, then its payload.
func encodeEntry(w *wire.Writer, e Entry, depth int) error {
	if !e.Type.Valid() {
		return wire.Errorf(wire.KindUnsupported, "tlv: type %d is not a TLV type", e.Type)
	}
	payload := e.Value
	if e.Type.IsContainer() {
		if len(e.Value) > 0 {
			return wire.Errorf(wire.KindMalformed, "tlv: %s %d carries a %d-byte value", e.Type, e.ID, len(e.Value))
		}
		if depth+1 > MaxDepth {
			return wire.Errorf(wire.KindMalformed, "tlv: nesting depth exceeds %d", MaxDepth)
		}
		child := wire.NewWriter(0)
		if err := encodeScope(child, e.Children, depth+1); err != nil {
			return err
		}
		payload = child.Bytes()
	} else if len(e.Children) > 0 {
		return wire.Errorf(wire.KindMalformed, "tlv: %s %d carries %d children", e.Type, e.ID, len(e.Children))
	}
	length := len(payload)
	if length > MaxValueLen {
		return wire.Errorf(wire.KindTooLarge, "tlv: value of %d bytes exceeds %d", length, MaxValueLen)
	}

	header := byte(e.Type) << 6
	if e.ID > 0xff {
		header |= flagID16
	}
	switch {
	case length < 8:
		// The length is inline in the header.
		header |= byte(length)
	case length <= 0xff:
		header |= len8
	case length <= 0xffff:
		header |= len16
	default:
		header |= len24
	}

	w.Byte(header)
	if e.ID > 0xff {
		w.Uint16(binary.BigEndian, e.ID)
	} else {
		w.Byte(byte(e.ID))
	}
	switch {
	case length < 8:
	case length <= 0xff:
		w.Byte(byte(length))
	case length <= 0xffff:
		w.Uint16(binary.BigEndian, uint16(length))
	default:
		w.Byte(byte(length >> 16))
		w.Byte(byte(length >> 8))
		w.Byte(byte(length))
	}
	w.Raw(payload)
	return nil
}
