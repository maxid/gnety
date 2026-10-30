package modbus

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

func TestTypedFrameMarshal(t *testing.T) {
	frame := TypedFrame{
		TransactionID: 0x1234,
		UnitID:        0x11,
		Message:       &ReadHoldingRegistersRequest{Address: 0x0010, Quantity: 2},
	}
	got, err := frame.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := []byte{0x12, 0x34, 0x00, 0x00, 0x00, 0x06, 0x11, 0x03, 0x00, 0x10, 0x00, 0x02}
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal = % x, want % x", got, want)
	}
	if length := binary.BigEndian.Uint16(got[4:6]); length != 6 {
		t.Fatalf("length = %d, want 6", length)
	}

	// The raw frame convenience produces the same bytes through Frame.Marshal.
	raw, err := frame.Frame()
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}
	rawBytes, err := raw.Marshal()
	if err != nil {
		t.Fatalf("raw Marshal: %v", err)
	}
	if !bytes.Equal(rawBytes, want) {
		t.Fatalf("raw Marshal = % x, want % x", rawBytes, want)
	}
	if raw.TransactionID != frame.TransactionID || raw.UnitID != frame.UnitID {
		t.Fatalf("raw frame header = %+v", raw)
	}
	if !bytes.Equal(raw.PDU, []byte{0x03, 0x00, 0x10, 0x00, 0x02}) {
		t.Fatalf("raw PDU = % x", raw.PDU)
	}
}

func TestReadTypedRoundTrip(t *testing.T) {
	original := TypedFrame{
		TransactionID: 7,
		UnitID:        3,
		Message:       &WriteMultipleRegistersRequest{Address: 0x0001, Values: []uint16{10, 258}},
	}
	encoded, err := original.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	decoded, err := ReadTyped(bufio.NewReader(bytes.NewReader(encoded)), 0)
	if err != nil {
		t.Fatalf("ReadTyped: %v", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("ReadTyped = %#v, want %#v", decoded, original)
	}

	// Existing Read plus DecodePDU is the raw equivalent.
	raw, err := Read(bufio.NewReader(bytes.NewReader(encoded)), 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	message, err := DecodePDU(raw.PDU)
	if err != nil {
		t.Fatalf("DecodePDU: %v", err)
	}
	if !reflect.DeepEqual(message, original.Message) {
		t.Fatalf("DecodePDU = %#v, want %#v", message, original.Message)
	}
}

func TestReadTypedResponse(t *testing.T) {
	response := TypedFrame{
		TransactionID: 9,
		UnitID:        1,
		Message:       &ReadHoldingRegistersResponse{Registers: []uint16{10, 20}},
	}
	encoded, err := response.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	request := &ReadHoldingRegistersRequest{Address: 0, Quantity: 2}
	decoded, err := ReadTypedResponse(bufio.NewReader(bytes.NewReader(encoded)), 0, request)
	if err != nil {
		t.Fatalf("ReadTypedResponse: %v", err)
	}
	if !reflect.DeepEqual(decoded, response) {
		t.Fatalf("ReadTypedResponse = %#v, want %#v", decoded, response)
	}

	// A response that does not answer the request is rejected.
	_, err = ReadTypedResponse(bufio.NewReader(bytes.NewReader(encoded)), 0, &ReadHoldingRegistersRequest{Quantity: 1})
	if got := wire.KindOf(err); got != wire.KindMalformed {
		t.Fatalf("mismatched request: kind = %v, want malformed (err %v)", got, err)
	}

	// An exception response is reported as a typed message, not a decode
	// failure.
	exception, err := TypedFrame{
		TransactionID: 1,
		UnitID:        1,
		Message:       &Exception{FunctionCode: FuncReadHoldingRegisters, Code: ExceptionIllegalDataAddress},
	}.Marshal()
	if err != nil {
		t.Fatalf("exception Marshal: %v", err)
	}
	got, err := ReadTypedResponse(bufio.NewReader(bytes.NewReader(exception)), 0, request)
	if err != nil {
		t.Fatalf("ReadTypedResponse(exception): %v", err)
	}
	if _, ok := got.Message.(*Exception); !ok {
		t.Fatalf("message = %T, want *Exception", got.Message)
	}
}

func TestTypedFrameRejections(t *testing.T) {
	if _, err := (TypedFrame{TransactionID: 1}).Marshal(); wire.KindOf(err) != wire.KindMalformed {
		t.Fatalf("nil message Marshal: kind = %v, want malformed", wire.KindOf(err))
	}
	if _, err := (TypedFrame{TransactionID: 1}).Frame(); wire.KindOf(err) != wire.KindMalformed {
		t.Fatalf("nil message Frame: kind = %v, want malformed", wire.KindOf(err))
	}
	invalid := TypedFrame{Message: &ReadCoilsRequest{Quantity: 0}}
	if _, err := invalid.Marshal(); wire.KindOf(err) != wire.KindMalformed {
		t.Fatalf("invalid message Marshal: kind = %v, want malformed", wire.KindOf(err))
	}

	// A truncated MBAP frame keeps the reader's incomplete classification.
	if _, err := ReadTyped(bufio.NewReader(bytes.NewReader([]byte{0, 1, 0, 0})), 0); wire.KindOf(err) != wire.KindIncomplete {
		t.Fatalf("truncated frame: kind = %v, want incomplete", wire.KindOf(err))
	}
}
