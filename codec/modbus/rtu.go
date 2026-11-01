package modbus

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/maxid/gnety/codec/wire"
)

// RTU frame limits. An RTU application data unit is the unit id, the PDU and
// the two CRC bytes; the specification caps it at 256 bytes.
const (
	// RTUMaxFrameLen is the largest RTU application data unit, CRC included.
	RTUMaxFrameLen = 256
	// rtuCRCLen is how many bytes the RTU CRC occupies.
	rtuCRCLen = 2
)

// RTU-only function codes that have no typed message in this package. They are
// listed here because the RTU framing derives a frame length from them, and a
// server built on this package can still answer them through raw frames.
const (
	funcDiagnostics      byte = 0x08
	funcCommEventCounter byte = 0x0b
)

// RTUFrame is one Modbus RTU application data unit without its CRC. The unit id
// and the PDU are the two parts an RTU header carries; everything else on the
// wire is derived.
type RTUFrame struct {
	// UnitID is the addressed slave.
	UnitID uint8
	// PDU is the function code and its data field, never empty.
	PDU []byte
}

// Function returns the PDU function code, or 0 for an empty PDU.
func (f RTUFrame) Function() byte {
	if len(f.PDU) == 0 {
		return 0
	}
	return f.PDU[0]
}

// Marshal encodes the frame with the two CRC bytes.
func (f RTUFrame) Marshal() ([]byte, error) { return RTUOptions{}.Marshal(f) }

// RTUOptions selects RTU framing variants.
//
// The zero value is the specification form: the CRC16 is appended on the wire
// and verified on read. NoCRC exists for gateways that carry RTU frames without
// their checksum over TCP or UDP; serial RTU must keep the CRC.
type RTUOptions struct {
	// NoCRC omits and skips the trailing CRC16.
	NoCRC bool
}

func (o RTUOptions) crcLen() int {
	if o.NoCRC {
		return 0
	}
	return rtuCRCLen
}

