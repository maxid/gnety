package modbus

import "encoding/binary"

// DecodePDU decodes one complete PDU in request orientation.
//
// pdu is the bytes after the MBAP header. The MBAP length field has already
// delimited the PDU, so a PDU that ends inside a field is malformed rather than
// incomplete.
//
// A PDU carries no direction flag. Read and write requests have their own
// layouts and are decoded here. A response to a read is decoded by
// DecodeResponse, which knows the quantity the request asked for and therefore
// where the data ends; decoding such a response here would misread its byte
// count as part of the address. Write-single requests and their responses share
// one layout and decode to the same type. Exception responses are decoded both
// here and in DecodeResponse.
//
// Errors are classified through codec/wire: an unknown function code reports
// wire.KindUnsupported, a truncated or contradictory PDU reports
// wire.KindMalformed, and a PDU longer than MaxPDU reports wire.KindTooLarge.
func DecodePDU(pdu []byte) (Message, error) {
	if len(pdu) == 0 {
		return nil, malformed("empty PDU")
	}
	if len(pdu) > MaxPDU {
		return nil, tooLarge("PDU of %d bytes exceeds %d", len(pdu), MaxPDU)
	}
	function := pdu[0]
	if IsException(function) {
		return decodeException(pdu)
	}
	switch function {
	case FuncReadCoils:
		address, quantity, err := decodeReadRequest(pdu, "read coils", MaxReadBits)
		if err != nil {
			return nil, err
		}
		return &ReadCoilsRequest{Address: address, Quantity: quantity}, nil
	case FuncReadDiscreteInputs:
		address, quantity, err := decodeReadRequest(pdu, "read discrete inputs", MaxReadBits)
		if err != nil {
			return nil, err
		}
		return &ReadDiscreteInputsRequest{Address: address, Quantity: quantity}, nil
	case FuncReadHoldingRegisters:
		address, quantity, err := decodeReadRequest(pdu, "read holding registers", MaxReadRegisters)
		if err != nil {
			return nil, err
		}
		return &ReadHoldingRegistersRequest{Address: address, Quantity: quantity}, nil
	case FuncReadInputRegisters:
		address, quantity, err := decodeReadRequest(pdu, "read input registers", MaxReadRegisters)
		if err != nil {
			return nil, err
		}
		return &ReadInputRegistersRequest{Address: address, Quantity: quantity}, nil
	case FuncWriteSingleCoil:
		return decodeWriteSingleCoil(pdu)
	case FuncWriteSingleRegister:
		return decodeWriteSingleRegister(pdu)
	case FuncWriteMultipleCoils:
		return decodeWriteMultipleCoilsRequest(pdu)
	case FuncWriteMultipleRegisters:
		return decodeWriteMultipleRegistersRequest(pdu)
	case FuncMaskWriteRegister:
		return decodeMaskWrite(pdu)
	case FuncReadWriteMultipleRegisters:
		return decodeReadWriteMultipleRegistersRequest(pdu)
	case FuncEncapsulatedInterface:
		return decodeDeviceIDRequest(pdu)
	default:
		return nil, unsupported("function code %#02x", function)
	}
}

// DecodeResponse decodes one complete PDU as the response to req.
//
// DecodeResponse decodes the response with the layout the requested function
// code implies and checks that it answers req: the function code must match, a
// read response must carry exactly the requested quantity (a bit-oriented
// response is trimmed to that quantity, dropping its padding bits), a
// write-single response must echo the address and value, and a write-multiple
// response must report the requested address and quantity. An exception
// response is accepted for req's function code whatever its exception code.
//
// req must be one of the request types in this package, by value or pointer. A
// response message, a nil request and a request whose own fields are invalid are
// rejected before the response is decoded. Errors carry the same codec/wire
// classification as DecodePDU.
func DecodeResponse(req Message, pdu []byte) (Message, error) {
	spec, err := requestSpecOf(req)
	if err != nil {
		return nil, err
	}
	if len(pdu) == 0 {
		return nil, malformed("empty response PDU")
	}
	if len(pdu) > MaxPDU {
		return nil, tooLarge("PDU of %d bytes exceeds %d", len(pdu), MaxPDU)
	}
	function := pdu[0]
	if IsException(function) {
		decoded, err := decodeException(pdu)
		if err != nil {
			return nil, err
		}
		exception := decoded.(*Exception)
		if exception.FunctionCode != spec.function {
			return nil, malformed("exception for function %#02x answers a %#02x request", exception.FunctionCode, spec.function)
		}
		return exception, nil
	}
	if function != spec.function {
		return nil, malformed("response function %#02x does not match the %#02x request", function, spec.function)
	}
	response, err := decodeResponsePDU(function, pdu)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(spec, response); err != nil {
		return nil, err
	}
	return response, nil
}

