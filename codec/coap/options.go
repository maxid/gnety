package coap

import (
	"sort"
	"strconv"
)

// Option numbers this package models, from the CoAP option number registry
// (RFC 7252 section 12.2, extended by RFC 7641 and RFC 7959). Numbers not
// listed here are still carried unchanged as raw options.
const (
	// OptionIfMatch is If-Match (1): entity tags a conditional request must
	// match.
	OptionIfMatch uint16 = 1
	// OptionETag is ETag (4): an entity tag of the representation.
	OptionETag uint16 = 4
	// OptionIfNoneMatch is If-None-Match (5): a conditional create that
	// succeeds only when no representation exists.
	OptionIfNoneMatch uint16 = 5
	// OptionObserve is Observe (6): registration and deregistration requests
	// and the sequence number of a notification (RFC 7641).
	OptionObserve uint16 = 6
	// OptionLocationPath is Location-Path (8): one segment of the location of
	// a resource a response created.
	OptionLocationPath uint16 = 8
	// OptionURIPath is Uri-Path (11): one segment of the request path.
	OptionURIPath uint16 = 11
	// OptionContentFormat is Content-Format (12): the media type of the
	// payload.
	OptionContentFormat uint16 = 12
	// OptionMaxAge is Max-Age (14): the freshness lifetime of a response in
	// seconds. An absent option means 60.
	OptionMaxAge uint16 = 14
	// OptionURIQuery is Uri-Query (15): one query argument such as "a=1".
	OptionURIQuery uint16 = 15
	// OptionAccept is Accept (17): the media type the client prefers for the
	// response.
	OptionAccept uint16 = 17
	// OptionLocationQuery is Location-Query (20): one query argument of the
	// location of a resource a response created.
	OptionLocationQuery uint16 = 20
	// OptionBlock2 is Block2 (23): one block of a response body (RFC 7959).
	OptionBlock2 uint16 = 23
	// OptionBlock1 is Block1 (27): one block of a request body (RFC 7959).
	OptionBlock1 uint16 = 27
	// OptionSize2 is Size2 (28): the total size of a block-wise response body
	// (RFC 7959).
	OptionSize2 uint16 = 28
	// OptionSize1 is Size1 (60): the total size of a block-wise request body
	// (RFC 7959).
	OptionSize1 uint16 = 60
)

// maxStringOptionLen is the largest value a string option (Uri-Path, Uri-Query,
// Location-Path, Location-Query) may carry, per RFC 7252 section 5.10.
const maxStringOptionLen = 255

// optionDef records how the specification constrains one known option.
type optionDef struct {
	// name is the registry name, used in error messages.
	name string
	// repeatable reports whether the option may appear more than once.
	repeatable bool
	// maxLen is the largest option value in bytes.
	maxLen int
}

// optionDefs holds the definition of every option number this package models.
// Numbers missing from the map are unknown to the codec and are carried as
// raw bytes without a repeat or length check.
var optionDefs = map[uint16]optionDef{
	OptionIfMatch:       {"If-Match", true, 8},
	OptionETag:          {"ETag", true, 8},
	OptionIfNoneMatch:   {"If-None-Match", false, 0},
	OptionObserve:       {"Observe", false, 3},
	OptionLocationPath:  {"Location-Path", true, maxStringOptionLen},
	OptionURIPath:       {"Uri-Path", true, maxStringOptionLen},
	OptionContentFormat: {"Content-Format", false, 2},
	OptionMaxAge:        {"Max-Age", false, 4},
	OptionURIQuery:      {"Uri-Query", true, maxStringOptionLen},
	OptionAccept:        {"Accept", false, 2},
	OptionLocationQuery: {"Location-Query", true, maxStringOptionLen},
	OptionBlock2:        {"Block2", false, 3},
	OptionBlock1:        {"Block1", false, 3},
	OptionSize2:         {"Size2", false, 4},
	OptionSize1:         {"Size1", false, 4},
}

// ContentFormat is a CoAP Content-Format value: the numeric media type
// identifier from the CoAP Content-Formats registry that travels on the wire.
type ContentFormat uint16

