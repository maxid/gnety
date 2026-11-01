package mqtt

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

// fmtProps renders a property block for failure messages.
func fmtProps(p Properties) string { return fmt.Sprintf("%+v", p) }

// encodePublishFrame encodes one PUBLISH into its wire frame.
func encodePublishFrame(t *testing.T, m *Publish) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Encode(&buf, m); err != nil {
		t.Fatalf("Encode(%s): %v", describe(m), err)
	}
	return buf.Bytes()
}

// publishFrames is the corpus both decoders are compared on: every QoS, empty
// and large payloads, both protocol versions with and without a property block,
// and the malformed packets a PUBLISH can carry. The raw frames are spelled out
// by hand so they cannot be produced by the encoder under test.
func publishFrames(t *testing.T) []struct {
	name    string
	version byte
	frame   []byte
} {
	t.Helper()
	v4 := MessageHeader{Level: Version311}
	v5 := MessageHeader{Level: Version5}
	large := bytes.Repeat([]byte("0123456789abcdef"), 512) // 8 KiB
	return []struct {
		name    string
		version byte
		frame   []byte
	}{
		{"v4/qos0", Version311, encodePublishFrame(t, &Publish{MessageHeader: v4, Topic: "sensors/1/temp", Payload: []byte("21.5")})},
		{"v4/qos0-empty-payload", Version311, encodePublishFrame(t, &Publish{MessageHeader: v4, Topic: "a", Payload: []byte{}})},
		{"v4/qos0-large-payload", Version311, encodePublishFrame(t, &Publish{MessageHeader: v4, Topic: "sensors/1/temp", Payload: large})},
		{"v4/qos0-retain", Version311, encodePublishFrame(t, &Publish{MessageHeader: v4, Topic: "a/b", Payload: []byte{0}, Retain: true})},
		{"v4/qos1", Version311, encodePublishFrame(t, &Publish{MessageHeader: v4, Topic: "a/b", Payload: []byte("x"), QoS: 1, PacketID: 0x1234, Dup: true})},
		{"v4/qos2", Version311, encodePublishFrame(t, &Publish{MessageHeader: v4, Topic: "a/b", Payload: []byte{1, 2, 3}, QoS: 2, PacketID: 7})},
		{"v5/qos0-no-properties", Version5, encodePublishFrame(t, &Publish{MessageHeader: v5, Topic: "a/b", Payload: []byte("x")})},
		{"v5/qos1-no-properties", Version5, encodePublishFrame(t, &Publish{MessageHeader: v5, Topic: "a/b", Payload: []byte("x"), QoS: 1, PacketID: 9})},
		{"v5/qos2-empty-payload", Version5, encodePublishFrame(t, &Publish{MessageHeader: v5, Topic: "a/b", Payload: []byte{}, QoS: 2, PacketID: 99})},
		{"v5/qos1-properties", Version5, encodePublishFrame(t, &Publish{
			MessageHeader: v5,
			Topic:         "a/b",
			Payload:       []byte("payload"),
			QoS:           1,
			PacketID:      0x0102,
			Properties: Properties{
				PayloadFormat:         bytePtr(1),
				MessageExpiryInterval: 30,
				TopicAlias:            5,
				ContentType:           "text/plain",
				ResponseTopic:         "reply/here",
				CorrelationData:       []byte{9, 9},
				SubscriptionIdentifiers: []uint32{
					1, 2,
				},
				UserProperties: []UserProperty{{Key: "k", Value: "v"}},
			},
		})},
		// Malformed frames: the fixed header and the body are written by hand.
		{"empty-body", Version311, []byte{0x30, 0x00}},
		{"topic-overruns-body", Version311, []byte{0x30, 0x03, 0x00, 0x09, 'a'}},
		{"qos1-no-packet-id", Version311, []byte{0x32, 0x03, 0x00, 0x01, 'a'}},
		{"qos1-packet-id-zero", Version311, []byte{0x32, 0x05, 0x00, 0x01, 'a', 0x00, 0x00}},
		{"qos3-reserved", Version311, []byte{0x36, 0x03, 0x00, 0x01, 'a'}},
		{"qos0-with-dup", Version311, []byte{0x38, 0x03, 0x00, 0x01, 'a'}},
		{"invalid-utf8-topic", Version311, []byte{0x30, 0x03, 0x00, 0x01, 0xff}},
		{"topic-with-u0000", Version311, []byte{0x30, 0x03, 0x00, 0x01, 0x00}},
		{"v5-properties-overrun", Version5, []byte{0x30, 0x04, 0x00, 0x01, 'a', 0x05}},
		{"v5-unknown-property", Version5, []byte{0x30, 0x06, 0x00, 0x01, 'a', 0x02, 0xff, 0x00}},
		{"v5-property-not-allowed", Version5, []byte{0x30, 0x09, 0x00, 0x01, 'a', 0x05, 0x11, 0x00, 0x00, 0x00, 0x3c}},
		{"v5-repeated-property", Version5, []byte{0x30, 0x0e, 0x00, 0x01, 'a', 0x0a, 0x02, 0x00, 0x00, 0x00, 0x01, 0x02, 0x00, 0x00, 0x00, 0x02}},
		{"v5-property-length-not-minimal", Version5, []byte{0x30, 0x05, 0x00, 0x01, 'a', 0x80, 0x00}},
		{"v5-truncated-property-value", Version5, []byte{0x30, 0x06, 0x00, 0x01, 'a', 0x03, 0x03, 0x00}},
	}
}

