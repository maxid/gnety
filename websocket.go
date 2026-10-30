package gnety

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const defaultWebSocketMaxFrameSize = 4 << 20

// webSocketConn is the message/stream adapter in gnety's transport pipeline.
// The frame codec is in websocket_frame.go; handshakes are in
// websocket_handshake.go. Protocol state machines remain above RawHandler.
type webSocketConn struct {
	raw         net.Conn
	reader      *bufio.Reader
	writer      *bufio.Writer
	maskWrites  bool
	maxMessage  atomic.Uint64
	subprotocol string

	readMu         sync.Mutex
	frame          webSocketFrameHeader
	frameActive    bool
	frameRemaining uint64
	frameOffset    uint64
	inMessage      bool
	messageOpcode  byte
	messageSize    uint64
	textPending    []byte
	textExpected   int
	readErr        error

	writeMu   sync.Mutex
	closeOnce sync.Once
	closeErr  error
	closeSent bool
	closed    bool
	done      chan struct{}

	activePingsMu sync.Mutex
	activePings   map[string]chan struct{}
}

func newWebSocketConn(raw net.Conn, rw *bufio.ReadWriter, maskWrites bool, maxFrameSize int, subprotocol string) *webSocketConn {
	if maxFrameSize <= 0 {
		maxFrameSize = defaultWebSocketMaxFrameSize
	}
	c := &webSocketConn{
		raw: raw, reader: rw.Reader, writer: rw.Writer,
		maskWrites: maskWrites, subprotocol: subprotocol,
		done:        make(chan struct{}),
		activePings: make(map[string]chan struct{}),
	}
	c.maxMessage.Store(uint64(maxFrameSize))
	return c
}

func (c *webSocketConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if c.readErr != nil {
		return 0, c.readErr
	}
	for {
		if !c.frameActive {
			if err := c.readDataFrame(); err != nil {
				return 0, c.setReadFailure(err)
			}
		}
		want := len(p)
		if uint64(want) > c.frameRemaining {
			want = int(c.frameRemaining)
		}
		n, err := c.reader.Read(p[:want])
		if n > 0 {
			chunk := p[:n]
			if c.frame.masked {
				applyWebSocketMask(chunk, c.frame.maskingKey, c.frameOffset)
			}
			c.frameOffset += uint64(n)
			c.frameRemaining -= uint64(n)
			if c.messageOpcode == webSocketTextFrame && !c.validTextChunk(chunk, false) {
				return n, c.setReadFailure(&webSocketProtocolError{status: 1007, text: "invalid UTF-8 text message"})
			}
			if c.frameRemaining == 0 {
				if finishErr := c.finishDataFrame(); finishErr != nil {
					return n, c.setReadFailure(finishErr)
				}
			}
			return n, nil
		}
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			c.readErr = err
			return 0, err
		}
	}
}

func (c *webSocketConn) readDataFrame() error {
	for {
		h, err := readWebSocketFrameHeader(c.reader)
		if err != nil {
			return err
		}
		// Clients mask writes and require unmasked server frames; servers
		// require masked client frames and never mask their own writes.
		if h.masked == c.maskWrites {
			return newWebSocketProtocolError("invalid masking direction")
		}
		switch h.opcode {
		case webSocketPingFrame, webSocketPongFrame, webSocketCloseFrame:
			payload, err := c.readControlPayload(h)
			if err != nil {
				return err
			}
			switch h.opcode {
			case webSocketPingFrame:
				if err := c.writeControl(webSocketPongFrame, payload); err != nil {
					return err
				}
			case webSocketPongFrame:
				c.deliverPong(payload)
			case webSocketCloseFrame:
				status := uint16(1000)
				if len(payload) == 1 {
					return newWebSocketProtocolError("close payload has one byte")
				}
				if len(payload) >= 2 {
					status = binary.BigEndian.Uint16(payload[:2])
					if !validWebSocketCloseStatus(status) || !utf8.Valid(payload[2:]) {
						return newWebSocketProtocolError("invalid close status or reason")
					}
				}
				_ = c.writeClose(status)
				return io.EOF
			}
			continue
		case webSocketContinuationFrame:
			if !c.inMessage {
				return newWebSocketProtocolError("continuation without an active message")
			}
		case webSocketTextFrame, webSocketBinaryFrame:
			if c.inMessage {
				return newWebSocketProtocolError("new data frame during a fragmented message")
			}
			c.inMessage = true
			c.messageOpcode = h.opcode
			c.messageSize = 0
			c.textPending = c.textPending[:0]
			c.textExpected = 0
		}
		if h.length > c.maxMessage.Load()-c.messageSize {
			return ErrFrameTooLarge
		}
		c.messageSize += h.length
		c.frame = h
		c.frameOffset = 0
		c.frameRemaining = h.length
		c.frameActive = true
		if h.length == 0 {
			if err := c.finishDataFrame(); err != nil {
				return err
			}
			continue
		}
		return nil
	}
}

