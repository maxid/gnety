package wire

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func newTestReader(data []byte, budget int) *Reader {
	return NewReader(bufio.NewReader(bytes.NewReader(data)), budget)
}

func TestReaderValidatesLengthBeforeAllocation(t *testing.T) {
	r := newTestReader([]byte{1, 2, 3}, 2)
	if _, err := r.ReadN(3); KindOf(err) != KindTooLarge {
		t.Fatalf("ReadN over budget: kind = %v, want too large (err %v)", KindOf(err), err)
	}
	if r.Budget() != 2 {
		t.Fatalf("budget after rejected read = %d, want 2", r.Budget())
	}
}

func TestReaderNegativeLengthIsMalformed(t *testing.T) {
	r := newTestReader(nil, 8)
	if _, err := r.ReadN(-1); KindOf(err) != KindMalformed {
		t.Fatalf("kind = %v, want malformed", KindOf(err))
	}
}

func TestReaderIncomplete(t *testing.T) {
	r := newTestReader([]byte{0x00}, 8)
	if _, err := r.Uint16(binary.BigEndian); KindOf(err) != KindIncomplete {
		t.Fatalf("short uint16: kind = %v, want incomplete", KindOf(err))
	}
}

func TestReaderIntegers(t *testing.T) {
	w := NewWriter(16)
	w.Byte(0x7f).Uint16(binary.BigEndian, 0x1234).Uint32(binary.LittleEndian, 0xdeadbeef).Uint64(binary.BigEndian, 1<<40)

	r := newTestReader(w.Bytes(), 64)
	b, err := r.Byte()
	if err != nil || b != 0x7f {
		t.Fatalf("Byte = %#x, %v", b, err)
	}
	if v, err := r.Uint16(binary.BigEndian); err != nil || v != 0x1234 {
		t.Fatalf("Uint16 = %#x, %v", v, err)
	}
	if v, err := r.Uint32(binary.LittleEndian); err != nil || v != 0xdeadbeef {
		t.Fatalf("Uint32 = %#x, %v", v, err)
	}
	if v, err := r.Uint64(binary.BigEndian); err != nil || v != 1<<40 {
		t.Fatalf("Uint64 = %#x, %v", v, err)
	}
}

func TestReaderUTF8(t *testing.T) {
	valid := append([]byte{byte(len("设备"))}, []byte("设备")...)
	r := newTestReader(valid, 16)
	s, err := r.String8()
	if err != nil || s != "设备" {
		t.Fatalf("String8 = %q, %v", s, err)
	}

	bad := newTestReader([]byte{0x01, 0xff}, 16)
	if _, err := bad.String8(); KindOf(err) != KindMalformed {
		t.Fatalf("invalid UTF-8: kind = %v, want malformed", KindOf(err))
	}

	long := newTestReader([]byte{0x05, 'a'}, 16)
	if _, err := long.String8(); KindOf(err) != KindIncomplete {
		t.Fatalf("truncated string: kind = %v, want incomplete", KindOf(err))
	}
}

func TestReaderDepth(t *testing.T) {
	r := newTestReader(nil, 8)
	r.SetMaxDepth(2)
	if err := r.Enter(); err != nil {
		t.Fatalf("first Enter: %v", err)
	}
	if err := r.Enter(); err != nil {
		t.Fatalf("second Enter: %v", err)
	}
	if err := r.Enter(); KindOf(err) != KindMalformed {
		t.Fatalf("third Enter: kind = %v, want malformed", KindOf(err))
	}
	r.Leave()
	r.Leave()
	if r.Depth() != 0 {
		t.Fatalf("depth = %d, want 0", r.Depth())
	}
}

func TestWriterLengthPrefixes(t *testing.T) {
	w := NewWriter(8)
	if err := w.String8("ok"); err != nil {
		t.Fatalf("String8: %v", err)
	}
	if err := w.Raw16(binary.BigEndian, []byte{0xaa, 0xbb}); err != nil {
		t.Fatalf("Raw16: %v", err)
	}

	r := newTestReader(w.Bytes(), 32)
	if s, err := r.String8(); err != nil || s != "ok" {
		t.Fatalf("read String8 = %q, %v", s, err)
	}
	if b, err := r.Raw16(binary.BigEndian); err != nil || !bytes.Equal(b, []byte{0xaa, 0xbb}) {
		t.Fatalf("read Raw16 = %x, %v", b, err)
	}
	if r.Budget() != 25 {
		t.Fatalf("budget = %d, want 25", r.Budget())
	}
}

func TestWriterRejectsOverlongValues(t *testing.T) {
	w := NewWriter(0)
	if err := w.String8(strings.Repeat("a", 256)); KindOf(err) != KindTooLarge {
		t.Fatalf("long String8: kind = %v, want too large", KindOf(err))
	}
	if err := w.String8("\xff"); KindOf(err) != KindMalformed {
		t.Fatalf("invalid String8: kind = %v, want malformed", KindOf(err))
	}
	if w.Len() != 0 {
		t.Fatalf("rejected writes left %d bytes", w.Len())
	}
}

func TestKindOfUnclassified(t *testing.T) {
	if KindOf(nil) != KindUnknown {
		t.Fatal("nil error should be KindUnknown")
	}
	if KindOf(errors.New("boom")) != KindUnknown {
		t.Fatal("foreign error should be KindUnknown")
	}
	if kinds := []Kind{KindIncomplete, KindMalformed, KindTooLarge, KindUnsupported}; kinds[1].String() != "malformed" {
		t.Fatalf("Kind.String = %q", kinds[1].String())
	}
}