// CRC16 returns the Modbus RTU CRC16 of data. The wire order is the low byte
// followed by the high byte.
func CRC16(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range data {
		crc ^= uint16(b)
		for range 8 {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xa001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// Marshal encodes one RTU frame: unit id, PDU and, unless NoCRC, the CRC16.
func (o RTUOptions) Marshal(f RTUFrame) ([]byte, error) {
	if len(f.PDU) == 0 {
		return nil, wire.Errorf(wire.KindMalformed, "modbus: empty RTU PDU")
	}
	if len(f.PDU) > MaxPDU {
		return nil, wire.Errorf(wire.KindTooLarge, "modbus: RTU PDU of %d bytes exceeds %d", len(f.PDU), MaxPDU)
	}
	w := wire.NewWriter(1 + len(f.PDU) + o.crcLen())
	w.Byte(f.UnitID)
	w.Raw(f.PDU)
	if !o.NoCRC {
		crc := CRC16(w.Bytes())
		w.Byte(byte(crc))
		w.Byte(byte(crc >> 8))
	}
	return w.Bytes(), nil
}

// Parse decodes one whole RTU frame, validates its length, CRC and PDU bound,
// and returns a frame with a newly allocated PDU.
//
// A datagram transport already delimits the frame, so Parse does not need to
// know whether the frame is a request or a response. The typed layer validates
// the direction.
func (o RTUOptions) Parse(data []byte, maxPDU int) (RTUFrame, error) {
	if maxPDU <= 0 {
		maxPDU = MaxPDU
	}
	crcLen := o.crcLen()
	if len(data) < 2+crcLen {
		return RTUFrame{}, wire.Errorf(wire.KindMalformed, "modbus: RTU frame of %d bytes is shorter than %d", len(data), 2+crcLen)
	}
	if len(data) > RTUMaxFrameLen {
		return RTUFrame{}, wire.Errorf(wire.KindTooLarge, "modbus: RTU frame of %d bytes exceeds %d", len(data), RTUMaxFrameLen)
	}
	if !o.NoCRC {
		want := CRC16(data[:len(data)-crcLen])
		got := uint16(data[len(data)-2]) | uint16(data[len(data)-1])<<8
		if got != want {
			return RTUFrame{}, wire.Errorf(wire.KindMalformed, "modbus: RTU CRC %#04x, want %#04x", got, want)
		}
	}
	pdu := data[1 : len(data)-crcLen]
	if len(pdu) > maxPDU {
		return RTUFrame{}, wire.Errorf(wire.KindTooLarge, "modbus: RTU PDU of %d bytes exceeds %d", len(pdu), maxPDU)
	}
	out := make([]byte, len(pdu))
	copy(out, pdu)
	return RTUFrame{UnitID: data[0], PDU: out}, nil
}

// Read reads one RTU request from a stream. The frame length is derived from
// the function code, so the reader knows where the CRC starts without a length
// field.
//
// A truncated frame reports wire.ErrIncomplete and leaves the reader positioned
// mid-frame: a stream decoder should stop for the connection, not for the
// process.
func (o RTUOptions) Read(r *bufio.Reader, maxPDU int, functions Functions) (RTUFrame, error) {
	return o.readFrame(r, maxPDU, false, functions, nil)
}

// ReadResponse reads one RTU response to req from a stream.
func (o RTUOptions) ReadResponse(r *bufio.Reader, maxPDU int, functions Functions, req Message) (RTUFrame, error) {
	return o.readFrame(r, maxPDU, true, functions, req)
}

// TypedRTUFrame is one RTU application data unit whose PDU is a typed message
// instead of raw bytes.
type TypedRTUFrame struct {
	// UnitID is the addressed slave.
	UnitID uint8
	// Message is the typed PDU. It is never nil.
	Message Message
}

// ReadTypedRequest reads one RTU frame and decodes its PDU in request
// orientation.
func (o RTUOptions) ReadTypedRequest(r *bufio.Reader, maxPDU int, functions Functions) (TypedRTUFrame, error) {
	frame, err := o.Read(r, maxPDU, functions)
	if err != nil {
		return TypedRTUFrame{}, err
	}
	message, err := functions.DecodePDU(frame.PDU)
	if err != nil {
		return TypedRTUFrame{}, err
	}
	return TypedRTUFrame{UnitID: frame.UnitID, Message: message}, nil
}

// ReadTypedResponse reads one RTU frame and decodes its PDU as the response to
// req.
func (o RTUOptions) ReadTypedResponse(r *bufio.Reader, maxPDU int, functions Functions, req Message) (TypedRTUFrame, error) {
	frame, err := o.ReadResponse(r, maxPDU, functions, req)
	if err != nil {
		return TypedRTUFrame{}, err
	}
	message, err := functions.DecodeResponse(req, frame.PDU)
	if err != nil {
		return TypedRTUFrame{}, err
	}
	return TypedRTUFrame{UnitID: frame.UnitID, Message: message}, nil
}

// ParseTypedRequest decodes one whole RTU datagram in request orientation.
func (o RTUOptions) ParseTypedRequest(data []byte, maxPDU int, functions Functions) (TypedRTUFrame, error) {
	frame, err := o.Parse(data, maxPDU)
	if err != nil {
		return TypedRTUFrame{}, err
	}
	message, err := functions.DecodePDU(frame.PDU)
	if err != nil {
		return TypedRTUFrame{}, err
	}
	return TypedRTUFrame{UnitID: frame.UnitID, Message: message}, nil
}

// ParseTypedResponse decodes one whole RTU datagram as the response to req.
func (o RTUOptions) ParseTypedResponse(data []byte, maxPDU int, functions Functions, req Message) (TypedRTUFrame, error) {
	frame, err := o.Parse(data, maxPDU)
	if err != nil {
		return TypedRTUFrame{}, err
	}
	message, err := functions.DecodeResponse(req, frame.PDU)
	if err != nil {
		return TypedRTUFrame{}, err
	}
	return TypedRTUFrame{UnitID: frame.UnitID, Message: message}, nil
}

// readFrame reads bytes until the framing knows the frame length, then reads the
// remainder (including the CRC) and validates it.
func (o RTUOptions) readFrame(r *bufio.Reader, maxPDU int, response bool, functions Functions, req Message) (RTUFrame, error) {
	if maxPDU <= 0 {
		maxPDU = MaxPDU
	}
	crcLen := o.crcLen()
	frame := make([]byte, 0, RTUMaxFrameLen)
	for {
		b, err := r.ReadByte()
		if err != nil {
			return RTUFrame{}, rtuIncomplete(err)
		}
		if len(frame) >= RTUMaxFrameLen {
			return RTUFrame{}, wire.Errorf(wire.KindTooLarge, "modbus: RTU frame exceeds %d bytes", RTUMaxFrameLen)
		}
		frame = append(frame, b)

		pduLen, err := o.pduLength(frame, response, functions, req)
		if err != nil {
			return RTUFrame{}, err
		}
		if pduLen == 0 {
			// The framing needs another byte before it can tell where the frame
			// ends.
			continue
		}
		if pduLen < 0 || pduLen > MaxPDU {
			return RTUFrame{}, wire.Errorf(wire.KindTooLarge, "modbus: RTU PDU of %d bytes exceeds %d", pduLen, MaxPDU)
		}
		if pduLen > maxPDU {
			return RTUFrame{}, wire.Errorf(wire.KindTooLarge, "modbus: RTU PDU of %d bytes exceeds %d", pduLen, maxPDU)
		}
		total := 1 + pduLen + crcLen
		if total > RTUMaxFrameLen {
			return RTUFrame{}, wire.Errorf(wire.KindTooLarge, "modbus: RTU frame of %d bytes exceeds %d", total, RTUMaxFrameLen)
		}
		if total < len(frame) {
			return RTUFrame{}, wire.Errorf(wire.KindMalformed,
				"modbus: RTU frame length %d is shorter than the %d bytes already read", total, len(frame))
		}
		if len(frame) < total {
			rest := make([]byte, total-len(frame))
			if _, err := io.ReadFull(r, rest); err != nil {
				return RTUFrame{}, rtuIncomplete(err)
			}
			frame = append(frame, rest...)
		}
		return o.frameFromBytes(frame, maxPDU)
	}
}

// pduLength reports the PDU length of a partially read frame, 0 when more bytes
// are needed. The registry is consulted first, then the built-in table.
func (o RTUOptions) pduLength(frame []byte, response bool, functions Functions, req Message) (int, error) {
	if len(frame) < 2 {
		return 0, nil
	}
	function := frame[1]
	if IsException(function) {
		// A function code with the exception bit set always carries exactly one
		// exception byte.
		return 2, nil
	}
	if fn, ok := functions[function]; ok {
		if response {
			if fn.ResponseLength != nil {
				return fn.ResponseLength(req, frame)
			}
		} else if fn.RequestLength != nil {
			return fn.RequestLength(frame)
		}
	}
	return standardRTUPDULength(frame, response)
}

// standardRTUPDULength is the framing table of the built-in function codes.
// frame starts with the unit id and the function code.
func standardRTUPDULength(frame []byte, response bool) (int, error) {
	function := frame[1]
	if response {
		switch function {
		case FuncReadCoils, FuncReadDiscreteInputs, FuncReadHoldingRegisters, FuncReadInputRegisters, FuncReadWriteMultipleRegisters:
			if len(frame) < 3 {
				return 0, nil
			}
			return 2 + int(frame[2]), nil
		case FuncWriteSingleCoil, FuncWriteSingleRegister, FuncWriteMultipleCoils, FuncWriteMultipleRegisters:
			return 5, nil
		case FuncMaskWriteRegister:
			return 7, nil
		case FuncReadExceptionStatus:
			return 2, nil
		case funcDiagnostics:
			return 5, nil
		case funcCommEventCounter:
			return 5, nil
		case FuncEncapsulatedInterface:
			return deviceIdentificationPDULength(frame)
		default:
			return 0, wire.Errorf(wire.KindUnsupported, "modbus: no RTU response framing for function %#02x", function)
		}
	}
	switch function {
	case FuncReadCoils, FuncReadDiscreteInputs, FuncReadHoldingRegisters, FuncReadInputRegisters,
		FuncWriteSingleCoil, FuncWriteSingleRegister, funcDiagnostics:
		return 5, nil
	case FuncReadExceptionStatus, funcCommEventCounter:
		return 1, nil
	case FuncWriteMultipleCoils, FuncWriteMultipleRegisters:
		if len(frame) < 7 {
			return 0, nil
		}
		return 6 + int(frame[6]), nil
	case FuncMaskWriteRegister:
		return 7, nil
	case FuncReadWriteMultipleRegisters:
		if len(frame) < 11 {
			return 0, nil
		}
		return 10 + int(frame[10]), nil
	case FuncEncapsulatedInterface:
		return 4, nil
	default:
		return 0, wire.Errorf(wire.KindUnsupported, "modbus: no RTU request framing for function %#02x", function)
	}
}

// deviceIdentificationPDULength derives the PDU length of a 0x2B/0x0E response
// by walking its object list. The first eight bytes are fixed: unit, function,
// MEI type, read device id code, conformity level, more follows, next object id
// and the object count.
func deviceIdentificationPDULength(frame []byte) (int, error) {
	if len(frame) < 8 {
		return 0, nil
	}
	if frame[2] != MEIReadDeviceIdentification {
		return 0, wire.Errorf(wire.KindUnsupported, "modbus: no RTU framing for MEI type %#02x", frame[2])
	}
	count := int(frame[7])
	offset := 8
	for i := 0; i < count; i++ {
		if len(frame) < offset+2 {
			return 0, nil
		}
		length := int(frame[offset+1])
		offset += 2 + length
		if 1+offset-1+rtuCRCLen > RTUMaxFrameLen {
			return 0, wire.Errorf(wire.KindTooLarge, "modbus: read device identification response exceeds %d bytes", RTUMaxFrameLen)
		}
	}
	return offset - 1, nil
}

// frameFromBytes validates the CRC and the PDU bound of a complete frame and
// returns a frame with a newly allocated PDU.
func (o RTUOptions) frameFromBytes(frame []byte, maxPDU int) (RTUFrame, error) {
	crcLen := o.crcLen()
	if len(frame) < 2+crcLen {
		return RTUFrame{}, wire.Errorf(wire.KindMalformed, "modbus: RTU frame of %d bytes is shorter than %d", len(frame), 2+crcLen)
	}
	if len(frame) > RTUMaxFrameLen {
		return RTUFrame{}, wire.Errorf(wire.KindTooLarge, "modbus: RTU frame of %d bytes exceeds %d", len(frame), RTUMaxFrameLen)
	}
	if !o.NoCRC {
		want := CRC16(frame[:len(frame)-crcLen])
		got := uint16(frame[len(frame)-2]) | uint16(frame[len(frame)-1])<<8
		if got != want {
			return RTUFrame{}, wire.Errorf(wire.KindMalformed, "modbus: RTU CRC %#04x, want %#04x", got, want)
		}
	}
	pdu := frame[1 : len(frame)-crcLen]
	if len(pdu) > maxPDU {
		return RTUFrame{}, wire.Errorf(wire.KindTooLarge, "modbus: RTU PDU of %d bytes exceeds %d", len(pdu), maxPDU)
	}
	out := make([]byte, len(pdu))
	copy(out, pdu)
	return RTUFrame{UnitID: frame[0], PDU: out}, nil
}

// rtuIncomplete keeps the io cause while classifying the error as incomplete,
// so a caller can test both wire.ErrIncomplete and io.ErrUnexpectedEOF.
func rtuIncomplete(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: %w", wire.ErrIncomplete, err)
	}
	return err
}