// checkPublishPaths decodes frame with both paths and requires them to agree:
// the same accept/reject decision, the same error classification for a rejection,
// and the same field values for an accepted packet.
func checkPublishPaths(t *testing.T, name string, version byte, frame []byte, maxSize int) {
	t.Helper()
	d := &Decoder{Version: version, MaxPacketSize: maxSize}
	dec, decErr := d.Decode(bufio.NewReader(bytes.NewReader(frame)))

	// The view is read into a deliberately small buffer, so buffer growth is part
	// of every case.
	view, viewErr := ReadPublishInto(bufio.NewReader(bytes.NewReader(frame)), version, maxSize, make([]byte, 0, 8))

	if (decErr == nil) != (viewErr == nil) {
		t.Fatalf("%s: Decode err = %v, ReadPublishInto err = %v: the two decoders disagree about accepting the packet", name, decErr, viewErr)
	}
	if decErr != nil {
		if got, want := wire.KindOf(viewErr), wire.KindOf(decErr); got != want {
			t.Errorf("%s: error kind = %v, want %v (Decode: %v, ReadPublishInto: %v)", name, got, want, decErr, viewErr)
		}
		if got, want := errors.Is(viewErr, ErrMalformedPacket), errors.Is(decErr, ErrMalformedPacket); got != want {
			t.Errorf("%s: errors.Is(ErrMalformedPacket) = %v, want %v (Decode: %v, ReadPublishInto: %v)", name, got, want, decErr, viewErr)
		}
		if got, want := errors.Is(viewErr, io.EOF), errors.Is(decErr, io.EOF); got != want {
			t.Errorf("%s: errors.Is(io.EOF) = %v, want %v (Decode: %v, ReadPublishInto: %v)", name, got, want, decErr, viewErr)
		}
		if decErr.Error() != viewErr.Error() {
			t.Errorf("%s: error text differs\n Decode: %v\n   view: %v", name, decErr, viewErr)
		}
		return
	}

	p, ok := dec.(*Publish)
	if !ok {
		t.Fatalf("%s: Decode returned %T, want *Publish", name, dec)
	}
	if got, want := view.Version, p.Version(); got != want {
		t.Errorf("%s: Version = %d, want %d", name, got, want)
	}
	if got, want := view.QoS, p.QoS; got != want {
		t.Errorf("%s: QoS = %d, want %d", name, got, want)
	}
	if view.Dup != p.Dup {
		t.Errorf("%s: Dup = %v, want %v", name, view.Dup, p.Dup)
	}
	if view.Retain != p.Retain {
		t.Errorf("%s: Retain = %v, want %v", name, view.Retain, p.Retain)
	}
	if got, want := view.PacketID, p.PacketID; got != want {
		t.Errorf("%s: PacketID = %d, want %d", name, got, want)
	}
	if !bytes.Equal(view.Topic, []byte(p.Topic)) {
		t.Errorf("%s: Topic = %q, want %q", name, view.Topic, p.Topic)
	}
	if !bytes.Equal(view.Payload, p.Payload) {
		t.Errorf("%s: Payload differs (%d bytes, want %d)", name, len(view.Payload), len(p.Payload))
	}
	if len(view.Buffer) != view.Remaining {
		t.Errorf("%s: Buffer is %d bytes, want the %d-byte remaining length", name, len(view.Buffer), view.Remaining)
	}
	// The fixed-header flags the view reports must be the ones on the wire.
	wantFlags := p.QoS << 1
	if p.Dup {
		wantFlags |= 0x08
	}
	if p.Retain {
		wantFlags |= 0x01
	}
	if got := view.Flags; got != wantFlags {
		t.Errorf("%s: Flags = 0x%x, want 0x%x", name, got, wantFlags)
	}
	// The property block: present, located and decoded alike, or absent in both.
	if view.PropertiesLength == 0 {
		if !reflect.DeepEqual(p.Properties, Properties{}) {
			t.Errorf("%s: Decode has properties %s but the view reports no block", name, fmtProps(p.Properties))
		}
		if view.Properties != nil {
			t.Errorf("%s: Properties = %s, want nil for a packet with no property block", name, fmtProps(*view.Properties))
		}
		return
	}
	if view.Properties == nil {
		t.Fatalf("%s: PropertiesLength = %d but Properties is nil", name, view.PropertiesLength)
	}
	if !reflect.DeepEqual(*view.Properties, p.Properties) {
		t.Errorf("%s: Properties = %s, want %s", name, fmtProps(*view.Properties), fmtProps(p.Properties))
	}
	block := view.Buffer[view.PropertiesOffset : view.PropertiesOffset+view.PropertiesLength]
	if len(block) != view.PropertiesLength {
		t.Errorf("%s: property block offset %d + length %d leaves the %d-byte buffer",
			name, view.PropertiesOffset, view.PropertiesLength, len(view.Buffer))
	}
}

