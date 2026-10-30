// Package coap implements the CoAP (RFC 7252) wire format: the four-byte
// header, the token, the option stream and the payload of one datagram.
//
// Two layers are available. Parse and Marshal convert a whole datagram to a
// Message and back and have always done so. Decoder, ParseLimit and Encode add
// the bounded and strict entry points a server wants, and the typed option
// accessors (URIPath, ContentFormat, Block1, Observe and friends) read and
// write the wire-faithful Option list.
//
// It converts bytes to messages and messages to bytes only. Retransmission,
// message-id deduplication, exchange lifetimes, observe registries and
// block-wise reassembly are protocol state machines: they belong to the
// protocol libraries (gcoap, glwm2m) built on gnety, never here. The package
// imports only the Go standard library and codec/wire.
package coap

import (
	"errors"
	"fmt"
)

// Version is the CoAP version implemented by this package.
const Version = 1

// Type is the CoAP message type, the two-bit field in the first header byte.
type Type uint8

// CoAP message types (RFC 7252 section 3).
const (
	// Confirmable is a message that must be acknowledged (CON).
	Confirmable Type = 0
	// NonConfirmable is a message that is not acknowledged (NON).
	NonConfirmable Type = 1
	// Acknowledgement acknowledges a confirmable message (ACK).
	Acknowledgement Type = 2
	// Reset rejects a message that could not be processed (RST).
	Reset Type = 3
)

// String returns the specification abbreviation of the message type.
func (t Type) String() string {
	switch t {
	case Confirmable:
		return "CON"
	case NonConfirmable:
		return "NON"
	case Acknowledgement:
		return "ACK"
	case Reset:
		return "RST"
	default:
		return "unknown"
	}
}

// Code is the CoAP request/response code (class<<5 | detail).
type Code uint8

// Request method codes.
const (
	// CodeEmpty is the empty code, 0.00. ACK and RST carry it, and so does a
	// CoAP ping.
	CodeEmpty Code = 0
	// CodeGET is GET, 0.01.
	CodeGET Code = 1
	// CodePOST is POST, 0.02.
	CodePOST Code = 2
	// CodePUT is PUT, 0.03.
	CodePUT Code = 3
	// CodeDELETE is DELETE, 0.04.
	CodeDELETE Code = 4
	// CodeFETCH is FETCH, 0.05 (RFC 8132).
	CodeFETCH Code = 5
	// CodePATCH is PATCH, 0.06 (RFC 8132).
	CodePATCH Code = 6
)

// Response codes a server commonly sends.
const (
	// CodeCreated is 2.01 Created.
	CodeCreated Code = 65
	// CodeDeleted is 2.02 Deleted.
	CodeDeleted Code = 66
	// CodeValid is 2.03 Valid.
	CodeValid Code = 67
	// CodeChanged is 2.04 Changed.
	CodeChanged Code = 68
	// CodeContent is 2.05 Content.
	CodeContent Code = 69
	// CodeContinue is 2.31 Continue, the interim answer to a block-wise
	// request body (RFC 7959).
	CodeContinue Code = 95
	// CodeBadRequest is 4.00 Bad Request.
	CodeBadRequest Code = 128
	// CodeUnauthorized is 4.01 Unauthorized.
	CodeUnauthorized Code = 129
	// CodeNotFound is 4.04 Not Found.
	CodeNotFound Code = 132
	// CodeMethodNotAllowed is 4.05 Method Not Allowed.
	CodeMethodNotAllowed Code = 133
	// CodePreconditionFailed is 4.12 Precondition Failed.
	CodePreconditionFailed Code = 140
	// CodeRequestEntityTooLarge is 4.13 Request Entity Too Large.
	CodeRequestEntityTooLarge Code = 141
	// CodeUnsupportedContentFormat is 4.15 Unsupported Content-Format.
	CodeUnsupportedContentFormat Code = 143
	// CodeInternalServerError is 5.00 Internal Server Error.
	CodeInternalServerError Code = 160
	// CodeNotImplemented is 5.01 Not Implemented.
	CodeNotImplemented Code = 161
	// CodeServiceUnavailable is 5.03 Service Unavailable.
	CodeServiceUnavailable Code = 163
)

// Class returns the code class (0-7).
func (c Code) Class() uint8 { return uint8(c) >> 5 }

// Detail returns the code detail (0-31).
func (c Code) Detail() uint8 { return uint8(c) & 0x1f }

// String renders the code as "c.dd".
func (c Code) String() string { return fmt.Sprintf("%d.%02d", c.Class(), c.Detail()) }

// IsRequest reports whether the code is a request method code: class 0 with a
// non-zero detail. The empty code, 0.00, is not a request.
func (c Code) IsRequest() bool { return c.Class() == 0 && c.Detail() != 0 }

// IsResponse reports whether the code is a response code, that is class 2
// (success), 4 (client error) or 5 (server error). Classes 1, 3, 6 and 7 are
// reserved.
func (c Code) IsResponse() bool {
	switch c.Class() {
	case 2, 4, 5:
		return true
	default:
		return false
	}
}

// Option is one CoAP option: the option number from the registry and the raw
// value bytes exactly as they travel on the wire.
//
// Option values are not interpreted here; the typed accessors on Message do
// that. Options with numbers this package does not know are carried unchanged,
// which is what lets an endpoint implement extensions without a codec change.
type Option struct {
	// Number is the option number: the accumulated delta of the option stream.
	Number uint16
	// Value is the raw option value. For a known option number, Encode and
	// the validated setters bound its length to the option definition.
	Value []byte
}

// Message is one CoAP message: the header, token, options and payload of a
// single datagram.
//
// Options are wire-faithful, so a decoded message can be re-encoded without
// losing an extension. The typed accessors keep the list sorted by number;
// Marshal sorts it before encoding, and Encode rejects a list that is unsorted
// or repeats an option that is not repeatable.
//
// The typed layer describes messages only. Retransmission, message-id
// deduplication, observe registries and block-wise reassembly belong to the
// protocol library that consumes this package.
type Message struct {
	// Version is the CoAP version. A decoded message always has it set; a zero
	// value is treated as Version when encoding, so a message built by hand
	// does not need it.
	Version uint8
	// Type is the message type.
	Type Type
	// Code is the request method or the response code.
	Code Code
	// MessageID is the 16-bit message identifier used for deduplication and
	// for matching an acknowledgement to its confirmable message. Choosing
	// and tracking it is exchange state, not wire format.
	MessageID uint16
	// Token matches a response to the request that caused it. A decoded token
	// is newly allocated and 0 to 8 bytes long.
	Token []byte
	// Options holds the options. Marshal sorts them by number; Encode requires
	// them sorted.
	Options []Option
	// Payload is the message body. It is empty when the datagram carries no
	// payload marker.
	Payload []byte
}

// ErrMalformed indicates an invalid CoAP message. Every decoding failure wraps
// it, including a datagram that exceeds a configured size limit; codec/wire's
// KindOf reports which class of failure it was.
var ErrMalformed = errors.New("coap: malformed message")