// Content-Format values a constrained server commonly needs. The registry holds
// many more; any uint16 can be carried, these are only the named ones.
const (
	// TextPlain is text/plain; charset=utf-8 (0).
	TextPlain ContentFormat = 0
	// ApplicationLinkFormat is application/link-format (40), the payload of
	// resource discovery.
	ApplicationLinkFormat ContentFormat = 40
	// ApplicationXML is application/xml (41).
	ApplicationXML ContentFormat = 41
	// ApplicationOctetStream is application/octet-stream (42).
	ApplicationOctetStream ContentFormat = 42
	// ApplicationJSON is application/json (50).
	ApplicationJSON ContentFormat = 50
	// ApplicationCBOR is application/cbor (60).
	ApplicationCBOR ContentFormat = 60
)

// String returns the media type of a named Content-Format, or the numeric value
// for one this package does not name.
func (f ContentFormat) String() string {
	switch f {
	case TextPlain:
		return "text/plain"
	case ApplicationLinkFormat:
		return "application/link-format"
	case ApplicationXML:
		return "application/xml"
	case ApplicationOctetStream:
		return "application/octet-stream"
	case ApplicationJSON:
		return "application/json"
	case ApplicationCBOR:
		return "application/cbor"
	default:
		return "content-format " + strconv.FormatUint(uint64(f), 10)
	}
}

// Observe is the value of an Observe option (RFC 7641): ObserveRegister and
// ObserveDeregister are the two requests, larger values are the sequence
// numbers a server stamps on notifications.
type Observe uint32

// Observe requests with a fixed meaning.
const (
	// ObserveRegister asks the server to add the client to the list of
	// observers of a resource (value 0).
	ObserveRegister Observe = 0
	// ObserveDeregister asks the server to remove the client from the list of
	// observers of a resource (value 1).
	ObserveDeregister Observe = 1
)

// observeMax is the largest value the 3-byte Observe option can carry.
const observeMax = 1<<24 - 1

// Block is the value of a Block1 or Block2 option (RFC 7959).
type Block struct {
	// Num is the block number. The option value carries 20 bits of it, so the
	// largest representable block number is 1048575.
	Num uint32
	// More reports that further blocks follow (the M bit).
	More bool
	// SizeExp is the size exponent SZX, 0 to 6. The block size is
	// 1<<(SizeExp+4) bytes.
	SizeExp uint8
}

// Size returns the block size in bytes, 1<<(SizeExp+4), or 0 when SizeExp is
// outside 0 to 6.
func (b Block) Size() int {
	if b.SizeExp > 6 {
		return 0
	}
	return 1 << (b.SizeExp + 4)
}

// Encode returns the option value: a 3-byte big-endian integer holding the
// block number in the high 20 bits, the M bit and the 4-bit size exponent in
// the low byte.
func (b Block) Encode() ([]byte, error) {
	if b.SizeExp > 6 {
		return nil, malformed("block size exponent %d is reserved", b.SizeExp)
	}
	if b.Num > 0x0fffff {
		return nil, malformed("block number %d exceeds the 20-bit maximum %d", b.Num, 0x0fffff)
	}
	raw := b.Num<<4 | uint32(b.SizeExp)
	if b.More {
		raw |= 0x08
	}
	return []byte{byte(raw >> 16), byte(raw >> 8), byte(raw)}, nil
}

// DecodeBlock decodes a Block1 or Block2 option value.
//
// The value is a uint of 0 to 3 bytes, so both the compact forms of RFC 7959
// and the fixed 3-byte form Encode produces are accepted. A size exponent of 7
// is reserved and rejected.
func DecodeBlock(value []byte) (Block, error) {
	if len(value) > 3 {
		return Block{}, malformed("block option value of %d bytes exceeds 3", len(value))
	}
	var raw uint32
	for _, c := range value {
		raw = raw<<8 | uint32(c)
	}
	b := Block{Num: raw >> 4, More: raw&0x08 != 0, SizeExp: uint8(raw & 0x07)}
	if b.SizeExp > 6 {
		return Block{}, malformed("block size exponent %d is reserved", b.SizeExp)
	}
	return b, nil
}

// OptionValue returns the value of the first option with the given number. The
// returned slice aliases the message; copy it before retaining it, and prefer
// the typed accessors when the number is one this package models.
func (m Message) OptionValue(number uint16) ([]byte, bool) {
	for _, opt := range m.Options {
		if opt.Number == number {
			return opt.Value, true
		}
	}
	return nil, false
}

