package modbus

import (
	"bufio"
	"bytes"
	"errors"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

func TestMaskWriteRegister(t *testing.T) {
	req := &MaskWriteRegister{Address: 0x0004, AndMask: 0x00f2, OrMask: 0x0025}
	pdu := mustPDU(t, req)
	want := []byte{0x16, 0x00, 0x04, 0x00, 0xf2, 0x00, 0x25}
	if !bytes.Equal(pdu, want) {
		t.Fatalf("PDU = % x, want % x", pdu, want)
	}
	decoded, err := DecodePDU(pdu)
	if err != nil {
		t.Fatalf("DecodePDU: %v", err)
	}
	if decoded.(*MaskWriteRegister).OrMask != 0x0025 {
		t.Fatalf("decoded %#v", decoded)
	}
	echo, err := DecodeResponse(req, pdu)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if got := echo.(*MaskWriteRegister); *got != *req {
		t.Fatalf("echo = %#v", got)
	}

	changed := append([]byte(nil), pdu...)
	changed[4] ^= 0xff
	if _, err := DecodeResponse(req, changed); !errors.Is(err, wire.ErrMalformed) {
		t.Fatalf("changed mask: err = %v, want malformed", err)
	}
	if _, err := DecodePDU(pdu[:6]); !errors.Is(err, wire.ErrMalformed) {
		t.Fatalf("truncated: err = %v, want malformed", err)
	}
}

func TestReadWriteMultipleRegisters(t *testing.T) {
	req := &ReadWriteMultipleRegistersRequest{
		ReadAddress:  0x0000,
		ReadQuantity: 2,
		WriteAddress: 0x0010,
		WriteValues:  []uint16{7, 8},
	}
	pdu := mustPDU(t, req)
	want := []byte{0x17, 0x00, 0x00, 0x00, 0x02, 0x00, 0x10, 0x00, 0x02, 0x04, 0x00, 0x07, 0x00, 0x08}
	if !bytes.Equal(pdu, want) {
		t.Fatalf("PDU = % x, want % x", pdu, want)
	}
	decoded, err := DecodePDU(pdu)
	if err != nil {
		t.Fatalf("DecodePDU: %v", err)
	}
	got := decoded.(*ReadWriteMultipleRegistersRequest)
	if got.ReadQuantity != 2 || got.WriteAddress != 0x0010 || len(got.WriteValues) != 2 || got.WriteValues[1] != 8 {
		t.Fatalf("decoded %#v", got)
	}

	resp := &ReadWriteMultipleRegistersResponse{Registers: []uint16{11, 12}}
	respPDU := mustPDU(t, resp)
	if want := []byte{0x17, 0x04, 0x00, 0x0b, 0x00, 0x0c}; !bytes.Equal(respPDU, want) {
		t.Fatalf("response PDU = % x, want % x", respPDU, want)
	}
	decodedResp, err := DecodeResponse(req, respPDU)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if got := decodedResp.(*ReadWriteMultipleRegistersResponse); got.Registers[0] != 11 {
		t.Fatalf("decoded response %#v", got)
	}

	// A response with the wrong register count does not answer the request.
	if _, err := DecodeResponse(req, []byte{0x17, 0x02, 0x00, 0x0b}); !errors.Is(err, wire.ErrMalformed) {
		t.Fatalf("short response: err = %v, want malformed", err)
	}
	// The write side is capped at 121 registers.
	tooMany := &ReadWriteMultipleRegistersRequest{ReadQuantity: 1, WriteValues: make([]uint16, MaxWriteReadRegisters+1)}
	if _, err := tooMany.PDU(); !errors.Is(err, wire.ErrMalformed) {
		t.Fatalf("write limit: err = %v, want malformed", err)
	}
}

func TestReadDeviceIdentification(t *testing.T) {
	req := &ReadDeviceIdentificationRequest{ReadCode: DeviceIDBasic}
	pdu := mustPDU(t, req)
	if want := []byte{0x2b, 0x0e, 0x01, 0x00}; !bytes.Equal(pdu, want) {
		t.Fatalf("PDU = % x, want % x", pdu, want)
	}
	decoded, err := DecodePDU(pdu)
	if err != nil {
		t.Fatalf("DecodePDU: %v", err)
	}
	if got := decoded.(*ReadDeviceIdentificationRequest); got.ReadCode != DeviceIDBasic {
		t.Fatalf("decoded %#v", got)
	}

	resp := &ReadDeviceIdentificationResponse{
		ReadCode:        DeviceIDBasic,
		ConformityLevel: 0x01,
		MoreFollows:     true,
		NextObjectID:    DeviceIDMajorMinorRevision,
		Objects: []DeviceObject{
			{ID: DeviceIDVendorName, Value: []byte("ACME")},
			{ID: DeviceIDProductCode, Value: []byte("P1")},
		},
	}
	respPDU := mustPDU(t, resp)
	decodedResp, err := DecodeResponse(req, respPDU)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	got := decodedResp.(*ReadDeviceIdentificationResponse)
	if !got.MoreFollows || got.NextObjectID != DeviceIDMajorMinorRevision || len(got.Objects) != 2 {
		t.Fatalf("decoded response %#v", got)
	}
	if string(got.Objects[0].Value) != "ACME" {
		t.Fatalf("vendor name = %q", got.Objects[0].Value)
	}

	// A response to another read code is not this request's answer.
	other := *resp
	other.ReadCode = DeviceIDRegular
	if _, err := DecodeResponse(req, mustPDU(t, other)); !errors.Is(err, wire.ErrMalformed) {
		t.Fatalf("read code mismatch: err = %v, want malformed", err)
	}
	// The object count must match the bytes that follow.
	if _, err := DecodeResponse(req, append(append([]byte(nil), respPDU...), 0x00)); !errors.Is(err, wire.ErrMalformed) {
		t.Fatalf("trailing byte: err = %v, want malformed", err)
	}
}

func TestReadDeviceIdentificationRejections(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
		want wire.Kind
	}{
		{"read code zero", &ReadDeviceIdentificationRequest{ReadCode: 0}, wire.KindMalformed},
		{"read code above 4", &ReadDeviceIdentificationRequest{ReadCode: 5}, wire.KindMalformed},
		{"conformity zero", &ReadDeviceIdentificationResponse{ReadCode: 1, ConformityLevel: 0}, wire.KindMalformed},
		{"conformity 0x7f", &ReadDeviceIdentificationResponse{ReadCode: 1, ConformityLevel: 0x7f}, wire.KindMalformed},
		{"object value too long", &ReadDeviceIdentificationResponse{
			ReadCode:        1,
			ConformityLevel: 1,
			Objects:         []DeviceObject{{ID: 0, Value: make([]byte, 256)}},
		}, wire.KindTooLarge},
		{"too many objects", &ReadDeviceIdentificationResponse{
			ReadCode:        1,
			ConformityLevel: 1,
			Objects:         make([]DeviceObject, 256),
		}, wire.KindTooLarge},
		{"PDU too long", &ReadDeviceIdentificationResponse{
			ReadCode:        1,
			ConformityLevel: 1,
			Objects:         []DeviceObject{{ID: 0, Value: make([]byte, 250)}},
		}, wire.KindTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.msg.PDU()
			if got := wire.KindOf(err); got != tc.want {
				t.Fatalf("kind = %v, want %v (err %v)", got, tc.want, err)
			}
		})
	}
}

