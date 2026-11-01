package modbus

import (
	"encoding/binary"
	"fmt"
	"io"
	"reflect"

	"github.com/maxid/gnety/codec/wire"
)

// malformed reports a PDU that violates the wire format. It carries
// wire.KindMalformed so codec/wire classifies it across packages.
func malformed(format string, args ...any) error {
	return wire.Errorf(wire.KindMalformed, "modbus: %s", fmt.Sprintf(format, args...))
}

// tooLarge reports a PDU that exceeds a size limit.
func tooLarge(format string, args ...any) error {
	return wire.Errorf(wire.KindTooLarge, "modbus: %s", fmt.Sprintf(format, args...))
}

// unsupported reports a function code or message this package does not model.
func unsupported(format string, args ...any) error {
	return wire.Errorf(wire.KindUnsupported, "modbus: %s", fmt.Sprintf(format, args...))
}

// maxBitReadBytes is the largest byte count a bit-oriented read response can
// carry: MaxReadBits packed one per bit.
const maxBitReadBytes = (MaxReadBits + 7) / 8

// EncodePDU writes the encoded PDU of m to w.
//
// EncodePDU validates through Message.PDU and writes the PDU with a single
// Write call. A nil writer, a nil or typed-nil message, an empty PDU and a PDU
// whose first byte disagrees with Message.Function report wire.KindMalformed; a
// PDU longer than MaxPDU reports wire.KindTooLarge. Errors from Message.PDU are
// returned unchanged.
func EncodePDU(w io.Writer, m Message) error {
	if w == nil {
		return malformed("nil writer")
	}
	if isNilMessage(m) {
		return malformed("nil message")
	}
	pdu, err := m.PDU()
	if err != nil {
		return err
	}
	if len(pdu) == 0 {
		return malformed("%T encoded an empty PDU", m)
	}
	if len(pdu) > MaxPDU {
		return tooLarge("%T PDU of %d bytes exceeds %d", m, len(pdu), MaxPDU)
	}
	if got, want := pdu[0], m.Function(); got != want {
		return malformed("%T reports function %#02x but encodes %#02x", m, want, got)
	}
	n, err := w.Write(pdu)
	if err != nil {
		return err
	}
	if n != len(pdu) {
		return io.ErrShortWrite
	}
	return nil
}