// SetOption replaces every option with the given number by one raw option. The
// value is copied. A value longer than the definition of a known option allows
// is rejected; unknown numbers are carried as-is.
func (m *Message) SetOption(number uint16, value []byte) error {
	if err := checkOptionValue(number, value); err != nil {
		return err
	}
	m.setOptionBytes(number, value)
	return nil
}

// AddOption appends one raw option value while keeping Options sorted by
// number. The value is copied. A value longer than the definition of a known
// option allows, or a second instance of a known non-repeatable option, is
// rejected; unknown numbers are carried as-is.
func (m *Message) AddOption(number uint16, value []byte) error {
	if err := checkOptionValue(number, value); err != nil {
		return err
	}
	if def, known := optionDefs[number]; known && !def.repeatable {
		if _, exists := m.OptionValue(number); exists {
			return malformed("%s is not repeatable but appears more than once", def.name)
		}
	}
	m.addOptionBytes(number, value)
	return nil
}

// URIPath returns the Uri-Path option values in order: one string per path
// segment, without leading or trailing slashes. The result is nil when the
// message carries no Uri-Path option.
func (m Message) URIPath() []string { return m.stringOptions(OptionURIPath) }

// SetURIPath replaces the Uri-Path options with the given segments, in order.
// Each segment is one option value of at most 255 bytes; a segment may be
// empty.
func (m *Message) SetURIPath(segments ...string) error {
	return m.setStringOptions(OptionURIPath, segments)
}

// URIQuery returns the Uri-Query arguments in order, as sent: one string per
// option, for example "a=1". The result is nil when the message carries no
// Uri-Query option.
func (m Message) URIQuery() []string { return m.stringOptions(OptionURIQuery) }

// SetURIQuery replaces the Uri-Query options with the given arguments, in
// order. Each argument is one option value of at most 255 bytes; an argument
// may be empty.
func (m *Message) SetURIQuery(query ...string) error {
	return m.setStringOptions(OptionURIQuery, query)
}

// LocationPath returns the Location-Path option values in order: one string per
// segment of the location of a created resource.
func (m Message) LocationPath() []string { return m.stringOptions(OptionLocationPath) }

// SetLocationPath replaces the Location-Path options with the given segments,
// in order. Each segment is one option value of at most 255 bytes.
func (m *Message) SetLocationPath(segments ...string) error {
	return m.setStringOptions(OptionLocationPath, segments)
}

// LocationQuery returns the Location-Query option values in order: one string
// per query argument of the location of a created resource.
func (m Message) LocationQuery() []string { return m.stringOptions(OptionLocationQuery) }

// SetLocationQuery replaces the Location-Query options with the given
// arguments, in order. Each argument is one option value of at most 255 bytes.
func (m *Message) SetLocationQuery(query ...string) error {
	return m.setStringOptions(OptionLocationQuery, query)
}

// ContentFormat returns the Content-Format option. The boolean is false when
// the option is absent or its value is not a 0 to 2 byte unsigned integer.
func (m Message) ContentFormat() (ContentFormat, bool) {
	value, ok := m.uintOption(OptionContentFormat, 2)
	return ContentFormat(value), ok
}

// SetContentFormat sets the Content-Format option, replacing any previous one.
// TextPlain (0) is encoded as an empty option value, as the specification
// requires.
func (m *Message) SetContentFormat(format ContentFormat) {
	m.setUintOption(OptionContentFormat, uint32(format))
}

// Accept returns the Accept option. The boolean is false when the option is
// absent or its value is not a 0 to 2 byte unsigned integer.
func (m Message) Accept() (ContentFormat, bool) {
	value, ok := m.uintOption(OptionAccept, 2)
	return ContentFormat(value), ok
}

// SetAccept sets the Accept option, replacing any previous one. TextPlain (0)
// is encoded as an empty option value.
func (m *Message) SetAccept(format ContentFormat) {
	m.setUintOption(OptionAccept, uint32(format))
}

// MaxAge returns the Max-Age option in seconds. The boolean is false when the
// option is absent; an absent Max-Age means the default of 60 seconds.
func (m Message) MaxAge() (uint32, bool) { return m.uintOption(OptionMaxAge, 4) }

// SetMaxAge sets the Max-Age option in seconds, replacing any previous one.
func (m *Message) SetMaxAge(seconds uint32) { m.setUintOption(OptionMaxAge, seconds) }

