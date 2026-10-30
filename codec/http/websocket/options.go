package websocket

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

// ErrFrameTooLarge is returned when a reassembled message exceeds the limit.
var ErrFrameTooLarge = errors.New("gnety: websocket frame too large")

// Options configures server or client WebSocket setup.
type Options struct {
	// Subprotocol is the required/selected Sec-WebSocket-Protocol.
	Subprotocol string
	// OriginPatterns authorizes cross-origin handshakes (path.Match against the
	// Origin host, or scheme://host when the pattern contains "://").
	OriginPatterns []string
	// AllowAnyOrigin disables Origin verification. Use only for non-browser
	// device gateways where Origin is not a security boundary.
	AllowAnyOrigin bool
	// MaxMessageSize limits the reassembled message; 0 uses the package default.
	MaxMessageSize int
	// Hijack obtains the connection during the server handshake. The default
	// (nil) requires w to implement http.Hijacker: the library uses the writer
	// it was given and never reaches through a wrapper. Set it when the server
	// framework exposes the hijackable writer another way, for example:
	//
	//	Options{Hijack: http.NewResponseController(w).Hijack}
	//
	// Ignored by the client handshake, which is handed a net.Conn directly.
	Hijack func(http.ResponseWriter) (net.Conn, *bufio.ReadWriter, error)
}

// ValidSubprotocol reports whether s is a valid Sec-WebSocket-Protocol token.
func ValidSubprotocol(s string) bool { return validToken(s) }

// HasSubprotocol reports whether the request header offers want exactly.
func HasSubprotocol(h http.Header, want string) bool { return hasSubprotocol(h, want) }
