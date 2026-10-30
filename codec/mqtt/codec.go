package mqtt

import (
	"bufio"
	"io"
)

// Codec adapts the typed codec to gnety's stream codec shape, so an MQTT
// server or client built on this module can plug it into a pipeline directly.
//
// Decode returns a Message; Encode accepts the pointer types Decode produces and
// the equivalent values.
type Codec struct {
	// Decoder carries the negotiation state the adapter uses.
	Decoder
}

// Decode reads one typed control packet.
func (c *Codec) Decode(r *bufio.Reader) (any, error) { return c.Decoder.Decode(r) }

// Encode writes one typed control packet.
func (c *Codec) Encode(w io.Writer, msg any) error {
	m, ok := msg.(Message)
	if !ok {
		return malformed("unsupported message %T", msg)
	}
	return Encode(w, m)
}