// Size1 returns the Size1 option: the total size of a block-wise request body.
// The boolean is false when the option is absent or its value is not a 0 to 4
// byte unsigned integer.
func (m Message) Size1() (uint32, bool) { return m.uintOption(OptionSize1, 4) }

// SetSize1 sets the Size1 option, replacing any previous one.
func (m *Message) SetSize1(size uint32) { m.setUintOption(OptionSize1, size) }

// Size2 returns the Size2 option: the total size of a block-wise response body.
// The boolean is false when the option is absent or its value is not a 0 to 4
// byte unsigned integer.
func (m Message) Size2() (uint32, bool) { return m.uintOption(OptionSize2, 4) }

// SetSize2 sets the Size2 option, replacing any previous one.
func (m *Message) SetSize2(size uint32) { m.setUintOption(OptionSize2, size) }

// ETags returns the ETag option values in order. The returned slices alias the
// message.
func (m Message) ETags() [][]byte { return m.optionValues(OptionETag) }

// SetETag replaces the ETag options with a single entity tag of 1 to 8 bytes.
// A nil tag removes the option.
func (m *Message) SetETag(tag []byte) error {
	if tag == nil {
		m.deleteOption(OptionETag)
		return nil
	}
	if len(tag) < 1 || len(tag) > 8 {
		return malformed("ETag of %d bytes is outside the 1 to 8 byte range", len(tag))
	}
	m.setOptionBytes(OptionETag, tag)
	return nil
}

// AddETag appends one ETag option value of 1 to 8 bytes.
func (m *Message) AddETag(tag []byte) error {
	if len(tag) < 1 || len(tag) > 8 {
		return malformed("ETag of %d bytes is outside the 1 to 8 byte range", len(tag))
	}
	m.addOptionBytes(OptionETag, tag)
	return nil
}

// IfMatches returns the If-Match option values in order: the entity tags a
// request requires the current representation to match. An empty list means
// the request is not conditional on an entity tag.
func (m Message) IfMatches() [][]byte { return m.optionValues(OptionIfMatch) }

// AddIfMatch appends one If-Match option value of 0 to 8 bytes. An empty value
// matches any existing representation.
func (m *Message) AddIfMatch(tag []byte) error {
	if len(tag) > 8 {
		return malformed("If-Match value of %d bytes exceeds 8", len(tag))
	}
	m.addOptionBytes(OptionIfMatch, tag)
	return nil
}

// IfNoneMatch reports whether the If-None-Match option is present. The option
// carries no value; its presence makes a request conditional on the resource
// not existing yet.
func (m Message) IfNoneMatch() bool {
	_, ok := m.OptionValue(OptionIfNoneMatch)
	return ok
}

// SetIfNoneMatch adds the If-None-Match option when v is true and removes it
// when v is false.
func (m *Message) SetIfNoneMatch(v bool) {
	if v {
		m.setOptionBytes(OptionIfNoneMatch, nil)
		return
	}
	m.deleteOption(OptionIfNoneMatch)
}

// Observe returns the Observe option. The boolean is false when the option is
// absent or its value is not a 0 to 3 byte unsigned integer.
func (m Message) Observe() (Observe, bool) {
	value, ok := m.uintOption(OptionObserve, 3)
	return Observe(value), ok
}

// SetObserve sets the Observe option, replacing any previous one: either
// ObserveRegister, ObserveDeregister, or a notification sequence number of at
// most 2^24-1.
func (m *Message) SetObserve(value Observe) error {
	if uint32(value) > observeMax {
		return malformed("observe value %d exceeds the 3-byte maximum %d", uint32(value), observeMax)
	}
	m.setUintOption(OptionObserve, uint32(value))
	return nil
}

// Block1 returns the Block1 option. The boolean is false when the option is
// absent or its value does not decode as a block option; use OptionValue and
// DecodeBlock to tell those two cases apart.
func (m Message) Block1() (Block, bool) { return m.blockOption(OptionBlock1) }

// SetBlock1 sets the Block1 option, replacing any previous one.
func (m *Message) SetBlock1(block Block) error {
	value, err := block.Encode()
	if err != nil {
		return err
	}
	m.setOptionBytes(OptionBlock1, value)
	return nil
}

// Block2 returns the Block2 option. The boolean is false when the option is
// absent or its value does not decode as a block option; use OptionValue and
// DecodeBlock to tell those two cases apart.
func (m Message) Block2() (Block, bool) { return m.blockOption(OptionBlock2) }

