package mqtt

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/maxid/gnety/codec/wire"
)

// ErrNotPublish reports that the next packet on the stream is not a PUBLISH.
//
// ReadPublishInto returns it without consuming a single byte, so a caller that
// reads PUBLISH on a fast path and everything else through Decoder can simply
// fall back to Decode.
var ErrNotPublish = errors.New("mqtt: next packet is not a PUBLISH")

// PublishView is one PUBLISH decoded into caller-supplied storage by
// ReadPublishInto. It carries the same fields Decoder.Decode fills into
// *Publish, except that the variable-length ones are slices into the caller's
// buffer instead of newly allocated memory.
//
// Every slice in a PublishView — Topic, Payload and Buffer — is valid only
// until the next ReadPublishInto (or any other read) that uses the same buffer:
// the next packet overwrites those bytes. A caller that needs the topic or the
// payload to outlive the next read must copy it.
type PublishView struct {
	// Version is the protocol level the body was decoded with: Version311 or
	// Version5.
	Version byte
	// Flags is the low nibble of the fixed-header byte, exactly as it arrived.
	Flags byte
	// QoS is the quality of service selected by Flags: 0, 1 or 2.
	QoS byte
	// Dup marks a re-delivery of an earlier PUBLISH.
	Dup bool
	// Retain marks the message as retained.
	Retain bool
	// Remaining is the remaining length the fixed header declared: the size of
	// the variable header plus the payload, in bytes.
	Remaining int
	// Topic is the topic name, exactly the bytes the packet carried. It aliases
	// Buffer.
	//
	// The topic-name rules — non-empty, no wildcard characters, and at MQTT 5
	// also a topic alias that may stand in for an empty name — are the protocol
	// layer's, exactly as they are for Decoder.Decode, which returns them as a
	// string and applies neither. They can be applied to these bytes without
	// allocating a string.
	Topic []byte
	// PacketID is the packet identifier of a QoS 1/2 PUBLISH. It is zero at
	// QoS 0, which is when the wire format does not carry one.
	PacketID uint16
	// PropertiesOffset is the offset of the property block's first byte inside
	// Buffer, and PropertiesLength its length. PropertiesLength is zero when the
	// packet carried no property block, which is always the case at MQTT 3.1.1
	// and is the zero-length block an MQTT 5 PUBLISH may carry.
	PropertiesOffset int
	PropertiesLength int
	// Properties is the decoded MQTT 5 property block, nil when the packet
	// carried none (so PropertiesLength is 0). It does not alias Buffer: the
	// block is copied out of the buffer before the package's property table
	// decodes it, so the value stays valid after the next read.
	Properties *Properties
	// Payload is the message payload, aliasing Buffer.
	Payload []byte
	// Buffer is the storage the packet was read into: dst itself, or a larger
	// buffer when dst was too small. Keep it and pass it back in as the next
	// call's dst; it only ever grows, and a packet that does not fit grows it
	// once. Topic and Payload are sub-slices of it.
	Buffer []byte
}

