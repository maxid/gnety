// Package modbus implements the Modbus TCP wire format: the MBAP header and the
// PDU bytes that follow it.
//
// It converts bytes to frames and frames to bytes only. Transaction matching,
// polling schedules, retries, exception-to-error mapping and device semantics
// belong to the Modbus client library (gmodbus) or to the adapter built on top —
// never here.
//
// The frame layout is fixed by the specification:
//
//	transaction id (2) | protocol id (2, always 0) | length (2) | unit id (1) | PDU
//
// where length counts the unit id plus the PDU, so a well-formed frame carries
// length in [2, 254] and a PDU of at most 253 bytes.
//
// Codec satisfies gnety's stream Codec contract (Decode/Encode) structurally.
package modbus

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/maxid/gnety/codec/wire"
)

const (
	// MBAPLen is the header length including the unit identifier.
	MBAPLen = 7
	// ProtocolID is the only valid MBAP protocol identifier.
	ProtocolID uint16 = 0
	// MaxPDU is the largest PDU the specification allows.
	MaxPDU = 253
	// minLength is the smallest legal MBAP length field: unit id plus one PDU
	// byte (the function code).
	minLength = 2
)

// Modbus function codes. Only the codes this format layer recognizes are
// listed; their semantics live in the client library.
const (
	FuncReadCoils              byte = 0x01
	FuncReadDiscreteInputs     byte = 0x02
	FuncReadHoldingRegisters   byte = 0x03
	FuncReadInputRegisters     byte = 0x04
	FuncWriteSingleCoil        byte = 0x05
	FuncWriteSingleRegister    byte = 0x06
	FuncReadExceptionStatus    byte = 0x07
	FuncWriteMultipleCoils     byte = 0x0f
	FuncWriteMultipleRegisters byte = 0x10
	FuncEncapsulatedInterface  byte = 0x2b
)

// IsException reports whether the function code marks an exception response
// (the high bit set, for example 0x83 for function 0x03).
func IsException(function byte) bool { return function&0x80 != 0 }

// Frame is one Modbus TCP application data unit.
type Frame struct {
	TransactionID uint16
	UnitID        uint8
	PDU           []byte
}

// Function returns the PDU function code, or 0 for an empty PDU.
func (f Frame) Function() byte {
	if len(f.PDU) == 0 {
		return 0
	}
	return f.PDU[0]
}

// Marshal encodes the frame as one MBAP header plus PDU.
func (f Frame) Marshal() ([]byte, error) {
	if len(f.PDU) == 0 {
		return nil, wire.Errorf(wire.KindMalformed, "modbus: empty PDU")
	}
	if len(f.PDU) > MaxPDU {
		return nil, wire.Errorf(wire.KindTooLarge, "modbus: PDU of %d bytes exceeds %d", len(f.PDU), MaxPDU)
	}
	w := wire.NewWriter(MBAPLen + len(f.PDU))
	w.Uint16(binary.BigEndian, f.TransactionID)
	w.Uint16(binary.BigEndian, ProtocolID)
	w.Uint16(binary.BigEndian, uint16(1+len(f.PDU)))
	w.Byte(f.UnitID)
	w.Raw(f.PDU)
	return w.Bytes(), nil
}

// Read reads one frame. A maxPDU <= 0 means MaxPDU.
//
// The declared length is validated before the PDU is allocated, so a peer
// cannot make the reader reserve more than maxPDU bytes. A truncated frame
// reports wire.ErrIncomplete and leaves the reader positioned mid-frame: a
// stream decoder should stop for the connection, not for the process.
func Read(r *bufio.Reader, maxPDU int) (Frame, error) {
	if maxPDU <= 0 {
		maxPDU = MaxPDU
	}
	rd := wire.NewReader(r, MBAPLen+maxPDU)

	txID, err := rd.Uint16(binary.BigEndian)
	if err != nil {
		return Frame{}, err
	}
	protoID, err := rd.Uint16(binary.BigEndian)
	if err != nil {
		return Frame{}, err
	}
	if protoID != ProtocolID {
		return Frame{}, wire.Errorf(wire.KindMalformed, "modbus: protocol id %d, want %d", protoID, ProtocolID)
	}
	length, err := rd.Uint16(binary.BigEndian)
	if err != nil {
		return Frame{}, err
	}
	if length < minLength {
		return Frame{}, wire.Errorf(wire.KindMalformed, "modbus: length %d is below the minimum %d", length, minLength)
	}
	if int(length)-1 > maxPDU {
		return Frame{}, wire.Errorf(wire.KindTooLarge, "modbus: PDU of %d bytes exceeds %d", int(length)-1, maxPDU)
	}
	unitID, err := rd.Byte()
	if err != nil {
		return Frame{}, err
	}
	pdu, err := rd.ReadN(int(length) - 1)
	if err != nil {
		return Frame{}, err
	}
	return Frame{TransactionID: txID, UnitID: unitID, PDU: pdu}, nil
}

// Codec adapts the Modbus frame format to gnety's stream codec interface.
type Codec struct {
	// MaxPDU bounds an inbound PDU. A non-positive value means MaxPDU.
	MaxPDU int
}

func (c Codec) max() int {
	if c.MaxPDU > 0 {
		return c.MaxPDU
	}
	return MaxPDU
}

// Decode reads one frame.
func (c Codec) Decode(r *bufio.Reader) (any, error) { return Read(r, c.max()) }

// Encode writes one frame.
func (c Codec) Encode(w io.Writer, msg any) error {
	var f Frame
	switch v := msg.(type) {
	case Frame:
		f = v
	case *Frame:
		if v == nil {
			return wire.Errorf(wire.KindMalformed, "modbus: nil frame")
		}
		f = *v
	default:
		return wire.Errorf(wire.KindUnsupported, "modbus: codec expects modbus.Frame, got %T", msg)
	}
	if len(f.PDU) > c.max() {
		return wire.Errorf(wire.KindTooLarge, "modbus: PDU of %d bytes exceeds %d", len(f.PDU), c.max())
	}
	buf, err := f.Marshal()
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

// String implements fmt.Stringer for logs that must not leak the payload.
func (f Frame) String() string {
	return fmt.Sprintf("modbus.Frame{tx=%d unit=%d pdu=%d bytes}", f.TransactionID, f.UnitID, len(f.PDU))
}
