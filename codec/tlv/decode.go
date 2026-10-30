package tlv

import (
	"bufio"
	"bytes"
	"encoding/binary"

	"github.com/maxid/gnety/codec/wire"
)

// Unmarshal decodes one TLV payload.
//
// The input must be a complete payload: a buffer that ends inside an entry is
// malformed, not incomplete, because a TLV payload is a datagram and there are
// no more bytes to wait for. Unmarshal bounds the input with DefaultMaxSize; use
// ParseLimit for a different budget.
//
// Decoded values are freshly allocated and never alias data, so the caller may
// reuse or overwrite the input. Errors are classified through codec/wire; see
// the package documentation.
func Unmarshal(data []byte) ([]Entry, error) { return ParseLimit(data, DefaultMaxSize) }

// ParseLimit decodes one TLV payload and rejects a payload larger than maxSize
// bytes before any entry is read. A non-positive maxSize means DefaultMaxSize;
// Unmarshal cannot be used for a larger payload, ParseLimit can.
func ParseLimit(data []byte, maxSize int) ([]Entry, error) {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	if len(data) > maxSize {
		return nil, wire.Errorf(wire.KindTooLarge, "tlv: payload of %d bytes exceeds the limit of %d", len(data), maxSize)
	}
	d := decoder{r: wire.NewReader(bufio.NewReaderSize(bytes.NewReader(data), len(data)), len(data))}
	d.r.SetMaxDepth(MaxDepth)
	entries, err := d.scope(len(data))
	if err != nil {
		return nil, datagramError(err)
	}
	return entries, nil
}

// datagramError turns a short read into a malformed error. The scope checks
// below already reject a declared length that runs past its parent, so this only
// catches a caller that hands ParseLimit a buffer that is shorter than the
// lengths it carries; there is still nothing to wait for.
func datagramError(err error) error {
	if wire.KindOf(err) == wire.KindIncomplete {
		return wire.Errorf(wire.KindMalformed, "tlv: payload ends inside an entry")
	}
	return err
}

// minEntryLen is the shortest entry the format allows: one header byte and an
// 8-bit identifier, with a length of 0 inline in the header.
const minEntryLen = 2

// decoder holds the bounded reader shared by every scope of one payload. The
// reader's budget is the payload length, so no read can pass the end of the
// buffer, and its depth counter bounds the recursion.
type decoder struct {
	r *wire.Reader
}

// scope decodes the entries that fill the next n bytes, which its parent has
// already validated. entry consumes at least its header byte, so n strictly
// decreases and the loop ends exactly at the end of the scope.
func (d *decoder) scope(n int) ([]Entry, error) {
	var entries []Entry
	for n > 0 {
		e, used, err := d.entry(n)
		if err != nil {
			return nil, err
		}
		n -= used
		entries = append(entries, e)
	}
	if err := checkUniqueIDs(entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// entry decodes one entry out of the n bytes its parent has left and reports how
// many bytes it consumed.
func (d *decoder) entry(n int) (Entry, int, error) {
	var e Entry
	if n < minEntryLen {
		return e, 0, wire.Errorf(wire.KindMalformed, "tlv: %d bytes left, too few for an entry", n)
	}
	before := d.r.Budget()

	header, err := d.r.Byte()
	if err != nil {
		return e, 0, err
	}
	e.Type = Type(header >> 6)
	idLen := 1
	if header&flagID16 != 0 {
		idLen = 2
	}
	lengthType := header & maskLen
	lengthLen := 0
	switch lengthType {
	case lenInline:
	case len8:
		lengthLen = 1
	case len16:
		lengthLen = 2
	case len24:
		lengthLen = 3
	}
	headerLen := 1 + idLen + lengthLen
	if n < headerLen {
		return e, 0, wire.Errorf(wire.KindMalformed,
			"tlv: %d bytes left, the entry header needs %d", n, headerLen)
	}

	if idLen == 2 {
		id, err := d.r.Uint16(binary.BigEndian)
		if err != nil {
			return e, 0, err
		}
		e.ID = id
	} else {
		b, err := d.r.Byte()
		if err != nil {
			return e, 0, err
		}
		e.ID = uint16(b)
	}

	length := 0
	switch lengthType {
	case lenInline:
		length = int(header & 0x07)
	case len8:
		b, err := d.r.Byte()
		if err != nil {
			return e, 0, err
		}
		length = int(b)
	case len16:
		v, err := d.r.Uint16(binary.BigEndian)
		if err != nil {
			return e, 0, err
		}
		length = int(v)
	case len24:
		b, err := d.r.ReadN(3)
		if err != nil {
			return e, 0, err
		}
		length = int(b[0])<<16 | int(b[1])<<8 | int(b[2])
	}

	// The declared length is checked against the bytes its parent carries
	// before anything is allocated from it.
	if avail := n - headerLen; length > avail {
		return e, 0, wire.Errorf(wire.KindMalformed,
			"tlv: entry %d declares %d bytes but its parent carries %d", e.ID, length, avail)
	}

	if e.Type.IsContainer() {
		if err := d.r.Enter(); err != nil {
			return e, 0, err
		}
		children, err := d.scope(length)
		d.r.Leave()
		if err != nil {
			return e, 0, err
		}
		e.Children = children
	} else if length > 0 {
		// An empty leaf keeps a nil Value, the same as an entry the caller
		// built without one, and allocates nothing.
		value, err := d.r.ReadN(length)
		if err != nil {
			return e, 0, err
		}
		e.Value = value
	}
	return e, before - d.r.Budget(), nil
}
