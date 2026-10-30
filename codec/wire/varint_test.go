package wire

import (
	"bufio"
	"bytes"
	"testing"
)

func TestVarintRoundTrip(t *testing.T) {
	for _, want := range []uint64{0, 1, 127, 128, 16383, 16384, 2097151, MaxVarintValue} {
		enc := AppendVarint(nil, want)
		if got, l := VarintLen(want), len(enc); got != l {
			t.Fatalf("VarintLen(%d) = %d, encoded %d bytes", want, got, l)
		}
		got, err := ReadVarint(bufio.NewReader(bytes.NewReader(enc)), 0)
		if err != nil || got != want {
			t.Fatalf("ReadVarint(%x) = %d, %v; want %d", enc, got, err, want)
		}
	}
}

func TestVarintRejectsNonMinimal(t *testing.T) {
	// 0x80 0x00 encodes 0 with two bytes; the shortest form is 0x00.
	_, err := ReadVarint(bufio.NewReader(bytes.NewReader([]byte{0x80, 0x00})), 0)
	if KindOf(err) != KindMalformed {
		t.Fatalf("kind = %v, want malformed (%v)", KindOf(err), err)
	}
}

func TestVarintRejectsOverlong(t *testing.T) {
	_, err := ReadVarint(bufio.NewReader(bytes.NewReader([]byte{0x80, 0x80, 0x80, 0x80, 0x01})), 0)
	if KindOf(err) != KindMalformed {
		t.Fatalf("kind = %v, want malformed (%v)", KindOf(err), err)
	}
}

func TestVarintRespectsMaxBytes(t *testing.T) {
	_, err := ReadVarint(bufio.NewReader(bytes.NewReader([]byte{0x80, 0x01})), 1)
	if KindOf(err) != KindMalformed {
		t.Fatalf("kind = %v, want malformed (%v)", KindOf(err), err)
	}
}

func TestVarintIncomplete(t *testing.T) {
	_, err := ReadVarint(bufio.NewReader(bytes.NewReader([]byte{0x80})), 0)
	if KindOf(err) != KindIncomplete {
		t.Fatalf("kind = %v, want incomplete (%v)", KindOf(err), err)
	}
}

func TestReaderVarintConsumesBudget(t *testing.T) {
	r := newTestReader([]byte{0x96, 0x01}, 2) // 150
	v, err := r.Varint(0)
	if err != nil || v != 150 {
		t.Fatalf("Varint = %d, %v", v, err)
	}
	if r.Budget() != 0 {
		t.Fatalf("budget = %d, want 0", r.Budget())
	}
}

func TestAppendVarintClampsAboveMax(t *testing.T) {
	enc := AppendVarint(nil, MaxVarintValue+1)
	got, err := ReadVarint(bufio.NewReader(bytes.NewReader(enc)), 0)
	if err != nil || got != MaxVarintValue {
		t.Fatalf("clamped varint = %d, %v; want %d", got, err, MaxVarintValue)
	}
}
