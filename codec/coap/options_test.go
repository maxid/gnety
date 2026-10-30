package coap

import (
	"bytes"
	"reflect"
	"testing"
)

// mustSet fails the test when a typed setter reports an error.
func mustSet(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("setter failed: %v", err)
	}
}

func TestOptionNumbers(t *testing.T) {
	numbers := []struct {
		number uint16
		name   string
	}{
		{OptionIfMatch, "If-Match"},
		{OptionETag, "ETag"},
		{OptionIfNoneMatch, "If-None-Match"},
		{OptionObserve, "Observe"},
		{OptionLocationPath, "Location-Path"},
		{OptionURIPath, "Uri-Path"},
		{OptionContentFormat, "Content-Format"},
		{OptionMaxAge, "Max-Age"},
		{OptionURIQuery, "Uri-Query"},
		{OptionAccept, "Accept"},
		{OptionLocationQuery, "Location-Query"},
		{OptionBlock2, "Block2"},
		{OptionBlock1, "Block1"},
		{OptionSize2, "Size2"},
		{OptionSize1, "Size1"},
	}
	want := []uint16{1, 4, 5, 6, 8, 11, 12, 14, 15, 17, 20, 23, 27, 28, 60}
	for i, c := range numbers {
		if c.number != want[i] {
			t.Fatalf("%s has number %d, want %d", c.name, c.number, want[i])
		}
		def, ok := optionDefs[c.number]
		if !ok || def.name != c.name {
			t.Fatalf("optionDefs[%d] = %+v, want name %q", c.number, def, c.name)
		}
	}
	if !optionDefs[OptionURIPath].repeatable || !optionDefs[OptionETag].repeatable {
		t.Fatal("Uri-Path and ETag must be repeatable")
	}
	if optionDefs[OptionContentFormat].repeatable || optionDefs[OptionBlock1].repeatable {
		t.Fatal("Content-Format and Block1 must not be repeatable")
	}
}

func TestContentFormatValues(t *testing.T) {
	cases := []struct {
		format ContentFormat
		value  uint16
		text   string
	}{
		{TextPlain, 0, "text/plain"},
		{ApplicationLinkFormat, 40, "application/link-format"},
		{ApplicationXML, 41, "application/xml"},
		{ApplicationOctetStream, 42, "application/octet-stream"},
		{ApplicationJSON, 50, "application/json"},
		{ApplicationCBOR, 60, "application/cbor"},
	}
	for _, c := range cases {
		if uint16(c.format) != c.value {
			t.Fatalf("%s = %d, want %d", c.text, uint16(c.format), c.value)
		}
		if got := c.format.String(); got != c.text {
			t.Fatalf("ContentFormat(%d).String() = %q, want %q", c.value, got, c.text)
		}
	}
	if got := ContentFormat(999).String(); got != "content-format 999" {
		t.Fatalf("unnamed ContentFormat.String() = %q", got)
	}
}

// typedMessage builds a message that carries every option the typed layer
// models.
func typedMessage(t *testing.T) Message {
	t.Helper()
	msg := Message{
		Version:   Version,
		Type:      Confirmable,
		Code:      CodePOST,
		MessageID: 0x1234,
		Token:     []byte{0xaa, 0xbb},
		Payload:   []byte("payload"),
	}
	mustSet(t, msg.SetURIPath("sensors", "temp"))
	mustSet(t, msg.SetURIQuery("a=1", "b=2"))
	msg.SetContentFormat(ApplicationJSON)
	msg.SetAccept(TextPlain)
	msg.SetMaxAge(120)
	mustSet(t, msg.AddETag([]byte{0x01}))
	mustSet(t, msg.AddETag([]byte{0x02, 0x03}))
	mustSet(t, msg.AddIfMatch([]byte{0x04}))
	msg.SetIfNoneMatch(true)
	mustSet(t, msg.SetLocationPath("created", "here"))
	mustSet(t, msg.SetLocationQuery("c=3"))
	mustSet(t, msg.SetObserve(Observe(16)))
	mustSet(t, msg.SetBlock1(Block{Num: 1, More: true, SizeExp: 2}))
	mustSet(t, msg.SetBlock2(Block{Num: 2, More: false, SizeExp: 6}))
	msg.SetSize1(4096)
	msg.SetSize2(8192)
	return msg
}

