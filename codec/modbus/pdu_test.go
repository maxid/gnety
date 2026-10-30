package modbus

import (
	"bytes"
	"reflect"
	"testing"
)

// readCoilsBits37 is the bit sequence of the classic 0x01 example response
// CD 6B B2 0E 1B read least significant bit first.
func readCoilsBits37() []bool {
	return []bool{
		true, false, true, true, false, false, true, true,
		true, true, false, true, false, true, true, false,
		false, true, false, false, true, true, false, true,
		false, true, true, true, false, false, false, false,
		true, true, false, true, true,
	}
}

// readDiscreteBits22 is the bit sequence of the classic 0x02 example response
// AC DB 35 read least significant bit first.
func readDiscreteBits22() []bool {
	return []bool{
		false, false, true, true, false, true, false, true,
		true, true, false, true, true, false, true, true,
		true, false, true, false, true, true,
	}
}

// writeMultipleCoilsValues10 is the coil sequence of the classic 0x0F example
// request CD 01.
func writeMultipleCoilsValues10() []bool {
	return []bool{true, false, true, true, false, false, true, true, true, false}
}

// requestGoldenCases returns hand-computed request PDUs, one per function code
// the package models, plus an exception response.
func requestGoldenCases() []struct {
	name string
	msg  Message
	pdu  []byte
} {
	return []struct {
		name string
		msg  Message
		pdu  []byte
	}{
		{
			"read coils",
			&ReadCoilsRequest{Address: 0x0013, Quantity: 37},
			[]byte{0x01, 0x00, 0x13, 0x00, 0x25},
		},
		{
			"read discrete inputs",
			&ReadDiscreteInputsRequest{Address: 0x00c4, Quantity: 22},
			[]byte{0x02, 0x00, 0xc4, 0x00, 0x16},
		},
		{
			"read holding registers",
			&ReadHoldingRegistersRequest{Address: 0x006b, Quantity: 3},
			[]byte{0x03, 0x00, 0x6b, 0x00, 0x03},
		},
		{
			"read input registers",
			&ReadInputRegistersRequest{Address: 0x0008, Quantity: 1},
			[]byte{0x04, 0x00, 0x08, 0x00, 0x01},
		},
		{
			"write single coil on",
			&WriteSingleCoil{Address: 0x00ac, Value: true},
			[]byte{0x05, 0x00, 0xac, 0xff, 0x00},
		},
		{
			"write single coil off",
			&WriteSingleCoil{Address: 0x00ac, Value: false},
			[]byte{0x05, 0x00, 0xac, 0x00, 0x00},
		},
		{
			"write single register",
			&WriteSingleRegister{Address: 0x0001, Value: 0x0003},
			[]byte{0x06, 0x00, 0x01, 0x00, 0x03},
		},
		{
			"write multiple coils",
			&WriteMultipleCoilsRequest{Address: 0x0013, Values: writeMultipleCoilsValues10()},
			[]byte{0x0f, 0x00, 0x13, 0x00, 0x0a, 0x02, 0xcd, 0x01},
		},
		{
			"write multiple registers",
			&WriteMultipleRegistersRequest{Address: 0x0001, Values: []uint16{0x000a, 0x0102}},
			[]byte{0x10, 0x00, 0x01, 0x00, 0x02, 0x04, 0x00, 0x0a, 0x01, 0x02},
		},
		{
			"exception response",
			&Exception{FunctionCode: FuncReadHoldingRegisters, Code: ExceptionIllegalDataAddress},
			[]byte{0x83, 0x02},
		},
	}
}