func (c *webSocketConn) readControlPayload(h webSocketFrameHeader) ([]byte, error) {
	payload := make([]byte, int(h.length))
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return nil, err
	}
	if h.masked {
		applyWebSocketMask(payload, h.maskingKey, 0)
	}
	return payload, nil
}

func (c *webSocketConn) finishDataFrame() error {
	c.frameActive = false
	if !c.frame.fin {
		return nil
	}
	if c.messageOpcode == webSocketTextFrame && !c.validTextChunk(nil, true) {
		return &webSocketProtocolError{status: 1007, text: "incomplete UTF-8 text message"}
	}
	c.inMessage = false
	c.messageOpcode = 0
	c.textPending = c.textPending[:0]
	c.textExpected = 0
	return nil
}

func (c *webSocketConn) setReadFailure(err error) error {
	if c.readErr != nil {
		return c.readErr
	}
	var protocolErr *webSocketProtocolError
	switch {
	case errors.Is(err, ErrFrameTooLarge):
		_ = c.writeClose(1009)
	case errors.As(err, &protocolErr):
		_ = c.writeClose(protocolErr.status)
	}
	c.readErr = err
	return err
}

func (c *webSocketConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return 0, net.ErrClosed
	}
	if err := writeWebSocketFrame(c.writer, c.maskWrites, true, webSocketBinaryFrame, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *webSocketConn) writeControl(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	return writeWebSocketFrame(c.writer, c.maskWrites, true, opcode, payload)
}

func (c *webSocketConn) writeClose(status uint16) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed || c.closeSent {
		return nil
	}
	payload := []byte{byte(status >> 8), byte(status)}
	err := writeWebSocketFrame(c.writer, c.maskWrites, true, webSocketCloseFrame, payload)
	c.closeSent = true
	return err
}

func (c *webSocketConn) Close() error {
	return c.CloseWithStatus(WebSocketStatusNormalClosure, "")
}

// WebSocketConn is the public WebSocket net.Conn adapter.
type WebSocketConn = webSocketConn

// WebSocketStatus is a WebSocket close status code.
type WebSocketStatus uint16

const (
	WebSocketStatusNormalClosure   WebSocketStatus = 1000
	WebSocketStatusGoingAway       WebSocketStatus = 1001
	WebSocketStatusProtocolError   WebSocketStatus = 1002
	WebSocketStatusUnsupportedData WebSocketStatus = 1003
	WebSocketStatusInvalidPayload  WebSocketStatus = 1007
	WebSocketStatusPolicyViolation WebSocketStatus = 1008
	WebSocketStatusMessageTooBig   WebSocketStatus = 1009
	WebSocketStatusInternalError   WebSocketStatus = 1011
)

// WebSocketCloseError reports a peer close status.
type WebSocketCloseError struct {
	Code   WebSocketStatus
	Reason string
}

func (e *WebSocketCloseError) Error() string {
	return fmt.Sprintf("gnety: websocket closed: %d %s", e.Code, e.Reason)
}

