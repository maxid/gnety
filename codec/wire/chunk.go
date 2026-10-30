package wire

// ChunkSeq reassembles a numbered chunk sequence: CoAP Block1/Block2 bodies,
// WebSocket continuation frames, or any protocol that ships a message as
// contiguous, numbered chunks.
//
// The sequence owns its buffer. Add copies each chunk, so the caller may reuse
// or mutate its slice afterwards. A gap, a duplicate, a chunk after the last one
// and an over-long message are all rejected without publishing a partial body.
type ChunkSeq struct {
	// MaxTotal bounds the assembled message. A non-positive value means
	// DefaultMaxFrame.
	MaxTotal int

	next int
	buf  []byte
	done bool
}

// Reset prepares the sequence for a new message.
func (c *ChunkSeq) Reset() {
	c.next = 0
	c.buf = c.buf[:0]
	c.done = false
}

// Len returns the bytes assembled so far.
func (c *ChunkSeq) Len() int { return len(c.buf) }

// Done reports whether the last chunk has been accepted.
func (c *ChunkSeq) Done() bool { return c.done }

// Add appends chunk num (zero-based, contiguous) and reports whether the
// message is complete.
func (c *ChunkSeq) Add(num int, data []byte, last bool) (bool, error) {
	if c.done {
		return true, Errorf(KindMalformed, "chunk %d after the last chunk", num)
	}
	if num != c.next {
		return false, Errorf(KindMalformed, "chunk %d, want %d", num, c.next)
	}
	max := c.MaxTotal
	if max <= 0 {
		max = DefaultMaxFrame
	}
	if len(c.buf)+len(data) > max {
		return false, Errorf(KindTooLarge, "assembled message exceeds %d bytes", max)
	}
	c.buf = append(c.buf, data...)
	c.next++
	if last {
		c.done = true
	}
	return c.done, nil
}

// Bytes returns the assembled message. Call it only after Add reported
// completion; the slice belongs to the sequence and is invalidated by Reset.
func (c *ChunkSeq) Bytes() []byte { return c.buf }
