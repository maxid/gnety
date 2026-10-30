package modbus

import "bufio"

// TypedFrame is one Modbus TCP application data unit whose PDU is a typed
// message instead of raw bytes. It mirrors Frame with Message in place of PDU.
type TypedFrame struct {
	// TransactionID is the client-chosen transaction identifier.
	TransactionID uint16
	// UnitID is the addressed unit.
	UnitID uint8
	// Message is the typed PDU. It must not be nil.
	Message Message
}

// Marshal encodes the typed frame as one MBAP header plus the encoded PDU.
func (f TypedFrame) Marshal() ([]byte, error) {
	frame, err := f.Frame()
	if err != nil {
		return nil, err
	}
	return frame.Marshal()
}

// Frame returns the raw frame holding the encoded PDU.
func (f TypedFrame) Frame() (Frame, error) {
	if isNilMessage(f.Message) {
		return Frame{}, malformed("typed frame carries no message")
	}
	pdu, err := f.Message.PDU()
	if err != nil {
		return Frame{}, err
	}
	return Frame{TransactionID: f.TransactionID, UnitID: f.UnitID, PDU: pdu}, nil
}

// ReadTyped reads one MBAP frame and decodes its PDU in request orientation.
// See DecodePDU: a response to a read must be read with ReadTypedResponse.
func ReadTyped(r *bufio.Reader, maxPDU int) (TypedFrame, error) {
	frame, err := Read(r, maxPDU)
	if err != nil {
		return TypedFrame{}, err
	}
	message, err := DecodePDU(frame.PDU)
	if err != nil {
		return TypedFrame{}, err
	}
	return TypedFrame{TransactionID: frame.TransactionID, UnitID: frame.UnitID, Message: message}, nil
}

// ReadTypedResponse reads one MBAP frame and decodes its PDU as the response to
// req, applying DecodeResponse's function code and size checks. req is
// validated before a frame is read, so an invalid request does not consume one.
func ReadTypedResponse(r *bufio.Reader, maxPDU int, req Message) (TypedFrame, error) {
	if _, err := requestSpecOf(req); err != nil {
		return TypedFrame{}, err
	}
	frame, err := Read(r, maxPDU)
	if err != nil {
		return TypedFrame{}, err
	}
	message, err := DecodeResponse(req, frame.PDU)
	if err != nil {
		return TypedFrame{}, err
	}
	return TypedFrame{TransactionID: frame.TransactionID, UnitID: frame.UnitID, Message: message}, nil
}