// responseKind tells DecodeResponse how to shape and check a response.
type responseKind uint8

const (
	// expectBits is a read response carrying one bit per requested value.
	expectBits responseKind = iota
	// expectRegisters is a read response carrying one 16-bit register per
	// requested value.
	expectRegisters
	// expectEcho is a write-single response that repeats the request.
	expectEcho
	// expectWriteAck is a write-multiple response carrying an address and a
	// quantity.
	expectWriteAck
	// expectMaskWrite is a mask-write response that repeats the request.
	expectMaskWrite
	// expectDeviceID is a read-device-identification response carrying the
	// objects of the requested read code.
	expectDeviceID
)

// request is implemented by the request message types, by value or pointer. It
// describes the response the request expects.
type request interface {
	responseSpec() (requestSpec, error)
}

// requestSpec describes the response a request message expects.
type requestSpec struct {
	function byte
	kind     responseKind
	address  uint16
	value    uint16
	quantity uint16
	andMask  uint16
	orMask   uint16
}

// requestSpecOf validates req and returns the response it expects.
func requestSpecOf(req Message) (requestSpec, error) {
	if isNilMessage(req) {
		return requestSpec{}, malformed("nil request")
	}
	typed, ok := req.(request)
	if !ok {
		return requestSpec{}, unsupported("%T is not a request", req)
	}
	return typed.responseSpec()
}

// decodeReadRequest decodes the five-byte request shared by function codes 0x01
// to 0x04 and enforces the quantity range.
func decodeReadRequest(pdu []byte, name string, max int) (uint16, uint16, error) {
	if len(pdu) != 5 {
		return 0, 0, malformed("%s request PDU of %d bytes, want 5", name, len(pdu))
	}
	address := binary.BigEndian.Uint16(pdu[1:3])
	quantity := binary.BigEndian.Uint16(pdu[3:5])
	if err := checkQuantity(name, quantity, max); err != nil {
		return 0, 0, err
	}
	return address, quantity, nil
}

// decodeWriteSingleCoil decodes the 0x05 layout shared by the request and its
// echo response.
func decodeWriteSingleCoil(pdu []byte) (Message, error) {
	if len(pdu) != 5 {
		return nil, malformed("write single coil PDU of %d bytes, want 5", len(pdu))
	}
	value := binary.BigEndian.Uint16(pdu[3:5])
	var on bool
	switch value {
	case 0xff00:
		on = true
	case 0x0000:
		on = false
	default:
		return nil, malformed("write single coil value %#04x is neither 0xFF00 nor 0x0000", value)
	}
	return &WriteSingleCoil{Address: binary.BigEndian.Uint16(pdu[1:3]), Value: on}, nil
}

// decodeWriteSingleRegister decodes the 0x06 layout shared by the request and
// its echo response.
func decodeWriteSingleRegister(pdu []byte) (Message, error) {
	if len(pdu) != 5 {
		return nil, malformed("write single register PDU of %d bytes, want 5", len(pdu))
	}
	return &WriteSingleRegister{
		Address: binary.BigEndian.Uint16(pdu[1:3]),
		Value:   binary.BigEndian.Uint16(pdu[3:5]),
	}, nil
}

// decodeWriteMultipleCoilsRequest decodes a 0x0F request.
func decodeWriteMultipleCoilsRequest(pdu []byte) (Message, error) {
	if len(pdu) < 6 {
		return nil, malformed("write multiple coils request PDU of %d bytes, want at least 6", len(pdu))
	}
	address := binary.BigEndian.Uint16(pdu[1:3])
	quantity := binary.BigEndian.Uint16(pdu[3:5])
	if err := checkQuantity("write multiple coils", quantity, MaxWriteBits); err != nil {
		return nil, err
	}
	byteCount := int(pdu[5])
	wantBytes := (int(quantity) + 7) / 8
	if byteCount != wantBytes {
		return nil, malformed("write multiple coils byte count %d, want %d for %d coils", byteCount, wantBytes, quantity)
	}
	if len(pdu) != 6+byteCount {
		return nil, malformed("write multiple coils request PDU of %d bytes, want %d", len(pdu), 6+byteCount)
	}
	return &WriteMultipleCoilsRequest{
		Address: address,
		Values:  unpackBits(pdu[6:], int(quantity)),
	}, nil
}

