package modbus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

// fakeMessage lets the tests exercise the EncodePDU guards no real message can
// reach.
type fakeMessage struct {
	function byte
	pdu      []byte
	err      error
}

func (m fakeMessage) Function() byte { return m.function }

func (m fakeMessage) PDU() ([]byte, error) { return m.pdu, m.err }

func TestDecodePDURejections(t *testing.T) {
	tooLong := append([]byte{FuncReadHoldingRegisters}, make([]byte, MaxPDU)...)

	tooManyCoils := make([]byte, 6+(1969+7)/8)
	tooManyCoils[0] = FuncWriteMultipleCoils
	binary.BigEndian.PutUint16(tooManyCoils[1:3], 0x0001)
	binary.BigEndian.PutUint16(tooManyCoils[3:5], 1969)
	tooManyCoils[5] = (1969 + 7) / 8

	tooManyRegisters := make([]byte, 6+2*MaxWriteRegisters)
	tooManyRegisters[0] = FuncWriteMultipleRegisters
	binary.BigEndian.PutUint16(tooManyRegisters[1:3], 0x0001)
	binary.BigEndian.PutUint16(tooManyRegisters[3:5], 124)
	tooManyRegisters[5] = 2 * MaxWriteRegisters

	cases := []struct {
		name string
		pdu  []byte
		want wire.Kind
	}{
		{"empty", nil, wire.KindMalformed},
		{"longer than MaxPDU", tooLong, wire.KindTooLarge},
		{"unknown function 0x00", []byte{0x00}, wire.KindUnsupported},
		{"unknown function 0x07", []byte{0x07, 0x00, 0x00}, wire.KindUnsupported},
		{"unknown function 0x11", []byte{0x11, 0x00}, wire.KindUnsupported},
		{"unknown function 0x2b", []byte{0x2b, 0x0e, 0x01, 0x00}, wire.KindUnsupported},
		{"truncated read coils", []byte{0x01, 0x00, 0x13, 0x00}, wire.KindMalformed},
		{"read coils with trailing byte", []byte{0x01, 0x00, 0x13, 0x00, 0x25, 0x00}, wire.KindMalformed},
		{"read coils quantity zero", []byte{0x01, 0x00, 0x13, 0x00, 0x00}, wire.KindMalformed},
		{"read coils quantity above limit", []byte{0x01, 0x00, 0x13, 0x07, 0xd1}, wire.KindMalformed},
		{"read discrete quantity above limit", []byte{0x02, 0x00, 0x00, 0x07, 0xd1}, wire.KindMalformed},
		{"read holding quantity zero", []byte{0x03, 0x00, 0x00, 0x00, 0x00}, wire.KindMalformed},
		{"read holding quantity above limit", []byte{0x03, 0x00, 0x00, 0x00, 0x7e}, wire.KindMalformed},
		{"read input quantity above limit", []byte{0x04, 0x00, 0x00, 0x00, 0x7e}, wire.KindMalformed},
		{"read response is not a request", []byte{0x03, 0x06, 0x02, 0x2b, 0x00}, wire.KindMalformed},
		{"write single coil bad value", []byte{0x05, 0x00, 0xac, 0x12, 0x34}, wire.KindMalformed},
		{"write single coil truncated", []byte{0x05, 0x00, 0xac, 0xff}, wire.KindMalformed},
		{"write single register truncated", []byte{0x06, 0x00, 0x01}, wire.KindMalformed},
		{"write single register trailing byte", []byte{0x06, 0x00, 0x01, 0x00, 0x03, 0x00}, wire.KindMalformed},
		{"write multiple coils quantity zero", []byte{0x0f, 0x00, 0x13, 0x00, 0x00, 0x00}, wire.KindMalformed},
		{"write multiple coils quantity above limit", tooManyCoils, wire.KindMalformed},
		{"write multiple coils byte count mismatch", []byte{0x0f, 0x00, 0x13, 0x00, 0x0a, 0x03, 0xcd, 0x01, 0x00}, wire.KindMalformed},
		{"write multiple coils truncated data", []byte{0x0f, 0x00, 0x13, 0x00, 0x0a, 0x02, 0xcd}, wire.KindMalformed},
		{"write multiple coils response is not a request", []byte{0x0f, 0x00, 0x13, 0x00, 0x0a}, wire.KindMalformed},
		{"write multiple registers quantity above limit", tooManyRegisters, wire.KindMalformed},
		{"write multiple registers byte count mismatch", []byte{0x10, 0x00, 0x01, 0x00, 0x02, 0x03, 0x00, 0x0a, 0x00}, wire.KindMalformed},
		{"write multiple registers truncated data", []byte{0x10, 0x00, 0x01, 0x00, 0x02, 0x04, 0x00, 0x0a}, wire.KindMalformed},
		{"exception truncated", []byte{0x83}, wire.KindMalformed},
		{"exception with trailing byte", []byte{0x83, 0x02, 0x00}, wire.KindMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodePDU(tc.pdu)
			if got := wire.KindOf(err); got != tc.want {
				t.Fatalf("kind = %v, want %v (err %v)", got, tc.want, err)
			}
		})
	}
}

