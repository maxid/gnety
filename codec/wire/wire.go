// Package wire holds the byte-level building blocks shared by gnety's protocol
// codecs: bounded readers and writers, length prefixes, varints, bounded UTF-8
// strings and chunked reassembly.
//
// Every codec in this module obeys the same contract:
//
//   - A decode never allocates from an unvalidated declared length. The declared
//     length is checked against the frame budget before any buffer is made.
//   - Decoded bytes are freshly allocated and never alias the read buffer, so a
//     caller may keep them while the transport reuses its own memory.
//   - One Decode call yields at most one frame; a stream codec leaves the rest
//     of the input in the reader.
//   - Incomplete input is reported as ErrIncomplete, not as a broken peer: the
//     caller waits for more bytes instead of closing the session.
//   - Errors are classified (KindOf) so that "need more bytes", "peer sent
//     garbage", "too large to accept" and "not supported" stay distinguishable
//     without string matching.
//
// State machines (retransmit timers, dedup tables, correlation registries,
// session or presence bookkeeping) do not belong here or in any codec package:
// they belong to the protocol library that owns the protocol.
package wire

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// DefaultMaxFrame bounds a single frame when the caller gives no budget.
const DefaultMaxFrame = 4 << 20

// Kind classifies a wire error.
type Kind uint8

// Wire error kinds. KindUnknown means the error did not originate in this
// package (for example a closed connection).
const (
	KindUnknown Kind = iota
	KindIncomplete
	KindMalformed
	KindTooLarge
	KindUnsupported
)

// Sentinel errors. Every classified error wraps exactly one of them, so
// errors.Is works across packages that only import these sentinels.
var (
	ErrIncomplete  = errors.New("wire: incomplete input")
	ErrMalformed   = errors.New("wire: malformed input")
	ErrTooLarge    = errors.New("wire: frame too large")
	ErrUnsupported = errors.New("wire: unsupported")
)

func (k Kind) String() string {
	switch k {
	case KindIncomplete:
		return "incomplete"
	case KindMalformed:
		return "malformed"
	case KindTooLarge:
		return "too large"
	case KindUnsupported:
		return "unsupported"
	default:
		return "unknown"
	}
}

// Errorf builds an error of the given kind, wrapping the matching sentinel.
func Errorf(k Kind, format string, args ...any) error {
	return fmt.Errorf("%w: %s", sentinel(k), fmt.Sprintf(format, args...))
}

func sentinel(k Kind) error {
	switch k {
	case KindIncomplete:
		return ErrIncomplete
	case KindMalformed:
		return ErrMalformed
	case KindTooLarge:
		return ErrTooLarge
	case KindUnsupported:
		return ErrUnsupported
	default:
		return fmt.Errorf("wire: error")
	}
}

// KindOf reports the kind of a wire error, or KindUnknown.
func KindOf(err error) Kind {
	switch {
	case err == nil:
		return KindUnknown
	case errors.Is(err, ErrIncomplete):
		return KindIncomplete
	case errors.Is(err, ErrMalformed):
		return KindMalformed
	case errors.Is(err, ErrTooLarge):
		return KindTooLarge
	case errors.Is(err, ErrUnsupported):
		return KindUnsupported
	default:
		return KindUnknown
	}
}

// incomplete turns a short-read error into ErrIncomplete.
func incomplete(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: %v", ErrIncomplete, err)
	}
	return err
}

// Reader is a bounded reader over a stream. Its budget shrinks as bytes are
// consumed, so a frame cannot outgrow the limit declared for it.
type Reader struct {
	r        *bufio.Reader
	budget   int
	maxDepth int
	depth    int
}

// NewReader wraps r with a byte budget. A non-positive budget means
// DefaultMaxFrame.
func NewReader(r *bufio.Reader, budget int) *Reader {
	if budget <= 0 {
		budget = DefaultMaxFrame
	}
	return &Reader{r: r, budget: budget, maxDepth: 16}
}

// Budget returns the bytes this reader may still consume.
func (r *Reader) Budget() int { return r.budget }

// MaxDepth returns the nesting limit used by Enter.
func (r *Reader) MaxDepth() int { return r.maxDepth }

// SetMaxDepth overrides the nesting limit. A non-positive value disables it.
func (r *Reader) SetMaxDepth(n int) { r.maxDepth = n }

// Depth returns the current nesting depth.
func (r *Reader) Depth() int { return r.depth }

// Enter increases the nesting depth, failing when the limit is exceeded. Every
// successful Enter must be paired with a Leave.
func (r *Reader) Enter() error {
	if r.maxDepth > 0 && r.depth+1 > r.maxDepth {
		return Errorf(KindMalformed, "nesting depth exceeds %d", r.maxDepth)
	}
	r.depth++
	return nil
}

// Leave decreases the nesting depth.
func (r *Reader) Leave() {
	if r.depth > 0 {
		r.depth--
	}
}

// take reads exactly n bytes, validating the length before allocating.
func (r *Reader) take(n int) ([]byte, error) {
	if n < 0 {
		return nil, Errorf(KindMalformed, "negative length %d", n)
	}
	if n > r.budget {
		return nil, Errorf(KindTooLarge, "length %d exceeds remaining budget %d", n, r.budget)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r.r, buf); err != nil {
		return nil, incomplete(err)
	}
	r.budget -= n
	return buf, nil
}

// ReadN reads exactly n bytes. The returned slice is newly allocated.
func (r *Reader) ReadN(n int) ([]byte, error) { return r.take(n) }

// Skip discards n bytes.
func (r *Reader) Skip(n int) error {
	_, err := r.take(n)
	return err
}