// decodeWriteMultipleRegistersRequest decodes a 0x10 request.
func decodeWriteMultipleRegistersRequest(pdu []byte) (Message, error) {
	if len(pdu) < 6 {
		return nil, malformed("write multiple registers request PDU of %d bytes, want at least 6", len(pdu))
	}
	address := binary.BigEndian.Uint16(pdu[1:3])
	quantity := binary.BigEndian.Uint16(pdu[3:5])
	if err := checkQuantity("write multiple registers", quantity, MaxWriteRegisters); err != nil {
		return nil, err
	}
	byteCount := int(pdu[5])
	wantBytes := 2 * int(quantity)
	if byteCount != wantBytes {
		return nil, malformed("write multiple registers byte count %d, want %d for %d registers", byteCount, wantBytes, quantity)
	}
	if len(pdu) != 6+byteCount {
		return nil, malformed("write multiple registers request PDU of %d bytes, want %d", len(pdu), 6+byteCount)
	}
	values := make([]uint16, quantity)
	for i := range values {
		values[i] = binary.BigEndian.Uint16(pdu[6+2*i : 8+2*i])
	}
	return &WriteMultipleRegistersRequest{Address: address, Values: values}, nil
}

// decodeResponsePDU decodes one response PDU for a function code whose match
// against the request has already been established.
func decodeResponsePDU(function byte, pdu []byte) (Message, error) {
	switch function {
	case FuncReadCoils:
		bits, err := decodeBitsResponse(pdu, "read coils")
		if err != nil {
			return nil, err
		}
		return &ReadCoilsResponse{Bits: bits}, nil
	case FuncReadDiscreteInputs:
		bits, err := decodeBitsResponse(pdu, "read discrete inputs")
		if err != nil {
			return nil, err
		}
		return &ReadDiscreteInputsResponse{Bits: bits}, nil
	case FuncReadHoldingRegisters:
		registers, err := decodeRegistersResponse(pdu, "read holding registers")
		if err != nil {
			return nil, err
		}
		return &ReadHoldingRegistersResponse{Registers: registers}, nil
	case FuncReadInputRegisters:
		registers, err := decodeRegistersResponse(pdu, "read input registers")
		if err != nil {
			return nil, err
		}
		return &ReadInputRegistersResponse{Registers: registers}, nil
	case FuncWriteSingleCoil:
		return decodeWriteSingleCoil(pdu)
	case FuncWriteSingleRegister:
		return decodeWriteSingleRegister(pdu)
	case FuncWriteMultipleCoils:
		return decodeWriteMultipleCoilsResponse(pdu)
	case FuncWriteMultipleRegisters:
		return decodeWriteMultipleRegistersResponse(pdu)
	case FuncMaskWriteRegister:
		return decodeMaskWrite(pdu)
	case FuncReadWriteMultipleRegisters:
		registers, err := decodeRegistersResponse(pdu, "read/write multiple registers")
		if err != nil {
			return nil, err
		}
		return &ReadWriteMultipleRegistersResponse{Registers: registers}, nil
	case FuncEncapsulatedInterface:
		return decodeDeviceIDResponse(pdu)
	default:
		return nil, unsupported("function code %#02x", function)
	}
}

// decodeBitsResponse decodes a bit-oriented read response: function code, byte
// count and the packed bits.
func decodeBitsResponse(pdu []byte, name string) ([]bool, error) {
	if len(pdu) < 2 {
		return nil, malformed("%s response PDU of %d bytes, want a function code and a byte count", name, len(pdu))
	}
	byteCount := int(pdu[1])
	if byteCount == 0 || byteCount > maxBitReadBytes {
		return nil, malformed("%s response byte count %d is outside 1..%d", name, byteCount, maxBitReadBytes)
	}
	if len(pdu) != 2+byteCount {
		return nil, malformed("%s response declares %d data bytes but the PDU carries %d", name, byteCount, len(pdu)-2)
	}
	return unpackBits(pdu[2:], 8*byteCount), nil
}