// TestReadPublishIntoMatchesDecode is the equivalence gate: every frame in the
// corpus must be accepted or rejected identically by Decoder.Decode and by
// ReadPublishInto, with the same values in the accepted cases.
func TestReadPublishIntoMatchesDecode(t *testing.T) {
	for _, tc := range publishFrames(t) {
		t.Run(tc.name, func(t *testing.T) {
			checkPublishPaths(t, tc.name, tc.version, tc.frame, DefaultMaxPacketSize)
		})
	}
}

// TestReadPublishIntoMatchesDecodeMaxPacketSize pins that the size limit is
// applied at the same place as Decode applies it: before any body byte is read.
func TestReadPublishIntoMatchesDecodeMaxPacketSize(t *testing.T) {
	for _, tc := range publishFrames(t) {
		// A limit of 4 bytes rejects every frame that carries more than a topic.
		checkPublishPaths(t, tc.name+"/maxSize", tc.version, tc.frame, 4)
	}
}

// TestReadPublishIntoAliasesBuffer pins the documented lifetime rule: Topic and
// Payload are views of the caller's buffer, so overwriting the buffer shows
// through them, and a second call with the same buffer returns the second packet.
func TestReadPublishIntoAliasesBuffer(t *testing.T) {
	first := encodePublishFrame(t, &Publish{Topic: "sensors/1/temp", Payload: []byte("21.5")})
	second := encodePublishFrame(t, &Publish{Topic: "sensors/2/temp", Payload: []byte("22.5")})

	r := bufio.NewReader(bytes.NewReader(append(append([]byte(nil), first...), second...)))
	one, err := ReadPublishInto(r, Version311, DefaultMaxPacketSize, nil)
	if err != nil {
		t.Fatalf("first ReadPublishInto: %v", err)
	}
	if got, want := string(one.Topic), "sensors/1/temp"; got != want {
		t.Fatalf("first Topic = %q, want %q", got, want)
	}
	// The topic starts right after its own two-byte length prefix, and the payload
	// is the tail of the body: both are inside the buffer the caller keeps.
	if one.Buffer[2] != one.Topic[0] {
		t.Fatalf("Topic does not alias the returned buffer")
	}
	payloadAt := len(one.Buffer) - len(one.Payload)
	if one.Buffer[payloadAt] != one.Payload[0] {
		t.Fatalf("Payload does not alias the returned buffer")
	}

	two, err := ReadPublishInto(r, Version311, DefaultMaxPacketSize, one.Buffer)
	if err != nil {
		t.Fatalf("second ReadPublishInto: %v", err)
	}
	if got, want := string(two.Topic), "sensors/2/temp"; got != want {
		t.Fatalf("second Topic = %q, want %q", got, want)
	}
	if got, want := string(two.Payload), "22.5"; got != want {
		t.Fatalf("second Payload = %q, want %q", got, want)
	}
	one.Payload[0] = 'X'
	if two.Payload[0] != 'X' {
		t.Fatalf("the two views do not share the reused buffer")
	}
}

