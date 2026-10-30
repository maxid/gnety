package coap

import (
	"bytes"
	"reflect"
	"testing"
)

func TestBlockEncode(t *testing.T) {
	cases := []struct {
		name  string
		block Block
		raw   []byte
	}{
		{"zero", Block{}, []byte{0x00, 0x00, 0x00}},
		{"num2-more-szx2", Block{Num: 2, More: true, SizeExp: 2}, []byte{0x00, 0x00, 0x2a}},
		{"num2-szx2", Block{Num: 2, SizeExp: 2}, []byte{0x00, 0x00, 0x22}},
		{"largest-12-bit", Block{Num: 4095, More: true, SizeExp: 6}, []byte{0x00, 0xff, 0xfe}},
		{"largest-20-bit", Block{Num: 0x0fffff, SizeExp: 0}, []byte{0xff, 0xff, 0xf0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := c.block.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(raw, c.raw) {
				t.Fatalf("Encode() = % x, want % x", raw, c.raw)
			}
			decoded, err := DecodeBlock(raw)
			if err != nil {
				t.Fatal(err)
			}
			if decoded != c.block {
				t.Fatalf("DecodeBlock(% x) = %+v, want %+v", raw, decoded, c.block)
			}
		})
	}
}

func TestBlockSize(t *testing.T) {
	want := []int{16, 32, 64, 128, 256, 512, 1024}
	for szx, size := range want {
		if got := (Block{SizeExp: uint8(szx)}).Size(); got != size {
			t.Fatalf("Size() with SZX %d = %d, want %d", szx, got, size)
		}
	}
	if got := (Block{SizeExp: 7}).Size(); got != 0 {
		t.Fatalf("Size() with reserved SZX = %d, want 0", got)
	}
}

func TestBlockBounds(t *testing.T) {
	if _, err := (Block{SizeExp: 7}).Encode(); err == nil {
		t.Fatal("Encode accepted the reserved size exponent 7")
	} else if kindOf(t, err) != "malformed" {
		t.Fatalf("SZX 7 kind = %s", kindOf(t, err))
	}
	if _, err := (Block{Num: 0x100000}).Encode(); err == nil {
		t.Fatal("Encode accepted a block number wider than 20 bits")
	}
	var msg Message
	if err := msg.SetBlock1(Block{SizeExp: 7}); err == nil {
		t.Fatal("SetBlock1 accepted the reserved size exponent 7")
	}
	if err := msg.SetBlock2(Block{Num: 0x100000}); err == nil {
		t.Fatal("SetBlock2 accepted a block number wider than 20 bits")
	}
}

func TestDecodeBlockCompactForms(t *testing.T) {
	cases := []struct {
		name  string
		raw   []byte
		block Block
	}{
		{"empty", nil, Block{}},
		{"one-byte", []byte{0x2a}, Block{Num: 2, More: true, SizeExp: 2}},
		{"one-byte-szx-only", []byte{0x02}, Block{SizeExp: 2}},
		{"two-bytes", []byte{0x01, 0x02}, Block{Num: 16, SizeExp: 2}},
		{"three-bytes", []byte{0x00, 0x00, 0x2a}, Block{Num: 2, More: true, SizeExp: 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			block, err := DecodeBlock(c.raw)
			if err != nil {
				t.Fatal(err)
			}
			if block != c.block {
				t.Fatalf("DecodeBlock(% x) = %+v, want %+v", c.raw, block, c.block)
			}
		})
	}
	if _, err := DecodeBlock([]byte{0x00, 0x00, 0x00, 0x00}); err == nil {
		t.Fatal("DecodeBlock accepted a 4-byte value")
	}
	if _, err := DecodeBlock([]byte{0x07}); err == nil {
		t.Fatal("DecodeBlock accepted the reserved size exponent 7")
	}
}

func TestBlockMessageHelpers(t *testing.T) {
	msg := Message{Version: Version, Type: Confirmable, Code: CodeContent, MessageID: 7}
	block1 := Block{Num: 0x12345, More: true, SizeExp: 4}
	block2 := Block{Num: 9, More: false, SizeExp: 6}
	if err := msg.SetBlock1(block1); err != nil {
		t.Fatal(err)
	}
	if err := msg.SetBlock2(block2); err != nil {
		t.Fatal(err)
	}
	raw := mustMarshal(t, msg)
	got := mustParse(t, raw)
	if block, ok := got.Block1(); !ok || block != block1 {
		t.Fatalf("Block1() = %+v, %v, want %+v", block, ok, block1)
	}
	if block, ok := got.Block2(); !ok || block != block2 {
		t.Fatalf("Block2() = %+v, %v, want %+v", block, ok, block2)
	}

	// A malformed block option is reported as absent by the typed getter while
	// the raw value stays visible.
	var bad Message
	bad.Options = []Option{{Number: OptionBlock2, Value: []byte{0x00, 0x00, 0x00, 0x00}}}
	if _, ok := bad.Block2(); ok {
		t.Fatal("Block2() accepted a 4-byte option value")
	}
	if value, ok := bad.OptionValue(OptionBlock2); !ok || len(value) != 4 {
		t.Fatalf("OptionValue(Block2) = % x, %v", value, ok)
	}

	// SetBlock2 replaces any previous Block2 option.
	if err := msg.SetBlock2(Block{Num: 1, SizeExp: 0}); err != nil {
		t.Fatal(err)
	}
	if blocks := msg.optionValues(OptionBlock2); len(blocks) != 1 {
		t.Fatalf("Block2 options = %v", blocks)
	}
}

func TestBlockOptionGolden(t *testing.T) {
	// A Block2 option with number 2, M set and SZX 2 is the RFC 7959 example
	// byte 0x2a; the fixed 3-byte form pads it with leading zeroes.
	msg := Message{Version: Version, Type: Acknowledgement, Code: CodeContent, MessageID: 0x1234}
	if err := msg.SetBlock2(Block{Num: 2, More: true, SizeExp: 2}); err != nil {
		t.Fatal(err)
	}
	raw := mustMarshal(t, msg)
	want := []byte{0x60, 0x45, 0x12, 0x34, 0xd3, 0x0a, 0x00, 0x00, 0x2a}
	if !bytes.Equal(raw, want) {
		t.Fatalf("Marshal = % x, want % x", raw, want)
	}
	got := mustParse(t, raw)
	if !reflect.DeepEqual(got.Options, []Option{{Number: OptionBlock2, Value: []byte{0x00, 0x00, 0x2a}}}) {
		t.Fatalf("options = %v", got.Options)
	}
}