// CloseWithStatus performs the close handshake frame write and closes the raw
// connection. The reason is truncated to the RFC 6455 control payload limit.
func (c *webSocketConn) CloseWithStatus(status WebSocketStatus, reason string) error {
	c.closeOnce.Do(func() {
		c.writeMu.Lock()
		var closeFrameErr error
		if !c.closed && !c.closeSent {
			if !validWebSocketCloseStatus(uint16(status)) {
				status = WebSocketStatusNormalClosure
			}
			if !utf8.ValidString(reason) {
				reason = ""
			}
			if len(reason) > 123 {
				reason = reason[:123]
			}
			payload := []byte{byte(status >> 8), byte(status)}
			payload = append(payload, reason...)
			closeFrameErr = writeWebSocketFrame(c.writer, c.maskWrites, true, webSocketCloseFrame, payload)
			c.closeSent = true
		}
		c.closed = true
		c.writeMu.Unlock()
		close(c.done)
		rawErr := c.raw.Close()
		if closeFrameErr != nil {
			c.closeErr = closeFrameErr
		} else {
			c.closeErr = rawErr
		}
	})
	return c.closeErr
}

// SetReadLimit limits the reassembled message size. n < 0 disables the limit.
func (c *webSocketConn) SetReadLimit(n int64) {
	if n < 0 {
		c.maxMessage.Store(^uint64(0))
		return
	}
	if n == 0 {
		c.maxMessage.Store(defaultWebSocketMaxFrameSize)
		return
	}
	c.maxMessage.Store(uint64(n))
}

// Subprotocol returns the negotiated WebSocket subprotocol.
func (c *webSocketConn) Subprotocol() string { return c.subprotocol }

// Ping sends a ping and waits for the matching pong. A concurrent Read is
// required to process the pong, as with coder/websocket's Ping API.
func (c *webSocketConn) Ping(ctx context.Context, payload []byte) error {
	if len(payload) > 125 {
		return errors.New("gnety: websocket ping payload exceeds 125 bytes")
	}
	key := string(payload)
	if key == "" {
		key = "gnety-ping"
	}
	ch := make(chan struct{}, 1)
	c.activePingsMu.Lock()
	c.activePings[key] = ch
	c.activePingsMu.Unlock()
	defer func() {
		c.activePingsMu.Lock()
		delete(c.activePings, key)
		c.activePingsMu.Unlock()
	}()
	if err := c.writeControl(webSocketPingFrame, []byte(key)); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return net.ErrClosed
	case <-ch:
		return nil
	}
}

func (c *webSocketConn) deliverPong(payload []byte) {
	c.activePingsMu.Lock()
	ch := c.activePings[string(payload)]
	c.activePingsMu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (c *webSocketConn) LocalAddr() net.Addr                { return c.raw.LocalAddr() }
func (c *webSocketConn) RemoteAddr() net.Addr               { return c.raw.RemoteAddr() }
func (c *webSocketConn) SetDeadline(t time.Time) error      { return c.raw.SetDeadline(t) }
func (c *webSocketConn) SetReadDeadline(t time.Time) error  { return c.raw.SetReadDeadline(t) }
func (c *webSocketConn) SetWriteDeadline(t time.Time) error { return c.raw.SetWriteDeadline(t) }

func (c *webSocketConn) validTextChunk(p []byte, final bool) bool {
	for len(p) > 0 {
		if c.textExpected == 0 {
			if p[0] < utf8.RuneSelf {
				p = p[1:]
				continue
			}
			switch {
			case p[0] >= 0xc2 && p[0] <= 0xdf:
				c.textExpected = 2
			case p[0] >= 0xe0 && p[0] <= 0xef:
				c.textExpected = 3
			case p[0] >= 0xf0 && p[0] <= 0xf4:
				c.textExpected = 4
			default:
				return false
			}
		}
		need := c.textExpected - len(c.textPending)
		take := len(p)
		if take > need {
			take = need
		}
		c.textPending = append(c.textPending, p[:take]...)
		p = p[take:]
		if len(c.textPending) == c.textExpected {
			r, size := utf8.DecodeRune(c.textPending)
			if r == utf8.RuneError && size == 1 {
				return false
			}
			c.textPending = c.textPending[:0]
			c.textExpected = 0
		}
	}
	return !final || c.textExpected == 0
}

func validWebSocketCloseStatus(status uint16) bool {
	if status < 1000 || status >= 5000 {
		return false
	}
	switch status {
	case 1004, 1005, 1006, 1015:
		return false
	}
	return status < 1016 || status >= 3000
}