// decodeRegistersResponse decodes a register-oriented read response: function
// code, byte count and the big-endian registers.
func decodeRegistersResponse(pdu []byte, name string) ([]uint16, error) {
	if len(pdu) < 2 {
		return nil, malformed("%s response PDU of %d bytes, want a function code and a byte count", name, len(pdu))
	}
	byteCount := int(pdu[1])
	maxBytes := 2 * MaxReadRegisters
	if byteCount < 2 || byteCount > maxBytes || byteCount%2 != 0 {
		return nil, malformed("%s response byte count %d is not an even number in 2..%d", name, byteCount, maxBytes)
	}
	if len(pdu) != 2+byteCount {
		return nil, malformed("%s response declares %d data bytes but the PDU carries %d", name, byteCount, len(pdu)-2)
	}
	registers := make([]uint16, byteCount/2)
	for i := range registers {
		registers[i] = binary.BigEndian.Uint16(pdu[2+2*i : 4+2*i])
	}
	return registers, nil
}

// decodeWriteMultipleCoilsResponse decodes a 0x0F response.
func decodeWriteMultipleCoilsResponse(pdu []byte) (Message, error) {
	if len(pdu) != 5 {
		return nil, malformed("write multiple coils response PDU of %d bytes, want 5", len(pdu))
	}
	address := binary.BigEndian.Uint16(pdu[1:3])
	quantity := binary.BigEndian.Uint16(pdu[3:5])
	if err := checkQuantity("write multiple coils response", quantity, MaxWriteBits); err != nil {
		return nil, err
	}
	return &WriteMultipleCoilsResponse{Address: address, Quantity: quantity}, nil
}

// decodeWriteMultipleRegistersResponse decodes a 0x10 response.
func decodeWriteMultipleRegistersResponse(pdu []byte) (Message, error) {
	if len(pdu) != 5 {
		return nil, malformed("write multiple registers response PDU of %d bytes, want 5", len(pdu))
	}
	address := binary.BigEndian.Uint16(pdu[1:3])
	quantity := binary.BigEndian.Uint16(pdu[3:5])
	if err := checkQuantity("write multiple registers response", quantity, MaxWriteRegisters); err != nil {
		return nil, err
	}
	return &WriteMultipleRegistersResponse{Address: address, Quantity: quantity}, nil
}

// decodeException decodes a function code with the exception bit set and its
// exception code. The code is not restricted to the ones this package names, so
// an exception response is always decodable.
func decodeException(pdu []byte) (Message, error) {
	if len(pdu) != 2 {
		return nil, malformed("exception PDU of %d bytes, want 2", len(pdu))
	}
	return &Exception{FunctionCode: pdu[0] &^ 0x80, Code: ExceptionCode(pdu[1])}, nil
}

// unpackBits unpacks n bits from the bit-oriented wire order: the first bit is
// the least significant bit of the first byte.
func unpackBits(data []byte, n int) []bool {
	bits := make([]bool, n)
	for i := range bits {
		bits[i] = data[i/8]&(1<<(i%8)) != 0
	}
	return bits
}

// checkResponse applies the response size and echo expectations of a request.
func checkResponse(spec requestSpec, response Message) error {
	switch spec.kind {
	case expectBits:
		bits, ok := responseBits(response)
		if !ok {
			return unsupported("response %T does not answer a bit read", response)
		}
		wantBytes := (int(spec.quantity) + 7) / 8
		if len(bits) != 8*wantBytes {
			return malformed("response carries %d bits (byte count %d), the request asked for %d bits (byte count %d)",
				len(bits), (len(bits)+7)/8, spec.quantity, wantBytes)
		}
		trimResponseBits(response, int(spec.quantity))
		return nil
	case expectRegisters:
		registers, ok := responseRegisters(response)
		if !ok {
			return unsupported("response %T does not answer a register read", response)
		}
		if len(registers) != int(spec.quantity) {
			return malformed("response carries %d registers, the request asked for %d", len(registers), spec.quantity)
		}
		return nil
	case expectEcho:
		return checkEcho(spec, response)
	case expectWriteAck:
		return checkWriteAck(spec, response)
	case expectMaskWrite:
		return checkMaskWrite(spec, response)
	case expectDeviceID:
		return checkDeviceID(spec, response)
	default:
		return unsupported("unknown response expectation %d", spec.kind)
	}
}

// responseBits returns the bits of a bit-oriented read response.
func responseBits(m Message) ([]bool, bool) {
	switch r := m.(type) {
	case *ReadCoilsResponse:
		return r.Bits, true
	case *ReadDiscreteInputsResponse:
		return r.Bits, true
	default:
		return nil, false
	}
}

