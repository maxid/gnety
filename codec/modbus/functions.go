package modbus

// Function describes how the codec handles one function code that is not part
// of the built-in set, or replaces the built-in handling of one that is.
//
// The registry is consulted before the built-in decoder, so a vendor that
// redefines the payload of a standard function code can override it without
// changing this package. An exception response (function code with the high bit
// set) is always decoded by the built-in exception decoder and never reaches a
// registered decoder.
//
// A zero Function leaves that direction unsupported: decoding fails with
// wire.KindUnsupported and the RTU reader cannot frame it on a stream.
type Function struct {
	// DecodeRequest decodes a request PDU: the function code followed by its
	// data field. It is used by servers and by the datagram path.
	DecodeRequest func(pdu []byte) (Message, error)
	// DecodeResponse decodes the PDU that answers req. It must apply the
	// function code and size checks the custom format defines.
	DecodeResponse func(req Message, pdu []byte) (Message, error)
	// RequestLength reports the PDU length (function code included) of a
	// request from the bytes read so far. frame starts with the unit id and the
	// function code and may hold only a prefix of the PDU; a return of 0 asks
	// the reader for one more byte. It is only needed on RTU stream transports;
	// MBAP and whole-datagram paths derive the length from the frame itself.
	RequestLength func(frame []byte) (int, error)
	// ResponseLength is the response-direction counterpart of RequestLength.
	// req is the request the response answers, so a response whose size depends
	// on the request can be framed.
	ResponseLength func(req Message, frame []byte) (int, error)
}

// Functions is a function-code registry keyed by the request function code.
//
// A nil map is valid and means "built-in function codes only". The decoder
// consults the registry first and falls back to the built-in set, so an entry
// may also override a standard function code.
type Functions map[byte]Function

// Function returns the registry entry for function, or the zero Function.
func (f Functions) Function(function byte) Function { return f[function] }

// DecodePDU decodes one request PDU with the registry consulted first.
// Exception responses are decoded by the built-in decoder, whatever the
// registry says, because their layout is fixed by the specification.
func (f Functions) DecodePDU(pdu []byte) (Message, error) {
	if len(pdu) > 0 && !IsException(pdu[0]) {
		if fn, ok := f[pdu[0]]; ok && fn.DecodeRequest != nil {
			return fn.DecodeRequest(pdu)
		}
	}
	return DecodePDU(pdu)
}

// DecodeResponse decodes one response PDU to req with the registry consulted
// first. A registered decoder is keyed by the request function code, so it also
// sees the response to a vendor-modified standard function code.
func (f Functions) DecodeResponse(req Message, pdu []byte) (Message, error) {
	if len(pdu) > 0 && IsException(pdu[0]) {
		// The exception layout is fixed, and a vendor request type has no
		// responseSpec for the built-in decoder, so exceptions are decoded here.
		message, err := decodeException(pdu)
		if err != nil {
			return nil, err
		}
		exception := message.(*Exception)
		if exception.FunctionCode != req.Function() {
			return nil, malformed("exception for function %#02x answers a %#02x request", exception.FunctionCode, req.Function())
		}
		return exception, nil
	}
	if len(pdu) > 0 {
		if fn, ok := f[pdu[0]]; ok && fn.DecodeResponse != nil {
			return fn.DecodeResponse(req, pdu)
		}
	}
	return DecodeResponse(req, pdu)
}