// SetBlock2 sets the Block2 option, replacing any previous one.
func (m *Message) SetBlock2(block Block) error {
	value, err := block.Encode()
	if err != nil {
		return err
	}
	m.setOptionBytes(OptionBlock2, value)
	return nil
}

// checkOptionValue rejects a value longer than the definition of a known option
// allows.
func checkOptionValue(number uint16, value []byte) error {
	def, known := optionDefs[number]
	if !known || len(value) <= def.maxLen {
		return nil
	}
	return malformed("%s value of %d bytes exceeds %d", def.name, len(value), def.maxLen)
}

// stringOptions returns the values of a repeatable string option.
func (m Message) stringOptions(number uint16) []string {
	var out []string
	for _, opt := range m.Options {
		if opt.Number == number {
			out = append(out, string(opt.Value))
		}
	}
	return out
}

// setStringOptions replaces every value of a string option, in order.
func (m *Message) setStringOptions(number uint16, values []string) error {
	def := optionDefs[number]
	for _, value := range values {
		if len(value) > def.maxLen {
			return malformed("%s value of %d bytes exceeds %d", def.name, len(value), def.maxLen)
		}
	}
	m.deleteOption(number)
	for _, value := range values {
		m.insertOption(Option{Number: number, Value: []byte(value)})
	}
	return nil
}

// optionValues returns every value of an option in order. The values alias the
// message.
func (m Message) optionValues(number uint16) [][]byte {
	var out [][]byte
	for _, opt := range m.Options {
		if opt.Number == number {
			out = append(out, opt.Value)
		}
	}
	return out
}

// uintOption decodes an unsigned integer option of at most maxLen bytes.
func (m Message) uintOption(number uint16, maxLen int) (uint32, bool) {
	value, ok := m.OptionValue(number)
	if !ok || len(value) > maxLen {
		return 0, false
	}
	var out uint32
	for _, b := range value {
		out = out<<8 | uint32(b)
	}
	return out, true
}

// setUintOption stores an unsigned integer option in its shortest form.
func (m *Message) setUintOption(number uint16, value uint32) {
	m.setOptionBytes(number, appendUintValue(value))
}

// blockOption decodes a Block1 or Block2 option.
func (m Message) blockOption(number uint16) (Block, bool) {
	value, ok := m.OptionValue(number)
	if !ok {
		return Block{}, false
	}
	block, err := DecodeBlock(value)
	if err != nil {
		return Block{}, false
	}
	return block, true
}

// appendUintValue returns the shortest big-endian unsigned integer encoding of
// value; zero is an empty slice.
func appendUintValue(value uint32) []byte {
	n := 0
	for v := value; v != 0; v >>= 8 {
		n++
	}
	out := make([]byte, n)
	for i := n - 1; i >= 0; i-- {
		out[i] = byte(value)
		value >>= 8
	}
	return out
}

// setOptionBytes replaces every option with the given number by a copy of value.
func (m *Message) setOptionBytes(number uint16, value []byte) {
	m.deleteOption(number)
	m.insertOption(Option{Number: number, Value: append([]byte(nil), value...)})
}

// addOptionBytes appends a copy of value, keeping Options sorted by number.
func (m *Message) addOptionBytes(number uint16, value []byte) {
	m.insertOption(Option{Number: number, Value: append([]byte(nil), value...)})
}

// insertOption inserts one option after every option with the same or a lower
// number, so repeated values keep their insertion order. A list that a caller
// edited into a different order is sorted first.
func (m *Message) insertOption(option Option) {
	if !sort.SliceIsSorted(m.Options, func(i, j int) bool {
		return m.Options[i].Number < m.Options[j].Number
	}) {
		sort.SliceStable(m.Options, func(i, j int) bool {
			return m.Options[i].Number < m.Options[j].Number
		})
	}
	at := sort.Search(len(m.Options), func(i int) bool {
		return m.Options[i].Number > option.Number
	})
	m.Options = append(m.Options, Option{})
	copy(m.Options[at+1:], m.Options[at:])
	m.Options[at] = option
}

// deleteOption removes every option with the given number.
func (m *Message) deleteOption(number uint16) {
	kept := m.Options[:0]
	for _, opt := range m.Options {
		if opt.Number != number {
			kept = append(kept, opt)
		}
	}
	m.Options = kept
}