// checkTypedMessage verifies every typed accessor of a message built by
// typedMessage.
func checkTypedMessage(t *testing.T, msg Message) {
	t.Helper()
	if got := msg.URIPath(); !reflect.DeepEqual(got, []string{"sensors", "temp"}) {
		t.Fatalf("URIPath() = %q", got)
	}
	if got := msg.URIQuery(); !reflect.DeepEqual(got, []string{"a=1", "b=2"}) {
		t.Fatalf("URIQuery() = %q", got)
	}
	if got := msg.LocationPath(); !reflect.DeepEqual(got, []string{"created", "here"}) {
		t.Fatalf("LocationPath() = %q", got)
	}
	if got := msg.LocationQuery(); !reflect.DeepEqual(got, []string{"c=3"}) {
		t.Fatalf("LocationQuery() = %q", got)
	}
	if format, ok := msg.ContentFormat(); !ok || format != ApplicationJSON {
		t.Fatalf("ContentFormat() = %v, %v", format, ok)
	}
	if format, ok := msg.Accept(); !ok || format != TextPlain {
		t.Fatalf("Accept() = %v, %v", format, ok)
	}
	if maxAge, ok := msg.MaxAge(); !ok || maxAge != 120 {
		t.Fatalf("MaxAge() = %d, %v", maxAge, ok)
	}
	if got := msg.ETags(); !reflect.DeepEqual(got, [][]byte{{0x01}, {0x02, 0x03}}) {
		t.Fatalf("ETags() = %v", got)
	}
	if got := msg.IfMatches(); !reflect.DeepEqual(got, [][]byte{{0x04}}) {
		t.Fatalf("IfMatches() = %v", got)
	}
	if !msg.IfNoneMatch() {
		t.Fatal("IfNoneMatch() = false")
	}
	if observe, ok := msg.Observe(); !ok || observe != 16 {
		t.Fatalf("Observe() = %d, %v", observe, ok)
	}
	if block, ok := msg.Block1(); !ok || block != (Block{Num: 1, More: true, SizeExp: 2}) {
		t.Fatalf("Block1() = %+v, %v", block, ok)
	}
	if block, ok := msg.Block2(); !ok || block != (Block{Num: 2, More: false, SizeExp: 6}) {
		t.Fatalf("Block2() = %+v, %v", block, ok)
	}
	if size, ok := msg.Size1(); !ok || size != 4096 {
		t.Fatalf("Size1() = %d, %v", size, ok)
	}
	if size, ok := msg.Size2(); !ok || size != 8192 {
		t.Fatalf("Size2() = %d, %v", size, ok)
	}
	for i := 1; i < len(msg.Options); i++ {
		if msg.Options[i-1].Number > msg.Options[i].Number {
			t.Fatalf("options are not sorted: %v", msg.Options)
		}
	}
}

func TestTypedOptionsRoundTrip(t *testing.T) {
	msg := typedMessage(t)
	checkTypedMessage(t, msg)

	raw, err := msg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Options, msg.Options) {
		t.Fatalf("options after round trip = %v, want %v", got.Options, msg.Options)
	}
	checkTypedMessage(t, got)

	strict, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode of a message built with the typed setters failed: %v", err)
	}
	if !bytes.Equal(strict, raw) {
		t.Fatalf("Encode = % x, Marshal = % x", strict, raw)
	}
}

func TestTypedOptionAccessorsAbsent(t *testing.T) {
	var msg Message
	if got := msg.URIPath(); got != nil {
		t.Fatalf("URIPath() = %v", got)
	}
	if _, ok := msg.ContentFormat(); ok {
		t.Fatal("ContentFormat() reports an option on an empty message")
	}
	if _, ok := msg.MaxAge(); ok {
		t.Fatal("MaxAge() reports an option on an empty message")
	}
	if msg.IfNoneMatch() {
		t.Fatal("IfNoneMatch() reports an option on an empty message")
	}
	if _, ok := msg.Observe(); ok {
		t.Fatal("Observe() reports an option on an empty message")
	}
	if _, ok := msg.Block1(); ok {
		t.Fatal("Block1() reports an option on an empty message")
	}
	if _, ok := msg.Size2(); ok {
		t.Fatal("Size2() reports an option on an empty message")
	}
	if got := msg.ETags(); got != nil {
		t.Fatalf("ETags() = %v", got)
	}
}