// TestReadPublishIntoGrowsBufferOnce pins the grow-only rule a per-connection
// buffer relies on: a packet that does not fit reallocates the buffer, and the
// packet after it does not.
func TestReadPublishIntoGrowsBufferOnce(t *testing.T) {
	small := encodePublishFrame(t, &Publish{Topic: "a", Payload: []byte("x")})
	big := encodePublishFrame(t, &Publish{Topic: "a/b", Payload: bytes.Repeat([]byte("y"), 4096)})
	stream := append(append(append(append([]byte(nil), small...), big...), small...), small...)

	r := bufio.NewReader(bytes.NewReader(stream))
	dst := make([]byte, 0, 4)
	first, err := ReadPublishInto(r, Version311, DefaultMaxPacketSize, dst)
	if err != nil {
		t.Fatalf("first ReadPublishInto: %v", err)
	}
	if got, want := cap(first.Buffer), 4; got != want {
		t.Fatalf("a packet of %d body bytes grew the 4-byte buffer to %d bytes, want it left alone", len(first.Buffer), got)
	}
	second, err := ReadPublishInto(r, Version311, DefaultMaxPacketSize, first.Buffer)
	if err != nil {
		t.Fatalf("second ReadPublishInto: %v", err)
	}
	if got, want := len(second.Payload), 4096; got != want {
		t.Fatalf("payload is %d bytes, want %d", got, want)
	}
	if got, want := cap(second.Buffer), len(second.Buffer); got < want {
		t.Fatalf("buffer is %d bytes, want at least the %d-byte packet", got, want)
	}
	grown := cap(second.Buffer)
	third, err := ReadPublishInto(r, Version311, DefaultMaxPacketSize, second.Buffer)
	if err != nil {
		t.Fatalf("third ReadPublishInto: %v", err)
	}
	if got := cap(third.Buffer); got != grown {
		t.Fatalf("a small packet after the big one changed the buffer from %d to %d bytes", grown, got)
	}
}

// TestReadPublishIntoNotPublish pins the fallback contract: a packet that is not
// a PUBLISH is reported without consuming any input.
func TestReadPublishIntoNotPublish(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, &Pingreq{MessageHeader: MessageHeader{Level: Version311}}); err != nil {
		t.Fatal(err)
	}
	frame := buf.Bytes()
	r := bufio.NewReader(bytes.NewReader(frame))
	if _, err := ReadPublishInto(r, Version311, DefaultMaxPacketSize, nil); !errors.Is(err, ErrNotPublish) {
		t.Fatalf("err = %v, want ErrNotPublish", err)
	}
	if r.Buffered() != len(frame) {
		t.Fatalf("%d of %d bytes were consumed", len(frame)-r.Buffered(), len(frame))
	}
	m, err := (&Decoder{}).Decode(r)
	if err != nil {
		t.Fatalf("Decode after the fallback: %v", err)
	}
	if m.Type() != TypePingreq {
		t.Fatalf("Decode returned type %d, want PINGREQ", m.Type())
	}
}