// isNilMessage reports whether m is nil or holds a typed nil pointer.
func isNilMessage(m Message) bool {
	if m == nil {
		return true
	}
	rv := reflect.ValueOf(m)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// checkQuantity enforces a quantity range of 1..max.
func checkQuantity(name string, quantity uint16, max int) error {
	if quantity == 0 || int(quantity) > max {
		return malformed("%s quantity %d is outside 1..%d", name, quantity, max)
	}
	return nil
}

// encodeReadRequest builds the five-byte request shared by function codes 0x01
// to 0x04: function code, starting address and quantity.
func encodeReadRequest(function byte, name string, address, quantity uint16, max int) ([]byte, error) {
	if err := checkQuantity(name, quantity, max); err != nil {
		return nil, err
	}
	w := wire.NewWriter(5)
	w.Byte(function)
	w.Uint16(binary.BigEndian, address)
	w.Uint16(binary.BigEndian, quantity)
	return w.Bytes(), nil
}

// encodeBitsResponse builds a bit-oriented read response: function code, byte
// count and the packed bits.
func encodeBitsResponse(function byte, name string, bits []bool, max int) ([]byte, error) {
	if len(bits) == 0 || len(bits) > max {
		return nil, malformed("%s carries %d bits, want 1..%d", name, len(bits), max)
	}
	w := wire.NewWriter(2 + (len(bits)+7)/8)
	w.Byte(function)
	w.Byte(byte((len(bits) + 7) / 8))
	w.Raw(packBits(bits))
	return w.Bytes(), nil
}

// encodeRegistersResponse builds a register-oriented read response: function
// code, byte count and the big-endian registers.
func encodeRegistersResponse(function byte, name string, registers []uint16, max int) ([]byte, error) {
	if len(registers) == 0 || len(registers) > max {
		return nil, malformed("%s carries %d registers, want 1..%d", name, len(registers), max)
	}
	w := wire.NewWriter(2 + 2*len(registers))
	w.Byte(function)
	w.Byte(byte(2 * len(registers)))
	for _, v := range registers {
		w.Uint16(binary.BigEndian, v)
	}
	return w.Bytes(), nil
}

// encodeWriteSingleCoil builds the 0x05 request: function code, address and the
// 0xFF00 or 0x0000 state.
func encodeWriteSingleCoil(address uint16, value bool) []byte {
	w := wire.NewWriter(5)
	w.Byte(FuncWriteSingleCoil)
	w.Uint16(binary.BigEndian, address)
	w.Uint16(binary.BigEndian, coilWire(value))
	return w.Bytes()
}

// encodeWriteSingleRegister builds the 0x06 request: function code, address and
// the big-endian value.
func encodeWriteSingleRegister(address, value uint16) []byte {
	w := wire.NewWriter(5)
	w.Byte(FuncWriteSingleRegister)
	w.Uint16(binary.BigEndian, address)
	w.Uint16(binary.BigEndian, value)
	return w.Bytes()
}

// encodeWriteMultipleCoils builds the 0x0F request: function code, address,
// quantity, byte count and the packed coil states.
func encodeWriteMultipleCoils(address uint16, values []bool) ([]byte, error) {
	if len(values) == 0 || len(values) > MaxWriteBits {
		return nil, malformed("write multiple coils carries %d values, want 1..%d", len(values), MaxWriteBits)
	}
	w := wire.NewWriter(6 + (len(values)+7)/8)
	w.Byte(FuncWriteMultipleCoils)
	w.Uint16(binary.BigEndian, address)
	w.Uint16(binary.BigEndian, uint16(len(values)))
	w.Byte(byte((len(values) + 7) / 8))
	w.Raw(packBits(values))
	return w.Bytes(), nil
}

// encodeWriteMultipleRegisters builds the 0x10 request: function code, address,
// quantity, byte count and the big-endian register values.
func encodeWriteMultipleRegisters(address uint16, values []uint16) ([]byte, error) {
	if len(values) == 0 || len(values) > MaxWriteRegisters {
		return nil, malformed("write multiple registers carries %d values, want 1..%d", len(values), MaxWriteRegisters)
	}
	w := wire.NewWriter(6 + 2*len(values))
	w.Byte(FuncWriteMultipleRegisters)
	w.Uint16(binary.BigEndian, address)
	w.Uint16(binary.BigEndian, uint16(len(values)))
	w.Byte(byte(2 * len(values)))
	for _, v := range values {
		w.Uint16(binary.BigEndian, v)
	}
	return w.Bytes(), nil
}

// encodeWriteMultipleAck builds the 0x0F or 0x10 response: function code,
// address and the quantity the server wrote.
func encodeWriteMultipleAck(function byte, name string, address, quantity uint16, max int) ([]byte, error) {
	if err := checkQuantity(name, quantity, max); err != nil {
		return nil, err
	}
	w := wire.NewWriter(5)
	w.Byte(function)
	w.Uint16(binary.BigEndian, address)
	w.Uint16(binary.BigEndian, quantity)
	return w.Bytes(), nil
}

// packBits packs bits in the bit-oriented wire order: the first bit is the
// least significant bit of the first byte and the padding bits of the final
// byte are zero.
func packBits(bits []bool) []byte {
	out := make([]byte, (len(bits)+7)/8)
	for i, bit := range bits {
		if bit {
			out[i/8] |= 1 << (i % 8)
		}
	}
	return out
}

// coilWire maps a coil state to its wire value.
func coilWire(value bool) uint16 {
	if value {
		return 0xff00
	}
	return 0x0000
}

// encodeMaskWrite builds the 0x16 request and its echo response: function code,
// address and the two masks.
func encodeMaskWrite(address, andMask, orMask uint16) ([]byte, error) {
	w := wire.NewWriter(7)
	w.Byte(FuncMaskWriteRegister)
	w.Uint16(binary.BigEndian, address)
	w.Uint16(binary.BigEndian, andMask)
	w.Uint16(binary.BigEndian, orMask)
	return w.Bytes(), nil
}

// encodeReadWriteMultipleRegisters builds the 0x17 request: function code, the
// read range, the write range, the write byte count and the values.
func encodeReadWriteMultipleRegisters(m ReadWriteMultipleRegistersRequest) ([]byte, error) {
	if err := checkQuantity("read/write multiple registers read", m.ReadQuantity, MaxReadRegisters); err != nil {
		return nil, err
	}
	if err := checkQuantity("read/write multiple registers write", uint16(len(m.WriteValues)), MaxWriteReadRegisters); err != nil {
		return nil, err
	}
	w := wire.NewWriter(10 + 2*len(m.WriteValues))
	w.Byte(FuncReadWriteMultipleRegisters)
	w.Uint16(binary.BigEndian, m.ReadAddress)
	w.Uint16(binary.BigEndian, m.ReadQuantity)
	w.Uint16(binary.BigEndian, m.WriteAddress)
	w.Uint16(binary.BigEndian, uint16(len(m.WriteValues)))
	w.Byte(byte(2 * len(m.WriteValues)))
	for _, v := range m.WriteValues {
		w.Uint16(binary.BigEndian, v)
	}
	return w.Bytes(), nil
}

// checkDeviceIDReadCode enforces the four read-device-identification codes.
func checkDeviceIDReadCode(code byte) error {
	switch code {
	case DeviceIDBasic, DeviceIDRegular, DeviceIDExtended, DeviceIDIndividual:
		return nil
	default:
		return malformed("read device identification code %#02x is outside 0x01..0x04", code)
	}
}

// validDeviceIDConformity reports whether level is one of the conformity values
// the specification defines, with or without individual access.
func validDeviceIDConformity(level byte) bool {
	switch level {
	case 0x01, 0x02, 0x03, 0x81, 0x82, 0x83:
		return true
	default:
		return false
	}
}

// encodeDeviceIDRequest builds the 0x2B/0x0E request: function code, MEI type,
// read code and object id.
func encodeDeviceIDRequest(readCode, objectID byte) ([]byte, error) {
	if err := checkDeviceIDReadCode(readCode); err != nil {
		return nil, err
	}
	w := wire.NewWriter(5)
	w.Byte(FuncEncapsulatedInterface)
	w.Byte(MEIReadDeviceIdentification)
	w.Byte(readCode)
	w.Byte(objectID)
	return w.Bytes(), nil
}

// encodeDeviceIDResponse builds the 0x2B/0x0E response: function code, MEI type,
// read code, conformity level, more-follows, next object id, object count and
// the objects.
func encodeDeviceIDResponse(m ReadDeviceIdentificationResponse) ([]byte, error) {
	if err := checkDeviceIDReadCode(m.ReadCode); err != nil {
		return nil, err
	}
	if !validDeviceIDConformity(m.ConformityLevel) {
		return nil, malformed("read device identification conformity level %#02x is outside 0x01..0x83", m.ConformityLevel)
	}
	if len(m.Objects) > 0xff {
		return nil, tooLarge("read device identification carries %d objects, want at most 255", len(m.Objects))
	}
	w := wire.NewWriter(7 + 2*len(m.Objects))
	w.Byte(FuncEncapsulatedInterface)
	w.Byte(MEIReadDeviceIdentification)
	w.Byte(m.ReadCode)
	w.Byte(m.ConformityLevel)
	if m.MoreFollows {
		w.Byte(0xff)
	} else {
		w.Byte(0x00)
	}
	w.Byte(m.NextObjectID)
	w.Byte(byte(len(m.Objects)))
	for _, object := range m.Objects {
		if len(object.Value) > 0xff {
			return nil, tooLarge("read device identification object %#02x carries %d bytes, want at most 255", object.ID, len(object.Value))
		}
		w.Byte(object.ID)
		w.Byte(byte(len(object.Value)))
		w.Raw(object.Value)
	}
	if w.Len() > MaxPDU {
		return nil, tooLarge("read device identification PDU of %d bytes exceeds %d", w.Len(), MaxPDU)
	}
	return w.Bytes(), nil
}