// trimResponseBits cuts a bit-oriented read response down to the requested
// quantity, dropping the padding bits of the final data byte.
func trimResponseBits(m Message, n int) {
	switch r := m.(type) {
	case *ReadCoilsResponse:
		r.Bits = r.Bits[:n]
	case *ReadDiscreteInputsResponse:
		r.Bits = r.Bits[:n]
	}
}

// responseRegisters returns the registers of a register-oriented read response.
func responseRegisters(m Message) ([]uint16, bool) {
	switch r := m.(type) {
	case *ReadHoldingRegistersResponse:
		return r.Registers, true
	case *ReadInputRegistersResponse:
		return r.Registers, true
	case *ReadWriteMultipleRegistersResponse:
		return r.Registers, true
	default:
		return nil, false
	}
}

// checkEcho verifies that a write-single response repeats the request.
func checkEcho(spec requestSpec, response Message) error {
	switch r := response.(type) {
	case *WriteSingleCoil:
		if r.Address != spec.address || coilWire(r.Value) != spec.value {
			return malformed("response writes coil %d = %#04x, the request wrote coil %d = %#04x",
				r.Address, coilWire(r.Value), spec.address, spec.value)
		}
	case *WriteSingleRegister:
		if r.Address != spec.address || r.Value != spec.value {
			return malformed("response writes register %d = %d, the request wrote register %d = %d",
				r.Address, r.Value, spec.address, spec.value)
		}
	default:
		return unsupported("response %T does not echo a single write", response)
	}
	return nil
}

// checkWriteAck verifies that a write-multiple response reports the requested
// range.
func checkWriteAck(spec requestSpec, response Message) error {
	switch r := response.(type) {
	case *WriteMultipleCoilsResponse:
		if r.Address != spec.address || r.Quantity != spec.quantity {
			return malformed("response acknowledges coil %d, quantity %d; the request wrote coil %d, quantity %d",
				r.Address, r.Quantity, spec.address, spec.quantity)
		}
	case *WriteMultipleRegistersResponse:
		if r.Address != spec.address || r.Quantity != spec.quantity {
			return malformed("response acknowledges register %d, quantity %d; the request wrote register %d, quantity %d",
				r.Address, r.Quantity, spec.address, spec.quantity)
		}
	default:
		return unsupported("response %T does not acknowledge a multiple write", response)
	}
	return nil
}

// decodeMaskWrite decodes the 0x16 layout shared by the request and its echo
// response.
func decodeMaskWrite(pdu []byte) (Message, error) {
	if len(pdu) != 7 {
		return nil, malformed("mask write register PDU of %d bytes, want 7", len(pdu))
	}
	return &MaskWriteRegister{
		Address: binary.BigEndian.Uint16(pdu[1:3]),
		AndMask: binary.BigEndian.Uint16(pdu[3:5]),
		OrMask:  binary.BigEndian.Uint16(pdu[5:7]),
	}, nil
}

// decodeReadWriteMultipleRegistersRequest decodes a 0x17 request.
func decodeReadWriteMultipleRegistersRequest(pdu []byte) (Message, error) {
	if len(pdu) < 10 {
		return nil, malformed("read/write multiple registers request PDU of %d bytes, want at least 10", len(pdu))
	}
	readAddress := binary.BigEndian.Uint16(pdu[1:3])
	readQuantity := binary.BigEndian.Uint16(pdu[3:5])
	writeAddress := binary.BigEndian.Uint16(pdu[5:7])
	writeQuantity := binary.BigEndian.Uint16(pdu[7:9])
	if err := checkQuantity("read/write multiple registers read", readQuantity, MaxReadRegisters); err != nil {
		return nil, err
	}
	if err := checkQuantity("read/write multiple registers write", writeQuantity, MaxWriteReadRegisters); err != nil {
		return nil, err
	}
	byteCount := int(pdu[9])
	if byteCount != 2*int(writeQuantity) {
		return nil, malformed("read/write multiple registers byte count %d, want %d for %d registers", byteCount, 2*writeQuantity, writeQuantity)
	}
	if len(pdu) != 10+byteCount {
		return nil, malformed("read/write multiple registers request PDU of %d bytes, want %d", len(pdu), 10+byteCount)
	}
	values := make([]uint16, writeQuantity)
	for i := range values {
		values[i] = binary.BigEndian.Uint16(pdu[10+2*i : 12+2*i])
	}
	return &ReadWriteMultipleRegistersRequest{
		ReadAddress:  readAddress,
		ReadQuantity: readQuantity,
		WriteAddress: writeAddress,
		WriteValues:  values,
	}, nil
}