func TestDeviceIdentificationDecodeRejections(t *testing.T) {
	cases := []struct {
		name string
		pdu  []byte
		want wire.Kind
	}{
		{"unsupported MEI", []byte{0x2b, 0x0d, 0x01, 0x00}, wire.KindUnsupported},
		{"short fixed part", []byte{0x2b, 0x0e, 0x01, 0x01, 0x00, 0x00}, wire.KindMalformed},
		{"bad more-follows", []byte{0x2b, 0x0e, 0x01, 0x01, 0x01, 0x00, 0x00}, wire.KindMalformed},
		{"bad conformity", []byte{0x2b, 0x0e, 0x01, 0x7f, 0x00, 0x00, 0x00}, wire.KindMalformed},
		{"object header truncated", []byte{0x2b, 0x0e, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00}, wire.KindMalformed},
		{"object value truncated", []byte{0x2b, 0x0e, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x05, 'a'}, wire.KindMalformed},
		{"object count mismatch", []byte{0x2b, 0x0e, 0x01, 0x01, 0x00, 0x00, 0x02, 0x00, 0x00}, wire.KindMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeResponse(&ReadDeviceIdentificationRequest{ReadCode: DeviceIDBasic}, tc.pdu)
			if got := wire.KindOf(err); got != tc.want {
				t.Fatalf("kind = %v, want %v (err %v)", got, tc.want, err)
			}
		})
	}
}

func TestDeviceIdentificationRTUFraming(t *testing.T) {
	req := &ReadDeviceIdentificationRequest{ReadCode: DeviceIDBasic, ObjectID: 0}
	resp := &ReadDeviceIdentificationResponse{
		ReadCode:        DeviceIDBasic,
		ConformityLevel: 0x01,
		Objects: []DeviceObject{
			{ID: DeviceIDVendorName, Value: []byte("ACME")},
			{ID: DeviceIDProductCode, Value: []byte("P1")},
			{ID: DeviceIDMajorMinorRevision, Value: []byte("1.2")},
		},
	}
	raw := mustMarshalRTU(t, 1, mustPDU(t, resp))
	frame, err := (RTUOptions{}).ReadTypedResponse(bufioReader(raw), 0, nil, req)
	if err != nil {
		t.Fatalf("ReadTypedResponse: %v", err)
	}
	got := frame.Message.(*ReadDeviceIdentificationResponse)
	if len(got.Objects) != 3 || string(got.Objects[2].Value) != "1.2" {
		t.Fatalf("decoded %#v", got)
	}
}

func mustMarshalRTU(t *testing.T, unit uint8, pdu []byte) []byte {
	t.Helper()
	raw, err := RTUFrame{UnitID: unit, PDU: pdu}.Marshal()
	if err != nil {
		t.Fatalf("RTU Marshal: %v", err)
	}
	return raw
}

func bufioReader(b []byte) *bufio.Reader { return bufio.NewReader(bytes.NewReader(b)) }
