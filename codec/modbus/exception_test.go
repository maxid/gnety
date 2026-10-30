package modbus

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func TestExceptionCodeNames(t *testing.T) {
	cases := []struct {
		code ExceptionCode
		want string
	}{
		{ExceptionIllegalFunction, "illegal function"},
		{ExceptionIllegalDataAddress, "illegal data address"},
		{ExceptionIllegalDataValue, "illegal data value"},
		{ExceptionServerDeviceFailure, "server device failure"},
		{ExceptionAcknowledge, "acknowledge"},
		{ExceptionServerDeviceBusy, "server device busy"},
		{ExceptionMemoryParityError, "memory parity error"},
		{ExceptionGatewayPathUnavailable, "gateway path unavailable"},
		{ExceptionGatewayTargetFailedToRespond, "gateway target failed to respond"},
	}
	for _, tc := range cases {
		if got := tc.code.String(); got != tc.want {
			t.Fatalf("ExceptionCode(%#02x).String() = %q, want %q", byte(tc.code), got, tc.want)
		}
	}
	if got := ExceptionCode(0x07).String(); got != "unknown exception code 0x07" {
		t.Fatalf("unmapped code = %q", got)
	}
}

func TestExceptionDecodeAndError(t *testing.T) {
	for _, code := range []ExceptionCode{
		ExceptionIllegalFunction,
		ExceptionIllegalDataAddress,
		ExceptionIllegalDataValue,
		ExceptionServerDeviceFailure,
		ExceptionAcknowledge,
		ExceptionServerDeviceBusy,
		ExceptionMemoryParityError,
		ExceptionGatewayPathUnavailable,
		ExceptionGatewayTargetFailedToRespond,
		ExceptionCode(0x7f),
	} {
		pdu := []byte{0x83, byte(code)}
		decoded, err := DecodePDU(pdu)
		if err != nil {
			t.Fatalf("DecodePDU(% x): %v", pdu, err)
		}
		want := &Exception{FunctionCode: FuncReadHoldingRegisters, Code: code}
		if decoded.Function() != 0x83 {
			t.Fatalf("Function() = %#02x, want 0x83", decoded.Function())
		}
		exception, ok := decoded.(*Exception)
		if !ok {
			t.Fatalf("decoded = %T, want *Exception", decoded)
		}
		if *exception != *want {
			t.Fatalf("decoded = %#v, want %#v", exception, want)
		}
		if !errors.Is(decoded.(error), ErrException) {
			t.Fatalf("exception does not match ErrException")
		}

		again, err := decoded.PDU()
		if err != nil {
			t.Fatalf("PDU: %v", err)
		}
		if !bytes.Equal(again, pdu) {
			t.Fatalf("re-encoded = % x, want % x", again, pdu)
		}
	}
}

func TestExceptionErrorsAsAndIs(t *testing.T) {
	decoded, err := DecodePDU([]byte{0x83, byte(ExceptionIllegalDataAddress)})
	if err != nil {
		t.Fatalf("DecodePDU: %v", err)
	}
	exception, ok := decoded.(error)
	if !ok {
		t.Fatalf("decoded %T does not implement error", decoded)
	}
	if !errors.Is(exception, ErrException) {
		t.Fatalf("errors.Is(err, ErrException) = false")
	}

	wrapped := fmt.Errorf("client: %w", exception)
	var target *Exception
	if !errors.As(wrapped, &target) {
		t.Fatalf("errors.As did not recover *Exception")
	}
	if target.Code != ExceptionIllegalDataAddress {
		t.Fatalf("code = %#02x, want illegal data address", byte(target.Code))
	}
	if target.FunctionCode != FuncReadHoldingRegisters {
		t.Fatalf("function = %#02x, want 0x03", target.FunctionCode)
	}
	if !errors.Is(wrapped, ErrException) {
		t.Fatalf("errors.Is(wrapped, ErrException) = false")
	}

	value := Exception{FunctionCode: FuncReadHoldingRegisters, Code: ExceptionIllegalDataAddress}
	if !errors.Is(value, ErrException) {
		t.Fatalf("errors.Is(Exception, ErrException) = false")
	}
	if value.Error() != "modbus: function 0x03 exception: illegal data address" {
		t.Fatalf("Error() = %q", value.Error())
	}
}