func TestUintOptionEncodings(t *testing.T) {
	cases := []struct {
		name   string
		number uint16
		set    func(*Message)
		raw    []byte
		read   func(Message) (uint32, bool)
		value  uint32
	}{
		{"content-format/text-plain", OptionContentFormat, func(m *Message) { m.SetContentFormat(TextPlain) }, nil,
			func(m Message) (uint32, bool) { v, ok := m.ContentFormat(); return uint32(v), ok }, 0},
		{"content-format/json", OptionContentFormat, func(m *Message) { m.SetContentFormat(ApplicationJSON) }, []byte{0x32},
			func(m Message) (uint32, bool) { v, ok := m.ContentFormat(); return uint32(v), ok }, 50},
		{"content-format/extended", OptionContentFormat, func(m *Message) { m.SetContentFormat(ContentFormat(0x0102)) }, []byte{0x01, 0x02},
			func(m Message) (uint32, bool) { v, ok := m.ContentFormat(); return uint32(v), ok }, 258},
		{"max-age/zero", OptionMaxAge, func(m *Message) { m.SetMaxAge(0) }, nil,
			func(m Message) (uint32, bool) { return m.MaxAge() }, 0},
		{"max-age/two-bytes", OptionMaxAge, func(m *Message) { m.SetMaxAge(300) }, []byte{0x01, 0x2c},
			func(m Message) (uint32, bool) { return m.MaxAge() }, 300},
		{"max-age/four-bytes", OptionMaxAge, func(m *Message) { m.SetMaxAge(0x01020304) }, []byte{0x01, 0x02, 0x03, 0x04},
			func(m Message) (uint32, bool) { return m.MaxAge() }, 0x01020304},
		{"size1", OptionSize1, func(m *Message) { m.SetSize1(65536) }, []byte{0x01, 0x00, 0x00},
			func(m Message) (uint32, bool) { return m.Size1() }, 65536},
		{"size2", OptionSize2, func(m *Message) { m.SetSize2(1024) }, []byte{0x04, 0x00},
			func(m Message) (uint32, bool) { return m.Size2() }, 1024},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var msg Message
			c.set(&msg)
			raw, ok := msg.OptionValue(c.number)
			if !ok {
				t.Fatalf("option %d is missing", c.number)
			}
			if !bytes.Equal(raw, c.raw) {
				t.Fatalf("raw option value = % x, want % x", raw, c.raw)
			}
			if value, ok := c.read(msg); !ok || value != c.value {
				t.Fatalf("read back = %d, %v, want %d", value, ok, c.value)
			}
		})
	}
}

func TestObserveOptions(t *testing.T) {
	var msg Message
	mustSet(t, msg.SetObserve(ObserveRegister))
	if raw, ok := msg.OptionValue(OptionObserve); !ok || len(raw) != 0 {
		t.Fatalf("ObserveRegister value = % x, present %v", raw, ok)
	}
	mustSet(t, msg.SetObserve(ObserveDeregister))
	if raw, ok := msg.OptionValue(OptionObserve); !ok || !bytes.Equal(raw, []byte{0x01}) {
		t.Fatalf("ObserveDeregister value = % x", raw)
	}
	mustSet(t, msg.SetObserve(Observe(0x010203)))
	if raw, ok := msg.OptionValue(OptionObserve); !ok || !bytes.Equal(raw, []byte{0x01, 0x02, 0x03}) {
		t.Fatalf("notification value = % x", raw)
	}
	if observe, ok := msg.Observe(); !ok || observe != 0x010203 {
		t.Fatalf("Observe() = %d, %v", observe, ok)
	}
	if err := msg.SetObserve(Observe(1 << 24)); err == nil {
		t.Fatal("SetObserve accepted a value wider than 3 bytes")
	} else if kind := kindOf(t, err); kind != "malformed" {
		t.Fatalf("SetObserve overflow kind = %s", kind)
	}

	raw, err := msg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if observe, ok := got.Observe(); !ok || observe != 0x010203 {
		t.Fatalf("Observe() after round trip = %d, %v", observe, ok)
	}
}

func TestIfNoneMatchOption(t *testing.T) {
	var msg Message
	msg.SetIfNoneMatch(true)
	raw, ok := msg.OptionValue(OptionIfNoneMatch)
	if !ok || len(raw) != 0 {
		t.Fatalf("If-None-Match value = % x, present %v", raw, ok)
	}
	if len(msg.Options) != 1 || msg.Options[0].Number != OptionIfNoneMatch {
		t.Fatalf("options = %v", msg.Options)
	}
	msg.SetIfNoneMatch(false)
	if msg.IfNoneMatch() || len(msg.Options) != 0 {
		t.Fatalf("If-None-Match was not removed: %v", msg.Options)
	}
}

