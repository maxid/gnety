package modbus

// Message is one typed Modbus PDU: a function code and its data field.
//
// The request and response types in this package implement Message. EncodePDU
// and TypedFrame accept a value or a pointer; DecodePDU and DecodeResponse
// return pointers.
//
// A PDU carries no transaction identifier, unit identifier or direction. The
// MBAP layer in Frame owns the first two; direction is supplied by the caller,
// which is what DecodeResponse does when it is given the request a response
// answers.
type Message interface {
	// Function reports the function code of the PDU: FuncReadCoils and friends,
	// or FuncReadCoils|0x80 for an exception response.
	Function() byte
	// PDU returns the complete PDU bytes: the function code followed by the data
	// field. PDU validates the message against the function code's rules and
	// returns a classified codec/wire error when a field is out of range.
	PDU() ([]byte, error)
}

// Quantity limits from the Modbus application protocol specification. A
// starting address is a uint16, so it is always in 0..0xFFFF and needs no
// separate check.
const (
	// MaxReadBits is the largest quantity one read-coils or read-discrete-inputs
	// request may ask for (function codes 0x01 and 0x02).
	MaxReadBits = 2000
	// MaxReadRegisters is the largest quantity one read-holding-registers or
	// read-input-registers request may ask for (function codes 0x03 and 0x04).
	MaxReadRegisters = 125
	// MaxWriteBits is the largest number of coils one write-multiple-coils
	// request may carry (function code 0x0F).
	MaxWriteBits = 1968
	// MaxWriteRegisters is the largest number of registers one
	// write-multiple-registers request may carry (function code 0x10).
	MaxWriteRegisters = 123
)

// ReadCoilsRequest is a 0x01 request: read the on/off state of a range of
// coils.
type ReadCoilsRequest struct {
	// Address is the first coil, 0 to 0xFFFF.
	Address uint16
	// Quantity is the number of coils to read, 1 to MaxReadBits.
	Quantity uint16
}

// Function reports function code 0x01.
func (m ReadCoilsRequest) Function() byte { return FuncReadCoils }

// PDU encodes the request as five bytes: function code, address and quantity.
func (m ReadCoilsRequest) PDU() ([]byte, error) {
	return encodeReadRequest(FuncReadCoils, "read coils", m.Address, m.Quantity, MaxReadBits)
}

// responseSpec reports the response a read-coils request expects.
func (m ReadCoilsRequest) responseSpec() (requestSpec, error) {
	if err := checkQuantity("read coils", m.Quantity, MaxReadBits); err != nil {
		return requestSpec{}, err
	}
	return requestSpec{function: FuncReadCoils, kind: expectBits, quantity: m.Quantity}, nil
}

// ReadCoilsResponse is a 0x01 response: one bit per coil in request order.
//
// DecodeResponse trims Bits to the quantity the request asked for, so the
// padding bits of the final data byte never appear. DecodePDU does not decode
// responses; use DecodeResponse for them.
type ReadCoilsResponse struct {
	// Bits holds the coil states; Bits[0] is the coil at the request address.
	Bits []bool
}

// Function reports function code 0x01.
func (m ReadCoilsResponse) Function() byte { return FuncReadCoils }

// PDU encodes the response as a function code, a byte count and the packed
// bits, least significant bit of the first data byte first.
func (m ReadCoilsResponse) PDU() ([]byte, error) {
	return encodeBitsResponse(FuncReadCoils, "read coils response", m.Bits, MaxReadBits)
}

// ReadDiscreteInputsRequest is a 0x02 request: read the on/off state of a range
// of discrete inputs.
type ReadDiscreteInputsRequest struct {
	// Address is the first input, 0 to 0xFFFF.
	Address uint16
	// Quantity is the number of inputs to read, 1 to MaxReadBits.
	Quantity uint16
}

// Function reports function code 0x02.
func (m ReadDiscreteInputsRequest) Function() byte { return FuncReadDiscreteInputs }

// PDU encodes the request as five bytes: function code, address and quantity.
func (m ReadDiscreteInputsRequest) PDU() ([]byte, error) {
	return encodeReadRequest(FuncReadDiscreteInputs, "read discrete inputs", m.Address, m.Quantity, MaxReadBits)
}

// responseSpec reports the response a read-discrete-inputs request expects.
func (m ReadDiscreteInputsRequest) responseSpec() (requestSpec, error) {
	if err := checkQuantity("read discrete inputs", m.Quantity, MaxReadBits); err != nil {
		return requestSpec{}, err
	}
	return requestSpec{function: FuncReadDiscreteInputs, kind: expectBits, quantity: m.Quantity}, nil
}

// ReadDiscreteInputsResponse is a 0x02 response: one bit per input in request
// order.
//
// DecodeResponse trims Bits to the quantity the request asked for, so the
// padding bits of the final data byte never appear.
type ReadDiscreteInputsResponse struct {
	// Bits holds the input states; Bits[0] is the input at the request address.
	Bits []bool
}

// Function reports function code 0x02.
func (m ReadDiscreteInputsResponse) Function() byte { return FuncReadDiscreteInputs }