// decodeDeviceIDRequest decodes a 0x2B/0x0E request.
func decodeDeviceIDRequest(pdu []byte) (Message, error) {
	if len(pdu) < 2 {
		return nil, malformed("encapsulated interface PDU of %d bytes, want at least 2", len(pdu))
	}
	if pdu[1] != MEIReadDeviceIdentification {
		return nil, unsupported("encapsulated interface MEI type %#02x", pdu[1])
	}
	if len(pdu) != 4 {
		return nil, malformed("read device identification request PDU of %d bytes, want 4", len(pdu))
	}
	if err := checkDeviceIDReadCode(pdu[2]); err != nil {
		return nil, err
	}
	return &ReadDeviceIdentificationRequest{ReadCode: pdu[2], ObjectID: pdu[3]}, nil
}

// decodeDeviceIDResponse decodes a 0x2B/0x0E response.
func decodeDeviceIDResponse(pdu []byte) (Message, error) {
	if len(pdu) < 2 {
		return nil, malformed("encapsulated interface PDU of %d bytes, want at least 2", len(pdu))
	}
	if pdu[1] != MEIReadDeviceIdentification {
		return nil, unsupported("encapsulated interface MEI type %#02x", pdu[1])
	}
	if len(pdu) < 7 {
		return nil, malformed("read device identification response PDU of %d bytes, want at least 7", len(pdu))
	}
	if err := checkDeviceIDReadCode(pdu[2]); err != nil {
		return nil, err
	}
	if !validDeviceIDConformity(pdu[3]) {
		return nil, malformed("read device identification conformity level %#02x is outside 0x01..0x83", pdu[3])
	}
	var more bool
	switch pdu[4] {
	case 0x00:
		more = false
	case 0xff:
		more = true
	default:
		return nil, malformed("read device identification more-follows %#02x is neither 0x00 nor 0xFF", pdu[4])
	}
	count := int(pdu[6])
	objects := make([]DeviceObject, 0, count)
	offset := 7
	for i := 0; i < count; i++ {
		if len(pdu) < offset+2 {
			return nil, malformed("read device identification object %d of %d ends inside its header", i+1, count)
		}
		id := pdu[offset]
		length := int(pdu[offset+1])
		offset += 2
		if len(pdu) < offset+length {
			return nil, malformed("read device identification object %#02x declares %d bytes, the PDU carries %d", id, length, len(pdu)-offset)
		}
		value := make([]byte, length)
		copy(value, pdu[offset:offset+length])
		objects = append(objects, DeviceObject{ID: id, Value: value})
		offset += length
	}
	if offset != len(pdu) {
		return nil, malformed("read device identification response PDU carries %d trailing bytes", len(pdu)-offset)
	}
	return &ReadDeviceIdentificationResponse{
		ReadCode:        pdu[2],
		ConformityLevel: pdu[3],
		MoreFollows:     more,
		NextObjectID:    pdu[5],
		Objects:         objects,
	}, nil
}

// checkMaskWrite verifies that a mask-write response repeats the request.
func checkMaskWrite(spec requestSpec, response Message) error {
	r, ok := response.(*MaskWriteRegister)
	if !ok {
		return unsupported("response %T does not echo a mask write", response)
	}
	if r.Address != spec.address || r.AndMask != spec.andMask || r.OrMask != spec.orMask {
		return malformed("response masks %#04x/%#04x at register %d, the request wrote %#04x/%#04x at register %d",
			r.AndMask, r.OrMask, r.Address, spec.andMask, spec.orMask, spec.address)
	}
	return nil
}

// checkDeviceID verifies that a device-identification response echoes the read
// code the request asked for.
func checkDeviceID(spec requestSpec, response Message) error {
	r, ok := response.(*ReadDeviceIdentificationResponse)
	if !ok {
		return unsupported("response %T does not answer a read-device-identification request", response)
	}
	if r.ReadCode != byte(spec.value) {
		return malformed("response read code %#02x, the request asked for %#02x", r.ReadCode, spec.value)
	}
	return nil
}