func TestETagBounds(t *testing.T) {
	var msg Message
	if err := msg.AddETag(nil); err == nil {
		t.Fatal("AddETag accepted an empty tag")
	}
	if err := msg.AddETag(make([]byte, 9)); err == nil {
		t.Fatal("AddETag accepted a 9-byte tag")
	}
	mustSet(t, msg.AddETag(make([]byte, 8)))
	mustSet(t, msg.SetETag([]byte{0x01}))
	if got := msg.ETags(); !reflect.DeepEqual(got, [][]byte{{0x01}}) {
		t.Fatalf("ETags() = %v", got)
	}
	msg.SetETag(nil)
	if got := msg.ETags(); got != nil {
		t.Fatalf("ETags() after SetETag(nil) = %v", got)
	}
	if err := msg.AddIfMatch(make([]byte, 9)); err == nil {
		t.Fatal("AddIfMatch accepted a 9-byte value")
	}
	mustSet(t, msg.AddIfMatch(nil))
	if got := msg.IfMatches(); len(got) != 1 || len(got[0]) != 0 {
		t.Fatalf("IfMatches() = %v", got)
	}
}

func TestStringOptionBounds(t *testing.T) {
	msg := Message{}
	mustSet(t, msg.SetURIPath("kept"))
	long := string(bytes.Repeat([]byte{'x'}, maxStringOptionLen+1))
	if err := msg.SetURIPath(long); err == nil {
		t.Fatal("SetURIPath accepted a 256-byte segment")
	}
	// A rejected setter must not drop the options that were already there.
	if got := msg.URIPath(); !reflect.DeepEqual(got, []string{"kept"}) {
		t.Fatalf("URIPath() after a rejected setter = %q", got)
	}
	if err := msg.SetURIQuery(long); err == nil {
		t.Fatal("SetURIQuery accepted a 256-byte argument")
	}
	if err := msg.SetLocationPath(long); err == nil {
		t.Fatal("SetLocationPath accepted a 256-byte segment")
	}
	if err := msg.SetLocationQuery(long); err == nil {
		t.Fatal("SetLocationQuery accepted a 256-byte argument")
	}
	mustSet(t, msg.SetURIPath())
	if got := msg.URIPath(); got != nil {
		t.Fatalf("URIPath() after clearing = %v", got)
	}
}

func TestRawOptionAccessors(t *testing.T) {
	var msg Message
	mustSet(t, msg.AddOption(OptionURIPath, []byte("a")))
	mustSet(t, msg.AddOption(OptionURIPath, []byte("b")))
	mustSet(t, msg.AddOption(OptionETag, []byte{0x01}))
	if got := msg.URIPath(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("URIPath() = %q", got)
	}
	if _, exists := msg.OptionValue(OptionURIPath); !exists {
		t.Fatal("OptionValue did not find Uri-Path")
	}

	// Content-Format is not repeatable.
	if err := msg.AddOption(OptionContentFormat, []byte{0x32}); err != nil {
		t.Fatal(err)
	}
	if err := msg.AddOption(OptionContentFormat, []byte{0x32}); err == nil {
		t.Fatal("AddOption accepted a repeated Content-Format")
	}

	// SetOption replaces every instance.
	mustSet(t, msg.SetOption(OptionURIPath, []byte("z")))
	if got := msg.URIPath(); !reflect.DeepEqual(got, []string{"z"}) {
		t.Fatalf("URIPath() after SetOption = %q", got)
	}

	// Known option values are length checked, unknown ones are not.
	if err := msg.SetOption(OptionContentFormat, []byte{1, 2, 3}); err == nil {
		t.Fatal("SetOption accepted a 3-byte Content-Format")
	}
	if err := msg.SetOption(OptionMaxAge, make([]byte, 5)); err == nil {
		t.Fatal("SetOption accepted a 5-byte Max-Age")
	}
	if err := msg.SetOption(300, bytes.Repeat([]byte{'x'}, 300)); err != nil {
		t.Fatalf("SetOption rejected an unknown option: %v", err)
	}
	if raw, ok := msg.OptionValue(300); !ok || len(raw) != 300 {
		t.Fatalf("unknown option = %d bytes, present %v", len(raw), ok)
	}
}

func TestOptionAccessorsKeepOptionsSorted(t *testing.T) {
	// A caller may hand-edit Options into any order; the typed accessors sort
	// the list before inserting.
	msg := Message{Options: []Option{
		{Number: OptionBlock2, Value: []byte{0x00, 0x00, 0x00}},
		{Number: OptionObserve, Value: []byte{0x01}},
	}}
	msg.SetContentFormat(ApplicationJSON)
	want := []uint16{OptionObserve, OptionContentFormat, OptionBlock2}
	if len(msg.Options) != len(want) {
		t.Fatalf("options = %v", msg.Options)
	}
	for i, number := range want {
		if msg.Options[i].Number != number {
			t.Fatalf("options = %v, want numbers %v", msg.Options, want)
		}
	}
}
