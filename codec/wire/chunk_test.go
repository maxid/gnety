package wire

import (
	"bytes"
	"testing"
)

func TestChunkSeqAssembles(t *testing.T) {
	var c ChunkSeq
	first := []byte("hello ")
	done, err := c.Add(0, first, false)
	if err != nil || done {
		t.Fatalf("first chunk: done=%v err=%v", done, err)
	}
	// The sequence owns its buffer: mutating the caller's slice must not leak in.
	copy(first, "XXXXX ")

	done, err = c.Add(1, []byte("world"), true)
	if err != nil || !done {
		t.Fatalf("last chunk: done=%v err=%v", done, err)
	}
	if got := string(c.Bytes()); got != "hello world" {
		t.Fatalf("assembled = %q", got)
	}
	if c.Len() != 11 || !c.Done() {
		t.Fatalf("len=%d done=%v", c.Len(), c.Done())
	}
}

func TestChunkSeqRejectsGapAndDuplicate(t *testing.T) {
	var c ChunkSeq
	if _, err := c.Add(1, []byte("x"), false); KindOf(err) != KindMalformed {
		t.Fatalf("gap: kind = %v, want malformed", KindOf(err))
	}
	if _, err := c.Add(0, []byte("x"), false); err != nil {
		t.Fatalf("chunk 0: %v", err)
	}
	if _, err := c.Add(0, []byte("x"), false); KindOf(err) != KindMalformed {
		t.Fatalf("duplicate: kind = %v, want malformed", KindOf(err))
	}
	if got := string(c.Bytes()); got != "x" {
		t.Fatalf("rejected chunk changed the buffer: %q", got)
	}
}

func TestChunkSeqRejectsChunkAfterLast(t *testing.T) {
	var c ChunkSeq
	if _, err := c.Add(0, []byte("x"), true); err != nil {
		t.Fatalf("last chunk: %v", err)
	}
	if _, err := c.Add(1, []byte("y"), true); KindOf(err) != KindMalformed {
		t.Fatalf("after last: kind = %v, want malformed", KindOf(err))
	}
}

func TestChunkSeqBoundsTotal(t *testing.T) {
	c := ChunkSeq{MaxTotal: 4}
	if _, err := c.Add(0, bytes.Repeat([]byte("a"), 3), false); err != nil {
		t.Fatalf("chunk 0: %v", err)
	}
	if _, err := c.Add(1, bytes.Repeat([]byte("a"), 2), true); KindOf(err) != KindTooLarge {
		t.Fatalf("overflow: kind = %v, want too large", KindOf(err))
	}
	if c.Len() != 3 || c.Done() {
		t.Fatalf("rejected chunk changed state: len=%d done=%v", c.Len(), c.Done())
	}
}

func TestChunkSeqReset(t *testing.T) {
	var c ChunkSeq
	if _, err := c.Add(0, []byte("abc"), true); err != nil {
		t.Fatalf("chunk 0: %v", err)
	}
	c.Reset()
	if c.Len() != 0 || c.Done() {
		t.Fatalf("after Reset: len=%d done=%v", c.Len(), c.Done())
	}
	if _, err := c.Add(0, []byte("de"), true); err != nil {
		t.Fatalf("chunk 0 after reset: %v", err)
	}
	if got := string(c.Bytes()); got != "de" {
		t.Fatalf("assembled after reset = %q", got)
	}
}