// PDU encodes the response as a function code, a byte count and the packed
// bits, least significant bit of the first data byte first.
func (m ReadDiscreteInputsResponse) PDU() ([]byte, error) {
	return encodeBitsResponse(FuncReadDiscreteInputs, "read discrete inputs response", m.Bits, MaxReadBits)
}

// ReadHoldingRegistersRequest is a 0x03 request: read a range of holding
// registers.
type ReadHoldingRegistersRequest struct {
	// Address is the first register, 0 to 0xFFFF.
	Address uint16
	// Quantity is the number of registers to read, 1 to MaxReadRegisters.
	Quantity uint16
}

// Function reports function code 0x03.
func (m ReadHoldingRegistersRequest) Function() byte { return FuncReadHoldingRegisters }

// PDU encodes the request as five bytes: function code, address and quantity.
func (m ReadHoldingRegistersRequest) PDU() ([]byte, error) {
	return encodeReadRequest(FuncReadHoldingRegisters, "read holding registers", m.Address, m.Quantity, MaxReadRegisters)
}

// responseSpec reports the response a read-holding-registers request expects.
func (m ReadHoldingRegistersRequest) responseSpec() (requestSpec, error) {
	if err := checkQuantity("read holding registers", m.Quantity, MaxReadRegisters); err != nil {
		return requestSpec{}, err
	}
	return requestSpec{function: FuncReadHoldingRegisters, kind: expectRegisters, quantity: m.Quantity}, nil
}

// ReadHoldingRegistersResponse is a 0x03 response: one 16-bit register per
// requested address, most significant byte first.
type ReadHoldingRegistersResponse struct {
	// Registers holds the register values; Registers[0] is the register at the
	// request address.
	Registers []uint16
}

// Function reports function code 0x03.
func (m ReadHoldingRegistersResponse) Function() byte { return FuncReadHoldingRegisters }

// PDU encodes the response as a function code, a byte count of twice the
// register count and the big-endian register values.
func (m ReadHoldingRegistersResponse) PDU() ([]byte, error) {
	return encodeRegistersResponse(FuncReadHoldingRegisters, "read holding registers response", m.Registers, MaxReadRegisters)
}

// ReadInputRegistersRequest is a 0x04 request: read a range of input
// registers.
type ReadInputRegistersRequest struct {
	// Address is the first register, 0 to 0xFFFF.
	Address uint16
	// Quantity is the number of registers to read, 1 to MaxReadRegisters.
	Quantity uint16
}

// Function reports function code 0x04.
func (m ReadInputRegistersRequest) Function() byte { return FuncReadInputRegisters }

// PDU encodes the request as five bytes: function code, address and quantity.
func (m ReadInputRegistersRequest) PDU() ([]byte, error) {
	return encodeReadRequest(FuncReadInputRegisters, "read input registers", m.Address, m.Quantity, MaxReadRegisters)
}

// responseSpec reports the response a read-input-registers request expects.
func (m ReadInputRegistersRequest) responseSpec() (requestSpec, error) {
	if err := checkQuantity("read input registers", m.Quantity, MaxReadRegisters); err != nil {
		return requestSpec{}, err
	}
	return requestSpec{function: FuncReadInputRegisters, kind: expectRegisters, quantity: m.Quantity}, nil
}

// ReadInputRegistersResponse is a 0x04 response: one 16-bit register per
// requested address, most significant byte first.
type ReadInputRegistersResponse struct {
	// Registers holds the register values; Registers[0] is the register at the
	// request address.
	Registers []uint16
}

// Function reports function code 0x04.
func (m ReadInputRegistersResponse) Function() byte { return FuncReadInputRegisters }

// PDU encodes the response as a function code, a byte count of twice the
// register count and the big-endian register values.
func (m ReadInputRegistersResponse) PDU() ([]byte, error) {
	return encodeRegistersResponse(FuncReadInputRegisters, "read input registers response", m.Registers, MaxReadRegisters)
}

// WriteSingleCoil is a 0x05 request and its response: write one coil. A server
// echoes the request unchanged.
type WriteSingleCoil struct {
	// Address is the coil, 0 to 0xFFFF.
	Address uint16
	// Value is the requested state: true is on (0xFF00), false is off (0x0000).
	Value bool
}

// Function reports function code 0x05.
func (m WriteSingleCoil) Function() byte { return FuncWriteSingleCoil }

// PDU encodes the request as five bytes: function code, address and the
// 0xFF00 or 0x0000 state.
func (m WriteSingleCoil) PDU() ([]byte, error) {
	return encodeWriteSingleCoil(m.Address, m.Value), nil
}

// responseSpec reports the echo a write-single-coil request expects.
func (m WriteSingleCoil) responseSpec() (requestSpec, error) {
	return requestSpec{
		function: FuncWriteSingleCoil,
		kind:     expectEcho,
		address:  m.Address,
		value:    coilWire(m.Value),
	}, nil
}