// ReadPublishInto reads one PUBLISH packet into dst instead of allocating the
// packet, its topic string and its payload.
//
// The decode is the one Decoder.Decode performs for PUBLISH: the same
// remaining-length check against maxSize before any body buffer is used, the same
// fixed-header rules (QoS > 2, DUP with QoS 0), the same UTF-8 string rules for
// the topic, the same packet-identifier rules, no property block at MQTT 3.1.1
// and the MQTT 5 property block validated and decoded by the same property table.
// version selects the body layout (Version311 or Version5; zero means
// Version311) and maxSize bounds the remaining length (a non-positive value means
// DefaultMaxPacketSize), exactly as the two Decoder fields do.
//
// The topic-name rules a server has to enforce — a non-empty name without
// wildcards, and at MQTT 5 the topic alias that may replace an empty name — are
// not applied here, exactly as Decode does not apply them: they depend on
// connection state (the alias table) and they are reported with a protocol reason
// code rather than as an undecodable packet. Topic is returned as bytes so the
// caller can check it without allocating a string.
//
// Lifetime: Topic and Payload alias Buffer — and Buffer is dst itself unless dst
// was too small — so they are valid only until the next read that uses the same
// buffer. A caller that keeps the payload (a queued delivery, a retained message,
// a hook that stores it) copies it. Keep view.Buffer and hand it back in as dst:
// the buffer is grow-only, and one packet larger than it reallocates it once.
//
// Allocation: an MQTT 3.1.1 PUBLISH and an MQTT 5 PUBLISH with an empty property
// block decode without allocating once dst is large enough. An MQTT 5 property
// block that carries properties does allocate: it is copied out of the buffer and
// decoded by the same table Decode uses, which builds the Properties value (and
// its []byte fields own their memory). That is the documented trade-off —
// correctness and one validator for both paths first, the zero-allocation fast
// path for the layout that has no property block at all.
//
// Errors are the ones Decode returns: the stream ending inside the packet is
// wire.ErrIncomplete (still matching io.EOF), a peer violating the wire format is
// ErrMalformedPacket (classified by wire.KindOf), and a packet whose remaining
// length exceeds maxSize is reported as too large before anything is read. When
// the next packet is not a PUBLISH the call returns ErrNotPublish and consumes
// nothing.
func ReadPublishInto(r *bufio.Reader, version byte, maxSize int, dst []byte) (PublishView, error) {
	head, err := r.Peek(1)
	if err != nil {
		return PublishView{}, err
	}
	if head[0]>>4 != TypePublish {
		return PublishView{}, ErrNotPublish
	}
	// The peeked byte is copied out before the stream advances: Peek returns a
	// view of the bufio buffer, which the next read overwrites.
	flags := head[0] & 0x0f
	if _, err := r.ReadByte(); err != nil {
		return PublishView{}, err
	}
	remaining, err := ReadRemainingLength(r)
	if err != nil {
		return PublishView{}, err
	}
	if maxSize <= 0 {
		maxSize = DefaultMaxPacketSize
	}
	if remaining > maxSize {
		return PublishView{}, tooLarge("%s body of %d bytes exceeds MaxPacketSize %d", packetName(TypePublish), remaining, maxSize)
	}
	if err := checkFlags(TypePublish, flags); err != nil {
		return PublishView{}, err
	}
	if version == 0 {
		version = Version311
	}
	if version != Version311 && version != Version5 {
		return PublishView{}, unsupported("protocol level %d", version)
	}
	body, err := readBodyInto(r, dst, remaining)
	if err != nil {
		return PublishView{}, err
	}
	return decodePublishInto(body, version, flags, remaining)
}

// readBodyInto reads exactly n body bytes into dst, growing dst when it is too
// small. The returned slice has n bytes and keeps the capacity of dst, so the
// caller's buffer is grown by at most one allocation per size increase.
func readBodyInto(r *bufio.Reader, dst []byte, n int) ([]byte, error) {
	if cap(dst) < n {
		dst = make([]byte, n)
	}
	body := dst[:n]
	if n == 0 {
		return body, nil
	}
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, incompleteBody(err)
	}
	return body, nil
}

// incompleteBody reports a stream that ended inside a packet body the way
// codec/wire does for its readers: the error wraps wire.ErrIncomplete and the
// underlying io cause, so errors.Is works for both.
func incompleteBody(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: %w", wire.ErrIncomplete, err)
	}
	return err
}

// decodePublishInto decodes one PUBLISH body held in body, which is the whole
// declared remaining length of the packet.
//
// It walks the fields in the order decodePublish reads them and fails with the
// same errors: the packet identifier of a QoS 1/2 PUBLISH must not be zero, an
// MQTT 5 property block goes through decodePropertyBlock, and the payload is
// whatever the remaining length leaves over. A field that declares more bytes
// than the body carries fails the way codec/wire's budget does, so a caller that
// classifies errors by wire.KindOf sees one behaviour, not two.
func decodePublishInto(body []byte, version, flags byte, remaining int) (PublishView, error) {
	qos := (flags >> 1) & 0x03
	view := PublishView{
		Version:   version,
		Flags:     flags,
		QoS:       qos,
		Dup:       flags&0x08 != 0,
		Retain:    flags&0x01 != 0,
		Remaining: remaining,
		Buffer:    body,
	}
	c := bodyCursor{body: body}
	topic, err := readStringBytes(&c)
	if err != nil {
		return PublishView{}, err
	}
	view.Topic = topic
	// QoS 0 has no packet identifier on the wire: PacketID stays zero and every
	// remaining byte belongs to the payload.
	if qos > 0 {
		id, err := c.uint16()
		if err != nil {
			return PublishView{}, wrapErr(err)
		}
		if id == 0 {
			return PublishView{}, malformed("PUBLISH with QoS %d carries packet id 0", qos)
		}
		view.PacketID = id
	}
	if version == Version5 {
		props, off, length, err := readPropertiesInto(&c)
		if err != nil {
			return PublishView{}, err
		}
		view.Properties, view.PropertiesOffset, view.PropertiesLength = props, off, length
	}
	payload, err := c.take(c.Budget())
	if err != nil {
		return PublishView{}, wrapErr(err)
	}
	view.Payload = payload
	return view, nil
}