func TestEncodePDURejections(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
		want wire.Kind
	}{
		{"read coils quantity zero", &ReadCoilsRequest{Quantity: 0}, wire.KindMalformed},
		{"read coils quantity above limit", &ReadCoilsRequest{Quantity: MaxReadBits + 1}, wire.KindMalformed},
		{"read discrete quantity zero", &ReadDiscreteInputsRequest{Quantity: 0}, wire.KindMalformed},
		{"read discrete quantity above limit", &ReadDiscreteInputsRequest{Quantity: MaxReadBits + 1}, wire.KindMalformed},
		{"read holding quantity zero", &ReadHoldingRegistersRequest{Quantity: 0}, wire.KindMalformed},
		{"read holding quantity above limit", &ReadHoldingRegistersRequest{Quantity: MaxReadRegisters + 1}, wire.KindMalformed},
		{"read input quantity above limit", &ReadInputRegistersRequest{Quantity: MaxReadRegisters + 1}, wire.KindMalformed},
		{"read coils response empty", &ReadCoilsResponse{}, wire.KindMalformed},
		{"read coils response above limit", &ReadCoilsResponse{Bits: make([]bool, MaxReadBits+1)}, wire.KindMalformed},
		{"read discrete response empty", &ReadDiscreteInputsResponse{}, wire.KindMalformed},
		{"read holding response empty", &ReadHoldingRegistersResponse{}, wire.KindMalformed},
		{"read holding response above limit", &ReadHoldingRegistersResponse{Registers: make([]uint16, MaxReadRegisters+1)}, wire.KindMalformed},
		{"read input response above limit", &ReadInputRegistersResponse{Registers: make([]uint16, MaxReadRegisters+1)}, wire.KindMalformed},
		{"write multiple coils empty", &WriteMultipleCoilsRequest{}, wire.KindMalformed},
		{"write multiple coils above limit", &WriteMultipleCoilsRequest{Values: make([]bool, MaxWriteBits+1)}, wire.KindMalformed},
		{"write multiple registers empty", &WriteMultipleRegistersRequest{}, wire.KindMalformed},
		{"write multiple registers above limit", &WriteMultipleRegistersRequest{Values: make([]uint16, MaxWriteRegisters+1)}, wire.KindMalformed},
		{"write multiple coils response quantity zero", &WriteMultipleCoilsResponse{Quantity: 0}, wire.KindMalformed},
		{"write multiple coils response quantity above limit", &WriteMultipleCoilsResponse{Quantity: MaxWriteBits + 1}, wire.KindMalformed},
		{"write multiple registers response quantity zero", &WriteMultipleRegistersResponse{Quantity: 0}, wire.KindMalformed},
		{"write multiple registers response quantity above limit", &WriteMultipleRegistersResponse{Quantity: MaxWriteRegisters + 1}, wire.KindMalformed},
		{"exception function code already marked", &Exception{FunctionCode: 0x83, Code: ExceptionIllegalFunction}, wire.KindMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.msg.PDU()
			if got := wire.KindOf(err); got != tc.want {
				t.Fatalf("kind = %v, want %v (err %v)", got, tc.want, err)
			}
			var buf bytes.Buffer
			err = EncodePDU(&buf, tc.msg)
			if got := wire.KindOf(err); got != tc.want {
				t.Fatalf("EncodePDU kind = %v, want %v (err %v)", got, tc.want, err)
			}
			if buf.Len() != 0 {
				t.Fatalf("EncodePDU wrote %d bytes of a rejected message", buf.Len())
			}
		})
	}
}

func TestEncodePDUGuards(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePDU(nil, &ReadCoilsRequest{Quantity: 1}); wire.KindOf(err) != wire.KindMalformed {
		t.Fatalf("nil writer: kind = %v, want malformed", wire.KindOf(err))
	}
	if err := EncodePDU(&buf, nil); wire.KindOf(err) != wire.KindMalformed {
		t.Fatalf("nil message: kind = %v, want malformed", wire.KindOf(err))
	}
	var typedNil *ReadCoilsRequest
	if err := EncodePDU(&buf, typedNil); wire.KindOf(err) != wire.KindMalformed {
		t.Fatalf("typed nil message: kind = %v, want malformed", wire.KindOf(err))
	}
	if buf.Len() != 0 {
		t.Fatalf("EncodePDU wrote %d bytes for a rejected message", buf.Len())
	}

	cases := []struct {
		name string
		msg  Message
		want wire.Kind
	}{
		{"empty PDU", fakeMessage{function: 0x01}, wire.KindMalformed},
		{"function mismatch", fakeMessage{function: 0x04, pdu: []byte{0x03, 0, 1, 0, 1}}, wire.KindMalformed},
		{"oversized PDU", fakeMessage{function: 0x03, pdu: append([]byte{0x03}, make([]byte, MaxPDU)...)}, wire.KindTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := EncodePDU(&bytes.Buffer{}, tc.msg)
			if got := wire.KindOf(err); got != tc.want {
				t.Fatalf("kind = %v, want %v (err %v)", got, tc.want, err)
			}
		})
	}

	// An error from PDU itself passes through with its classification.
	sentinel := wire.Errorf(wire.KindTooLarge, "modbus: test sentinel")
	err := EncodePDU(&bytes.Buffer{}, fakeMessage{function: 0x03, err: sentinel})
	if !errors.Is(err, sentinel) {
		t.Fatalf("EncodePDU error = %v, want the PDU error", err)
	}
}

func TestEncodePDUWritesOnePDU(t *testing.T) {
	var buf bytes.Buffer
	msg := &ReadHoldingRegistersRequest{Address: 0x006b, Quantity: 3}
	if err := EncodePDU(&buf, msg); err != nil {
		t.Fatalf("EncodePDU: %v", err)
	}
	want := []byte{0x03, 0x00, 0x6b, 0x00, 0x03}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("wrote % x, want % x", buf.Bytes(), want)
	}
}