// WriteSingleRegister is a 0x06 request and its response: write one holding
// register. A server echoes the request unchanged.
type WriteSingleRegister struct {
	// Address is the register, 0 to 0xFFFF.
	Address uint16
	// Value is the register value, 0 to 0xFFFF.
	Value uint16
}

// Function reports function code 0x06.
func (m WriteSingleRegister) Function() byte { return FuncWriteSingleRegister }

// PDU encodes the request as five bytes: function code, address and the
// big-endian value.
func (m WriteSingleRegister) PDU() ([]byte, error) {
	return encodeWriteSingleRegister(m.Address, m.Value), nil
}

// responseSpec reports the echo a write-single-register request expects.
func (m WriteSingleRegister) responseSpec() (requestSpec, error) {
	return requestSpec{
		function: FuncWriteSingleRegister,
		kind:     expectEcho,
		address:  m.Address,
		value:    m.Value,
	}, nil
}

// WriteMultipleCoilsRequest is a 0x0F request: write a range of coils.
type WriteMultipleCoilsRequest struct {
	// Address is the first coil, 0 to 0xFFFF.
	Address uint16
	// Values holds one state per coil, 1 to MaxWriteBits entries. The encoder
	// packs Values[0] into the least significant bit of the first data byte and
	// zeroes the padding bits of the final byte; the decoder ignores them.
	Values []bool
}

// Function reports function code 0x0F.
func (m WriteMultipleCoilsRequest) Function() byte { return FuncWriteMultipleCoils }

// PDU encodes the request as function code, address, quantity, byte count and
// the packed coil states.
func (m WriteMultipleCoilsRequest) PDU() ([]byte, error) {
	return encodeWriteMultipleCoils(m.Address, m.Values)
}

// responseSpec reports the acknowledgement a write-multiple-coils request
// expects.
func (m WriteMultipleCoilsRequest) responseSpec() (requestSpec, error) {
	if len(m.Values) == 0 || len(m.Values) > MaxWriteBits {
		return requestSpec{}, malformed("write multiple coils carries %d values, want 1..%d", len(m.Values), MaxWriteBits)
	}
	return requestSpec{
		function: FuncWriteMultipleCoils,
		kind:     expectWriteAck,
		address:  m.Address,
		quantity: uint16(len(m.Values)),
	}, nil
}

// WriteMultipleCoilsResponse is a 0x0F response: the server reports the range
// it wrote.
type WriteMultipleCoilsResponse struct {
	// Address is the first coil of the written range, 0 to 0xFFFF.
	Address uint16
	// Quantity is the number of coils written, 1 to MaxWriteBits.
	Quantity uint16
}

// Function reports function code 0x0F.
func (m WriteMultipleCoilsResponse) Function() byte { return FuncWriteMultipleCoils }

// PDU encodes the response as five bytes: function code, address and quantity.
func (m WriteMultipleCoilsResponse) PDU() ([]byte, error) {
	return encodeWriteMultipleAck(FuncWriteMultipleCoils, "write multiple coils response", m.Address, m.Quantity, MaxWriteBits)
}

// WriteMultipleRegistersRequest is a 0x10 request: write a range of holding
// registers.
type WriteMultipleRegistersRequest struct {
	// Address is the first register, 0 to 0xFFFF.
	Address uint16
	// Values holds one 16-bit value per register, 1 to MaxWriteRegisters
	// entries.
	Values []uint16
}

// Function reports function code 0x10.
func (m WriteMultipleRegistersRequest) Function() byte { return FuncWriteMultipleRegisters }

// PDU encodes the request as function code, address, quantity, byte count and
// the big-endian register values.
func (m WriteMultipleRegistersRequest) PDU() ([]byte, error) {
	return encodeWriteMultipleRegisters(m.Address, m.Values)
}

// responseSpec reports the acknowledgement a write-multiple-registers request
// expects.
func (m WriteMultipleRegistersRequest) responseSpec() (requestSpec, error) {
	if len(m.Values) == 0 || len(m.Values) > MaxWriteRegisters {
		return requestSpec{}, malformed("write multiple registers carries %d values, want 1..%d", len(m.Values), MaxWriteRegisters)
	}
	return requestSpec{
		function: FuncWriteMultipleRegisters,
		kind:     expectWriteAck,
		address:  m.Address,
		quantity: uint16(len(m.Values)),
	}, nil
}

// WriteMultipleRegistersResponse is a 0x10 response: the server reports the
// range it wrote.
type WriteMultipleRegistersResponse struct {
	// Address is the first register of the written range, 0 to 0xFFFF.
	Address uint16
	// Quantity is the number of registers written, 1 to MaxWriteRegisters.
	Quantity uint16
}

// Function reports function code 0x10.
func (m WriteMultipleRegistersResponse) Function() byte { return FuncWriteMultipleRegisters }

// PDU encodes the response as five bytes: function code, address and quantity.
func (m WriteMultipleRegistersResponse) PDU() ([]byte, error) {
	return encodeWriteMultipleAck(FuncWriteMultipleRegisters, "write multiple registers response", m.Address, m.Quantity, MaxWriteRegisters)
}