// responseGoldenCases returns hand-computed response PDUs together with the
// request each answers and the decoded message it must produce.
func responseGoldenCases() []struct {
	name    string
	request Message
	want    Message
	pdu     []byte
} {
	return []struct {
		name    string
		request Message
		want    Message
		pdu     []byte
	}{
		{
			"read coils",
			&ReadCoilsRequest{Address: 0x0013, Quantity: 37},
			&ReadCoilsResponse{Bits: readCoilsBits37()},
			[]byte{0x01, 0x05, 0xcd, 0x6b, 0xb2, 0x0e, 0x1b},
		},
		{
			"read discrete inputs",
			&ReadDiscreteInputsRequest{Address: 0x00c4, Quantity: 22},
			&ReadDiscreteInputsResponse{Bits: readDiscreteBits22()},
			[]byte{0x02, 0x03, 0xac, 0xdb, 0x35},
		},
		{
			"read holding registers",
			&ReadHoldingRegistersRequest{Address: 0x006b, Quantity: 3},
			&ReadHoldingRegistersResponse{Registers: []uint16{0x022b, 0x0000, 0x0064}},
			[]byte{0x03, 0x06, 0x02, 0x2b, 0x00, 0x00, 0x00, 0x64},
		},
		{
			"read input registers",
			&ReadInputRegistersRequest{Address: 0x0008, Quantity: 1},
			&ReadInputRegistersResponse{Registers: []uint16{0x000a}},
			[]byte{0x04, 0x02, 0x00, 0x0a},
		},
		{
			"write single coil echo",
			&WriteSingleCoil{Address: 0x00ac, Value: true},
			&WriteSingleCoil{Address: 0x00ac, Value: true},
			[]byte{0x05, 0x00, 0xac, 0xff, 0x00},
		},
		{
			"write single register echo",
			&WriteSingleRegister{Address: 0x0001, Value: 0x0003},
			&WriteSingleRegister{Address: 0x0001, Value: 0x0003},
			[]byte{0x06, 0x00, 0x01, 0x00, 0x03},
		},
		{
			"write multiple coils response",
			&WriteMultipleCoilsRequest{Address: 0x0013, Values: writeMultipleCoilsValues10()},
			&WriteMultipleCoilsResponse{Address: 0x0013, Quantity: 10},
			[]byte{0x0f, 0x00, 0x13, 0x00, 0x0a},
		},
		{
			"write multiple registers response",
			&WriteMultipleRegistersRequest{Address: 0x0001, Values: []uint16{0x000a, 0x0102}},
			&WriteMultipleRegistersResponse{Address: 0x0001, Quantity: 2},
			[]byte{0x10, 0x00, 0x01, 0x00, 0x02},
		},
	}
}

func TestGoldenRequestPDUs(t *testing.T) {
	for _, tc := range requestGoldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.msg.PDU()
			if err != nil {
				t.Fatalf("PDU: %v", err)
			}
			if !bytes.Equal(got, tc.pdu) {
				t.Fatalf("PDU = % x, want % x", got, tc.pdu)
			}

			decoded, err := DecodePDU(tc.pdu)
			if err != nil {
				t.Fatalf("DecodePDU(% x): %v", tc.pdu, err)
			}
			if !reflect.DeepEqual(decoded, tc.msg) {
				t.Fatalf("DecodePDU = %#v, want %#v", decoded, tc.msg)
			}
			again, err := decoded.PDU()
			if err != nil {
				t.Fatalf("re-encode: %v", err)
			}
			if !bytes.Equal(again, tc.pdu) {
				t.Fatalf("re-encoded = % x, want % x", again, tc.pdu)
			}
		})
	}
}

func TestGoldenResponsePDUs(t *testing.T) {
	for _, tc := range responseGoldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.want.PDU()
			if err != nil {
				t.Fatalf("PDU: %v", err)
			}
			if !bytes.Equal(got, tc.pdu) {
				t.Fatalf("PDU = % x, want % x", got, tc.pdu)
			}

			decoded, err := DecodeResponse(tc.request, tc.pdu)
			if err != nil {
				t.Fatalf("DecodeResponse(% x): %v", tc.pdu, err)
			}
			if !reflect.DeepEqual(decoded, tc.want) {
				t.Fatalf("DecodeResponse = %#v, want %#v", decoded, tc.want)
			}
			again, err := decoded.PDU()
			if err != nil {
				t.Fatalf("re-encode: %v", err)
			}
			if !bytes.Equal(again, tc.pdu) {
				t.Fatalf("re-encoded = % x, want % x", again, tc.pdu)
			}
		})
	}
}

func TestBitPackingOrder(t *testing.T) {
	// Three coils packed into one byte: bit 0 is the first coil, and the five
	// padding bits are zero.
	pdu, err := (&WriteMultipleCoilsRequest{Address: 0, Values: []bool{true, false, true}}).PDU()
	if err != nil {
		t.Fatalf("PDU: %v", err)
	}
	want := []byte{0x0f, 0x00, 0x00, 0x00, 0x03, 0x01, 0x05}
	if !bytes.Equal(pdu, want) {
		t.Fatalf("PDU = % x, want % x", pdu, want)
	}

	// A response carrying 0xff for a three-bit read keeps only three bits.
	decoded, err := DecodeResponse(&ReadCoilsRequest{Quantity: 3}, []byte{0x01, 0x01, 0xff})
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	response, ok := decoded.(*ReadCoilsResponse)
	if !ok {
		t.Fatalf("decoded = %T, want *ReadCoilsResponse", decoded)
	}
	if want := []bool{true, true, true}; !reflect.DeepEqual(response.Bits, want) {
		t.Fatalf("bits = %v, want %v", response.Bits, want)
	}

	// Re-encoding drops the padding bits the decoder discarded.
	again, err := response.PDU()
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if want := []byte{0x01, 0x01, 0x07}; !bytes.Equal(again, want) {
		t.Fatalf("re-encoded = % x, want % x", again, want)
	}
}