// readStringBytes reads one length-prefixed UTF-8 string field and returns it as
// a slice of the body it was read from. It is the in-memory counterpart of
// readString and applies the same two rules in the order the stream path applies
// them — the bytes must be valid UTF-8 (codec/wire's check and message, which is
// where the stream path catches it) and must not contain U+0000 — so both paths
// accept and reject the same fields with the same errors. publish_test.go decodes
// a corpus through both and compares the error text, which is what keeps the two
// in step.
func readStringBytes(c *bodyCursor) ([]byte, error) {
	n, err := c.uint16()
	if err != nil {
		return nil, wrapErr(err)
	}
	s, err := c.take(int(n))
	if err != nil {
		return nil, wrapErr(err)
	}
	if !utf8.Valid(s) {
		return nil, wrapErr(wire.Errorf(wire.KindMalformed, "invalid UTF-8 in %d-byte string", n))
	}
	if bytes.IndexByte(s, 0) >= 0 {
		return nil, malformed("UTF-8 string contains U+0000")
	}
	return s, nil
}

// readPropertiesInto reads one MQTT 5 property block out of the body cursor,
// returning the decoded properties and where the block sits in the buffer.
//
// The block is copied before the table decodes it: a property value that carries
// a binary field keeps a slice of the bytes it was decoded from, so decoding in
// place would leave a Properties value pointing into the caller's reusable
// buffer. The copy is the one allocation ReadPublishInto's documentation names,
// and the property rules are the table's own (decodePropertyBlock), not a second
// implementation of them.
func readPropertiesInto(c *bodyCursor) (*Properties, int, int, error) {
	n, err := readPropertyLengthInto(c)
	if err != nil {
		return nil, 0, 0, err
	}
	if n > c.Budget() {
		return nil, 0, 0, malformed("PUBLISH declares %d property bytes but its body carries %d", n, c.Budget())
	}
	if n == 0 {
		// The block is present and empty: no properties, and nothing to allocate.
		return nil, c.off, 0, nil
	}
	offset := c.off
	block, err := c.take(n)
	if err != nil {
		return nil, 0, 0, wrapErr(err)
	}
	decoded, err := decodePropertyBlock(bytes.Clone(block), ctxPublish, "PUBLISH")
	if err != nil {
		return nil, 0, 0, err
	}
	return &decoded, offset, n, nil
}

// readPropertyLengthInto reads the property length prefix of a property block out
// of the body cursor: the in-memory counterpart of readPropertyLength. The
// collected bytes go through the same takeVarint, so the width limit and the
// shortest-form rule are one implementation; only the byte source differs.
func readPropertyLengthInto(c *bodyCursor) (int, error) {
	var buf [wire.MaxVarintBytes]byte
	for n := 1; n <= len(buf); n++ {
		b, err := c.Byte()
		if err != nil {
			return 0, wrapErr(err)
		}
		buf[n-1] = b
		if b&0x80 == 0 {
			length, _, err := takeVarint(buf[:n])
			if err != nil {
				return 0, err
			}
			return length, nil
		}
	}
	return 0, malformed("property length is wider than %d bytes", wire.MaxVarintBytes)
}

// bodyCursor is a bounded in-memory view of a packet body: the counterpart of
// codec/wire's Reader for a body that is already in a buffer. It keeps the same
// budget arithmetic and the same error kinds and texts, so a field that declares
// more bytes than the packet carries is reported identically on both paths. off
// is the offset of the next byte inside the body.
type bodyCursor struct {
	body []byte
	off  int
}

// Budget reports the bytes the cursor may still consume.
func (c *bodyCursor) Budget() int { return len(c.body) }

// take reads exactly n bytes as a slice of the body.
func (c *bodyCursor) take(n int) ([]byte, error) {
	if n < 0 {
		return nil, wire.Errorf(wire.KindMalformed, "negative length %d", n)
	}
	if n > len(c.body) {
		return nil, wire.Errorf(wire.KindTooLarge, "length %d exceeds remaining budget %d", n, len(c.body))
	}
	b := c.body[:n]
	c.body = c.body[n:]
	c.off += n
	return b, nil
}

// Byte reads one byte.
func (c *bodyCursor) Byte() (byte, error) {
	b, err := c.take(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

// uint16 reads one big-endian uint16.
func (c *bodyCursor) uint16() (uint16, error) {
	b, err := c.take(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b), nil
}