// Byte reads one byte.
func (r *Reader) Byte() (byte, error) {
	b, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

// Uint16 reads a 16-bit unsigned integer.
func (r *Reader) Uint16(o binary.ByteOrder) (uint16, error) {
	b, err := r.take(2)
	if err != nil {
		return 0, err
	}
	return o.Uint16(b), nil
}

// Uint32 reads a 32-bit unsigned integer.
func (r *Reader) Uint32(o binary.ByteOrder) (uint32, error) {
	b, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return o.Uint32(b), nil
}

// Uint64 reads a 64-bit unsigned integer.
func (r *Reader) Uint64(o binary.ByteOrder) (uint64, error) {
	b, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return o.Uint64(b), nil
}

// Varint reads a base-128 varint of at most maxBytes bytes (0 means
// MaxVarintBytes).
func (r *Reader) Varint(maxBytes int) (uint64, error) {
	v, n, err := readVarint(r.r, maxBytes)
	if err != nil {
		return 0, err
	}
	r.budget -= n
	return v, nil
}

// UTF8 reads exactly n bytes and validates them as UTF-8.
func (r *Reader) UTF8(n int) (string, error) {
	b, err := r.take(n)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", Errorf(KindMalformed, "invalid UTF-8 in %d-byte string", n)
	}
	return string(b), nil
}

// String8 reads a 1-byte length prefix followed by that many UTF-8 bytes.
func (r *Reader) String8() (string, error) {
	n, err := r.Byte()
	if err != nil {
		return "", err
	}
	return r.UTF8(int(n))
}

// String16 reads a 2-byte length prefix (in order o) followed by that many
// UTF-8 bytes.
func (r *Reader) String16(o binary.ByteOrder) (string, error) {
	n, err := r.Uint16(o)
	if err != nil {
		return "", err
	}
	return r.UTF8(int(n))
}

// Raw8 reads a 1-byte length prefix followed by that many opaque bytes.
func (r *Reader) Raw8() ([]byte, error) {
	n, err := r.Byte()
	if err != nil {
		return nil, err
	}
	return r.ReadN(int(n))
}

// Raw16 reads a 2-byte length prefix (in order o) followed by that many opaque
// bytes.
func (r *Reader) Raw16(o binary.ByteOrder) ([]byte, error) {
	n, err := r.Uint16(o)
	if err != nil {
		return nil, err
	}
	return r.ReadN(int(n))
}

// Writer builds a frame in memory. Its methods never fail except where a value
// cannot be represented (a string longer than its length prefix).
type Writer struct {
	buf []byte
}

// NewWriter returns a writer with the given initial capacity.
func NewWriter(capacity int) *Writer {
	if capacity < 0 {
		capacity = 0
	}
	return &Writer{buf: make([]byte, 0, capacity)}
}

// Len returns the number of bytes written so far.
func (w *Writer) Len() int { return len(w.buf) }

// Bytes returns the written bytes. The slice is owned by the writer: copy it
// before retaining it across further writes.
func (w *Writer) Bytes() []byte { return w.buf }

// Reset drops the written bytes and keeps the capacity.
func (w *Writer) Reset() { w.buf = w.buf[:0] }

// Raw appends b verbatim.
func (w *Writer) Raw(b []byte) *Writer {
	w.buf = append(w.buf, b...)
	return w
}

// Byte appends one byte.
func (w *Writer) Byte(v byte) *Writer {
	w.buf = append(w.buf, v)
	return w
}

// Uint16 appends a 16-bit unsigned integer.
func (w *Writer) Uint16(o binary.ByteOrder, v uint16) *Writer {
	var b [2]byte
	o.PutUint16(b[:], v)
	return w.Raw(b[:])
}

// Uint32 appends a 32-bit unsigned integer.
func (w *Writer) Uint32(o binary.ByteOrder, v uint32) *Writer {
	var b [4]byte
	o.PutUint32(b[:], v)
	return w.Raw(b[:])
}

// Uint64 appends a 64-bit unsigned integer.
func (w *Writer) Uint64(o binary.ByteOrder, v uint64) *Writer {
	var b [8]byte
	o.PutUint64(b[:], v)
	return w.Raw(b[:])
}

// Varint appends a base-128 varint.
func (w *Writer) Varint(v uint64) *Writer {
	w.buf = AppendVarint(w.buf, v)
	return w
}

// String8 appends a 1-byte length prefix and a UTF-8 string.
func (w *Writer) String8(s string) error {
	if len(s) > 0xff {
		return Errorf(KindTooLarge, "string8 length %d exceeds 255", len(s))
	}
	if !utf8.ValidString(s) {
		return Errorf(KindMalformed, "string8 holds invalid UTF-8")
	}
	return w.Raw8([]byte(s))
}

// String16 appends a 2-byte length prefix (in order o) and a UTF-8 string.
func (w *Writer) String16(o binary.ByteOrder, s string) error {
	if len(s) > 0xffff {
		return Errorf(KindTooLarge, "string16 length %d exceeds 65535", len(s))
	}
	if !utf8.ValidString(s) {
		return Errorf(KindMalformed, "string16 holds invalid UTF-8")
	}
	return w.Raw16(o, []byte(s))
}

// Raw8 appends a 1-byte length prefix and opaque bytes.
func (w *Writer) Raw8(b []byte) error {
	if len(b) > 0xff {
		return Errorf(KindTooLarge, "raw8 length %d exceeds 255", len(b))
	}
	w.Byte(byte(len(b)))
	w.Raw(b)
	return nil
}

// Raw16 appends a 2-byte length prefix (in order o) and opaque bytes.
func (w *Writer) Raw16(o binary.ByteOrder, b []byte) error {
	if len(b) > 0xffff {
		return Errorf(KindTooLarge, "raw16 length %d exceeds 65535", len(b))
	}
	w.Uint16(o, uint16(len(b)))
	w.Raw(b)
	return nil
}
