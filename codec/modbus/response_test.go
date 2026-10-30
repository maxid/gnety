package modbus

import (
	"reflect"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

func TestDecodeResponseMatchesRequest(t *testing.T) {
	cases := []struct {
		name string
		req  Message
		pdu  []byte
		want Message
	}{
		{
			"read coils exact quantity",
			&ReadCoilsRequest{Address: 0x0013, Quantity: 37},
			[]byte{0x01, 0x05, 0xcd, 0x6b, 0xb2, 0x0e, 0x1b},
			&ReadCoilsResponse{Bits: readCoilsBits37()},
		},
		{
			"read coils trims padding bits",
			&ReadCoilsRequest{Quantity: 3},
			[]byte{0x01, 0x01, 0xff},
			&ReadCoilsResponse{Bits: []bool{true, true, true}},
		},
		{
			"read discrete inputs exact quantity",
			&ReadDiscreteInputsRequest{Address: 0x00c4, Quantity: 22},
			[]byte{0x02, 0x03, 0xac, 0xdb, 0x35},
			&ReadDiscreteInputsResponse{Bits: readDiscreteBits22()},
		},
		{
			"read holding registers exact quantity",
			&ReadHoldingRegistersRequest{Quantity: 3},
			[]byte{0x03, 0x06, 0x02, 0x2b, 0x00, 0x00, 0x00, 0x64},
			&ReadHoldingRegistersResponse{Registers: []uint16{0x022b, 0x0000, 0x0064}},
		},
		{
			"read input registers exact quantity",
			&ReadInputRegistersRequest{Quantity: 1},
			[]byte{0x04, 0x02, 0x00, 0x0a},
			&ReadInputRegistersResponse{Registers: []uint16{0x000a}},
		},
		{
			"write single coil echo",
			&WriteSingleCoil{Address: 0x00ac, Value: true},
			[]byte{0x05, 0x00, 0xac, 0xff, 0x00},
			&WriteSingleCoil{Address: 0x00ac, Value: true},
		},
		{
			"write single register echo",
			&WriteSingleRegister{Address: 0x0001, Value: 0x0003},
			[]byte{0x06, 0x00, 0x01, 0x00, 0x03},
			&WriteSingleRegister{Address: 0x0001, Value: 0x0003},
		},
		{
			"write multiple coils acknowledgement",
			&WriteMultipleCoilsRequest{Address: 0x0013, Values: writeMultipleCoilsValues10()},
			[]byte{0x0f, 0x00, 0x13, 0x00, 0x0a},
			&WriteMultipleCoilsResponse{Address: 0x0013, Quantity: 10},
		},
		{
			"write multiple registers acknowledgement",
			&WriteMultipleRegistersRequest{Address: 0x0001, Values: []uint16{10, 258}},
			[]byte{0x10, 0x00, 0x01, 0x00, 0x02},
			&WriteMultipleRegistersResponse{Address: 0x0001, Quantity: 2},
		},
		{
			"exception response",
			&ReadHoldingRegistersRequest{Quantity: 1},
			[]byte{0x83, 0x02},
			&Exception{FunctionCode: FuncReadHoldingRegisters, Code: ExceptionIllegalDataAddress},
		},
		{
			"exception with unnamed code",
			&ReadInputRegistersRequest{Quantity: 1},
			[]byte{0x84, 0x7f},
			&Exception{FunctionCode: FuncReadInputRegisters, Code: ExceptionCode(0x7f)},
		},
		{
			"value request instead of pointer",
			ReadCoilsRequest{Quantity: 2},
			[]byte{0x01, 0x01, 0x03},
			&ReadCoilsResponse{Bits: []bool{true, true}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeResponse(tc.req, tc.pdu)
			if err != nil {
				t.Fatalf("DecodeResponse(% x): %v", tc.pdu, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("DecodeResponse = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestDecodeResponseRejections(t *testing.T) {
	tooLong := append([]byte{FuncReadHoldingRegisters}, make([]byte, MaxPDU)...)

	cases := []struct {
		name string
		req  Message
		pdu  []byte
		want wire.Kind
	}{
		{"nil request", nil, []byte{0x03, 0x02, 0x00, 0x01}, wire.KindMalformed},
		{"response is not a request", &ReadCoilsResponse{}, []byte{0x01, 0x01, 0x00}, wire.KindUnsupported},
		{"invalid request quantity", &ReadCoilsRequest{Quantity: 0}, []byte{0x01, 0x01, 0x00}, wire.KindMalformed},
		{"empty response", &ReadCoilsRequest{Quantity: 1}, nil, wire.KindMalformed},
		{"response longer than MaxPDU", &ReadHoldingRegistersRequest{Quantity: 1}, tooLong, wire.KindTooLarge},
		{"function mismatch", &ReadHoldingRegistersRequest{Quantity: 1}, []byte{0x01, 0x01, 0x00}, wire.KindMalformed},
		{"truncated read response", &ReadCoilsRequest{Quantity: 1}, []byte{0x01}, wire.KindMalformed},
		{"read response byte count zero", &ReadCoilsRequest{Quantity: 1}, []byte{0x01, 0x00}, wire.KindMalformed},
		{"read response data too short", &ReadCoilsRequest{Quantity: 1}, []byte{0x01, 0x02, 0x00}, wire.KindMalformed},
		{"read response byte count too large", &ReadCoilsRequest{Quantity: 1}, []byte{0x01, 0x05, 0, 0, 0, 0, 0}, wire.KindMalformed},
		{"read coils wrong byte count", &ReadCoilsRequest{Quantity: 10}, []byte{0x01, 0x05, 0xcd, 0x6b, 0xb2, 0x0e, 0x1b}, wire.KindMalformed},
		{"read discrete wrong byte count", &ReadDiscreteInputsRequest{Quantity: 10}, []byte{0x02, 0x03, 0xac, 0xdb, 0x35}, wire.KindMalformed},
		{"read registers odd byte count", &ReadHoldingRegistersRequest{Quantity: 1}, []byte{0x03, 0x03, 0x00, 0x01, 0x02}, wire.KindMalformed},
		{"read registers wrong count", &ReadHoldingRegistersRequest{Quantity: 1}, []byte{0x03, 0x04, 0x00, 0x01, 0x00, 0x02}, wire.KindMalformed},
		{"write single coil value mismatch", &WriteSingleCoil{Address: 0x00ac, Value: true}, []byte{0x05, 0x00, 0xac, 0x00, 0x00}, wire.KindMalformed},
		{"write single coil address mismatch", &WriteSingleCoil{Address: 0x00ac, Value: true}, []byte{0x05, 0x00, 0xad, 0xff, 0x00}, wire.KindMalformed},
		{"write single register value mismatch", &WriteSingleRegister{Address: 1, Value: 3}, []byte{0x06, 0x00, 0x01, 0x00, 0x04}, wire.KindMalformed},
		{"write multiple coils quantity mismatch", &WriteMultipleCoilsRequest{Address: 0x0013, Values: writeMultipleCoilsValues10()}, []byte{0x0f, 0x00, 0x13, 0x00, 0x09}, wire.KindMalformed},
		{"write multiple coils address mismatch", &WriteMultipleCoilsRequest{Address: 0x0013, Values: writeMultipleCoilsValues10()}, []byte{0x0f, 0x00, 0x14, 0x00, 0x0a}, wire.KindMalformed},
		{"write multiple registers quantity mismatch", &WriteMultipleRegistersRequest{Address: 1, Values: []uint16{10, 258}}, []byte{0x10, 0x00, 0x01, 0x00, 0x01}, wire.KindMalformed},
		{"write multiple response truncated", &WriteMultipleCoilsRequest{Address: 0x0013, Values: writeMultipleCoilsValues10()}, []byte{0x0f, 0x00, 0x13, 0x00}, wire.KindMalformed},
		{"exception function mismatch", &ReadHoldingRegistersRequest{Quantity: 1}, []byte{0x84, 0x02}, wire.KindMalformed},
		{"exception truncated", &ReadHoldingRegistersRequest{Quantity: 1}, []byte{0x83}, wire.KindMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeResponse(tc.req, tc.pdu)
			if got := wire.KindOf(err); got != tc.want {
				t.Fatalf("kind = %v, want %v (err %v)", got, tc.want, err)
			}
		})
	}

	var typedNil *ReadCoilsRequest
	if _, err := DecodeResponse(typedNil, []byte{0x01, 0x01, 0x00}); wire.KindOf(err) != wire.KindMalformed {
		t.Fatalf("typed nil request: kind = %v, want malformed", wire.KindOf(err))
	}
}