func TestRequestPDURoundTrips(t *testing.T) {
	for _, tc := range requestGoldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			pdu, err := tc.msg.PDU()
			if err != nil {
				t.Fatalf("PDU: %v", err)
			}
			decoded, err := DecodePDU(pdu)
			if err != nil {
				t.Fatalf("DecodePDU: %v", err)
			}
			if !reflect.DeepEqual(decoded, tc.msg) {
				t.Fatalf("round trip = %#v, want %#v", decoded, tc.msg)
			}
		})
	}
}

func TestMaximumSizePDUs(t *testing.T) {
	coils := make([]bool, MaxWriteBits)
	for i := range coils {
		coils[i] = i%3 == 0
	}
	registers := make([]uint16, MaxWriteRegisters)
	for i := range registers {
		registers[i] = uint16(i * 0x0101)
	}
	readBits := make([]bool, MaxReadBits)
	for i := range readBits {
		readBits[i] = i%2 == 0
	}
	readRegisters := make([]uint16, MaxReadRegisters)
	for i := range readRegisters {
		readRegisters[i] = uint16(0xffff - i)
	}

	requests := []struct {
		name string
		msg  Message
		size int
	}{
		{"read coils", &ReadCoilsRequest{Address: 0xffff, Quantity: MaxReadBits}, 5},
		{"read discrete inputs", &ReadDiscreteInputsRequest{Address: 0, Quantity: MaxReadBits}, 5},
		{"read holding registers", &ReadHoldingRegistersRequest{Address: 0xffff, Quantity: MaxReadRegisters}, 5},
		{"read input registers", &ReadInputRegistersRequest{Address: 0xffff, Quantity: MaxReadRegisters}, 5},
		{"write multiple coils", &WriteMultipleCoilsRequest{Address: 0xffff, Values: coils}, 6 + (MaxWriteBits+7)/8},
		{"write multiple registers", &WriteMultipleRegistersRequest{Address: 0xffff, Values: registers}, 6 + 2*MaxWriteRegisters},
	}
	for _, tc := range requests {
		t.Run(tc.name, func(t *testing.T) {
			pdu, err := tc.msg.PDU()
			if err != nil {
				t.Fatalf("PDU: %v", err)
			}
			if len(pdu) != tc.size {
				t.Fatalf("encoded %d bytes, want %d", len(pdu), tc.size)
			}
			if len(pdu) > MaxPDU {
				t.Fatalf("encoded %d bytes, more than MaxPDU %d", len(pdu), MaxPDU)
			}
			decoded, err := DecodePDU(pdu)
			if err != nil {
				t.Fatalf("DecodePDU: %v", err)
			}
			if !reflect.DeepEqual(decoded, tc.msg) {
				t.Fatalf("round trip = %#v, want %#v", decoded, tc.msg)
			}
		})
	}

	responses := []struct {
		name    string
		request Message
		msg     Message
		size    int
	}{
		{"read coils", &ReadCoilsRequest{Quantity: MaxReadBits}, &ReadCoilsResponse{Bits: readBits}, 2 + (MaxReadBits+7)/8},
		{"read holding registers", &ReadHoldingRegistersRequest{Quantity: MaxReadRegisters}, &ReadHoldingRegistersResponse{Registers: readRegisters}, 2 + 2*MaxReadRegisters},
		{"write multiple coils", &WriteMultipleCoilsRequest{Address: 0xffff, Values: coils}, &WriteMultipleCoilsResponse{Address: 0xffff, Quantity: MaxWriteBits}, 5},
		{"write multiple registers", &WriteMultipleRegistersRequest{Address: 0xffff, Values: registers}, &WriteMultipleRegistersResponse{Address: 0xffff, Quantity: MaxWriteRegisters}, 5},
	}
	for _, tc := range responses {
		t.Run(tc.name, func(t *testing.T) {
			pdu, err := tc.msg.PDU()
			if err != nil {
				t.Fatalf("PDU: %v", err)
			}
			if len(pdu) != tc.size {
				t.Fatalf("encoded %d bytes, want %d", len(pdu), tc.size)
			}
			decoded, err := DecodeResponse(tc.request, pdu)
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			if !reflect.DeepEqual(decoded, tc.msg) {
				t.Fatalf("round trip = %#v, want %#v", decoded, tc.msg)
			}
		})
	}
}
