package modbus

import (
	"errors"
	"fmt"
)

// ErrException is the sentinel every Exception wraps, so
// errors.Is(err, ErrException) recognizes an exception response whatever its
// code. Decoding an exception response succeeds; whether to treat it as an
// error is the caller's decision.
var ErrException = errors.New("modbus: exception response")

// ExceptionCode is the one-byte code of an exception response.
type ExceptionCode byte

// Exception codes from the Modbus application protocol specification.
const (
	// ExceptionIllegalFunction is 0x01: the server does not support the
	// function code.
	ExceptionIllegalFunction ExceptionCode = 0x01
	// ExceptionIllegalDataAddress is 0x02: the address or address range is not
	// available on the server.
	ExceptionIllegalDataAddress ExceptionCode = 0x02
	// ExceptionIllegalDataValue is 0x03: a value in the request data field is
	// outside what the server accepts.
	ExceptionIllegalDataValue ExceptionCode = 0x03
	// ExceptionServerDeviceFailure is 0x04: the server failed while processing
	// the request.
	ExceptionServerDeviceFailure ExceptionCode = 0x04
	// ExceptionAcknowledge is 0x05: the server accepted the request and needs a
	// long time to process it.
	ExceptionAcknowledge ExceptionCode = 0x05
	// ExceptionServerDeviceBusy is 0x06: the server is busy with a long-running
	// operation and the client should retry later.
	ExceptionServerDeviceBusy ExceptionCode = 0x06
	// ExceptionMemoryParityError is 0x08: the server detected a parity error
	// reading its extended memory.
	ExceptionMemoryParityError ExceptionCode = 0x08
	// ExceptionGatewayPathUnavailable is 0x0A: the gateway could not allocate a
	// path to the target device.
	ExceptionGatewayPathUnavailable ExceptionCode = 0x0a
	// ExceptionGatewayTargetFailedToRespond is 0x0B: the gateway reached the
	// path but the target device did not answer.
	ExceptionGatewayTargetFailedToRespond ExceptionCode = 0x0b
)

// String returns the specification name of the code, or a hex rendering for a
// code this package does not name.
func (c ExceptionCode) String() string {
	switch c {
	case ExceptionIllegalFunction:
		return "illegal function"
	case ExceptionIllegalDataAddress:
		return "illegal data address"
	case ExceptionIllegalDataValue:
		return "illegal data value"
	case ExceptionServerDeviceFailure:
		return "server device failure"
	case ExceptionAcknowledge:
		return "acknowledge"
	case ExceptionServerDeviceBusy:
		return "server device busy"
	case ExceptionMemoryParityError:
		return "memory parity error"
	case ExceptionGatewayPathUnavailable:
		return "gateway path unavailable"
	case ExceptionGatewayTargetFailedToRespond:
		return "gateway target failed to respond"
	default:
		return fmt.Sprintf("unknown exception code %#02x", byte(c))
	}
}

// Exception is a Modbus exception response: the function code of the request
// with the high bit set, followed by an exception code.
//
// Decoding returns *Exception as the Message with a nil error, because a
// well-formed exception response is a valid reply rather than a decode failure.
// Exception implements error so a client can surface it directly; errors.As
// recovers the typed value and errors.Is(err, ErrException) matches any code.
// Mapping a code to a device or protocol error stays in the client library.
type Exception struct {
	// FunctionCode is the function code of the request that failed, with the
	// exception bit clear.
	FunctionCode byte
	// Code is the exception code.
	Code ExceptionCode
}

// Function reports the wire function code, which is FunctionCode with the
// exception bit set.
func (m Exception) Function() byte { return m.FunctionCode | 0x80 }

// PDU encodes the exception as the exception function code and its code.
func (m Exception) PDU() ([]byte, error) {
	if IsException(m.FunctionCode) {
		return nil, malformed("exception function code %#02x already has the exception bit set", m.FunctionCode)
	}
	return []byte{m.Function(), byte(m.Code)}, nil
}

// Error implements error.
func (m Exception) Error() string {
	return fmt.Sprintf("modbus: function %#02x exception: %s", m.FunctionCode, m.Code)
}

// Unwrap reports ErrException, so errors.Is(err, ErrException) holds for every
// exception code.
func (m Exception) Unwrap() error { return ErrException }