// TestReadPublishIntoEmptyStream pins that a clean end of stream stays io.EOF,
// like Decode.
func TestReadPublishIntoEmptyStream(t *testing.T) {
	_, err := ReadPublishInto(bufio.NewReader(bytes.NewReader(nil)), Version311, DefaultMaxPacketSize, nil)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

// TestReadPublishIntoTruncated pins that a stream ending inside a packet is
// reported as incomplete input, like Decode.
func TestReadPublishIntoTruncated(t *testing.T) {
	frame := encodePublishFrame(t, &Publish{Topic: "sensors/1/temp", Payload: []byte("21.5")})
	for _, cut := range []int{1, 2, 5, len(frame) - 1} {
		truncated := frame[:cut]
		_, decErr := (&Decoder{}).Decode(bufio.NewReader(bytes.NewReader(truncated)))
		_, viewErr := ReadPublishInto(bufio.NewReader(bytes.NewReader(truncated)), Version311, DefaultMaxPacketSize, nil)
		if viewErr == nil {
			t.Fatalf("cut at %d: ReadPublishInto accepted a truncated packet", cut)
		}
		if wire.KindOf(viewErr) != wire.KindOf(decErr) {
			t.Fatalf("cut at %d: kind = %v, want %v (Decode: %v, ReadPublishInto: %v)", cut, wire.KindOf(viewErr), wire.KindOf(decErr), decErr, viewErr)
		}
		if got, want := errors.Is(viewErr, io.EOF), errors.Is(decErr, io.EOF); got != want {
			t.Fatalf("cut at %d: errors.Is(io.EOF) = %v, want %v (Decode: %v, ReadPublishInto: %v)", cut, got, want, decErr, viewErr)
		}
	}
}

// TestReadPublishIntoUnsupportedVersion pins the protocol-level rule Decode
// applies.
func TestReadPublishIntoUnsupportedVersion(t *testing.T) {
	frame := encodePublishFrame(t, &Publish{MessageHeader: MessageHeader{Level: Version5}, Topic: "a", Payload: []byte("x")})
	_, decErr := (&Decoder{Version: 3}).Decode(bufio.NewReader(bytes.NewReader(frame)))
	_, viewErr := ReadPublishInto(bufio.NewReader(bytes.NewReader(frame)), 3, DefaultMaxPacketSize, nil)
	if !errors.Is(viewErr, ErrMalformedPacket) || wire.KindOf(viewErr) != wire.KindOf(decErr) {
		t.Fatalf("ReadPublishInto err = %v, Decode err = %v", viewErr, decErr)
	}
}

// TestReadPublishIntoAllocs pins the allocation contract of the hot path: an MQTT
// 3.1.1 PUBLISH, and an MQTT 5 PUBLISH whose property block is empty, decode into
// a reusable buffer without allocating. Zero is the number this path exists for;
// a v5 packet that carries properties is documented to allocate (the block is
// copied out of the buffer and decoded by the shared property table), so it is
// measured here only to pin that it does allocate — the documented allocation
// path — rather than to pin a number that the property table owns.
func TestReadPublishIntoAllocs(t *testing.T) {
	const payloadLen = 64
	v4 := encodePublishFrame(t, &Publish{
		MessageHeader: MessageHeader{Level: Version311},
		Topic:         "bench/throughput/p0/7",
		Payload:       bytes.Repeat([]byte("p"), payloadLen),
	})
	v5 := encodePublishFrame(t, &Publish{
		MessageHeader: MessageHeader{Level: Version5},
		Topic:         "bench/throughput/p0/7",
		Payload:       bytes.Repeat([]byte("p"), payloadLen),
	})
	v5props := encodePublishFrame(t, &Publish{
		MessageHeader: MessageHeader{Level: Version5},
		Topic:         "bench/throughput/p0/7",
		Payload:       bytes.Repeat([]byte("p"), payloadLen),
		Properties:    Properties{TopicAlias: 3, ContentType: "text/plain"},
	})

	measure := func(t *testing.T, frame []byte, version byte) float64 {
		t.Helper()
		r := bufio.NewReaderSize(&frameRepeater{frame: frame}, 4096)
		dst := make([]byte, len(frame))
		view, err := ReadPublishInto(r, version, DefaultMaxPacketSize, dst)
		if err != nil {
			t.Fatalf("warm-up ReadPublishInto: %v", err)
		}
		dst = view.Buffer
		read := 0
		allocs := testing.AllocsPerRun(1000, func() {
			v, err := ReadPublishInto(r, version, DefaultMaxPacketSize, dst)
			if err != nil {
				panic(err)
			}
			read = len(v.Payload)
		})
		if read != payloadLen {
			t.Fatalf("payload is %d bytes, want %d", read, payloadLen)
		}
		return allocs
	}

	if allocs := measure(t, v4, Version311); allocs != 0 {
		t.Errorf("MQTT 3.1.1 PUBLISH into a reused buffer allocates %v times per read, want 0", allocs)
	}
	if allocs := measure(t, v5, Version5); allocs != 0 {
		t.Errorf("MQTT 5 PUBLISH with an empty property block allocates %v times per read, want 0", allocs)
	}
	if allocs := measure(t, v5props, Version5); allocs <= 0 {
		t.Errorf("MQTT 5 PUBLISH with properties allocated %v times per read, want the documented allocation path (block copy plus the decoded properties)", allocs)
	} else {
		t.Logf("MQTT 5 PUBLISH with properties allocates %.0f times per read (documented allocation path)", allocs)
	}
}

// frameRepeater serves the same frame forever, so a measurement loop never
// allocates its own input.
type frameRepeater struct {
	frame []byte
	off   int
}

func (r *frameRepeater) Read(p []byte) (int, error) {
	n := copy(p, r.frame[r.off:])
	r.off += n
	if r.off == len(r.frame) {
		r.off = 0
	}
	return n, nil
}

// BenchmarkDecodePublish is the allocation baseline of the typed decoder on the
// packet the broker reads most: a 64-byte QoS 0 PUBLISH over MQTT 3.1.1.
func BenchmarkDecodePublish(b *testing.B) {
	frame := benchmarkPublishFrame(b)
	r := bufio.NewReaderSize(&frameRepeater{frame: frame}, 4096)
	d := &Decoder{Version: Version311, MaxPacketSize: DefaultMaxPacketSize}
	b.ReportAllocs()
	b.SetBytes(int64(len(frame)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg, err := d.Decode(r)
		if err != nil {
			b.Fatalf("Decode: %v", err)
		}
		if p := msg.(*Publish); len(p.Payload) == 0 {
			b.Fatal("empty payload")
		}
	}
}

// BenchmarkReadPublishInto is the same packet through the reuse path: the topic
// and the payload are views of one buffer the caller keeps, so the read itself
// allocates nothing.
func BenchmarkReadPublishInto(b *testing.B) {
	frame := benchmarkPublishFrame(b)
	r := bufio.NewReaderSize(&frameRepeater{frame: frame}, 4096)
	dst := make([]byte, len(frame))
	b.ReportAllocs()
	b.SetBytes(int64(len(frame)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		view, err := ReadPublishInto(r, Version311, DefaultMaxPacketSize, dst)
		if err != nil {
			b.Fatalf("ReadPublishInto: %v", err)
		}
		if len(view.Payload) == 0 {
			b.Fatal("empty payload")
		}
		dst = view.Buffer
	}
}

// benchmarkPublishFrame encodes the 64-byte QoS 0 MQTT 3.1.1 PUBLISH both
// benchmarks read.
func benchmarkPublishFrame(b *testing.B) []byte {
	b.Helper()
	var buf bytes.Buffer
	err := Encode(&buf, &Publish{
		MessageHeader: MessageHeader{Level: Version311},
		Topic:         "bench/throughput/p0/7",
		Payload:       bytes.Repeat([]byte("p"), 64),
	})
	if err != nil {
		b.Fatalf("Encode: %v", err)
	}
	return buf.Bytes()
}
