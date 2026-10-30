package websocket

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

const defaultMaxMessageSize = 4 << 20

// Conn is the message/stream adapter in gnety's transport pipeline.
// The frame codec is in websocket_frame.go; handshakes are in
// websocket_handshake.go. Protocol state machines remain above RawHandler.
type Conn struct {
	raw         net.Conn
	reader      *bufio.Reader
	writer      *bufio.Writer
	maskWrites  bool
	maxMessage  atomic.Uint64
	subprotocol string

	readMu         sync.Mutex
	frame          frameHeader
	frameActive    bool
	frameRemaining uint64
	frameOffset    uint64
	inMessage      bool
	messageOpcode  byte
	messageSize    uint64
	textUTF8       utf8Validator
	readErr        error

	writeMu   sync.Mutex
	closeOnce sync.Once
	doneOnce  sync.Once
	closeErr  error
	closeSent atomic.Bool
	closed    atomic.Bool
	done      chan struct{}

	activePingsMu sync.Mutex
	activePings   map[string]chan struct{}
}

func newConn(raw net.Conn, rw *bufio.ReadWriter, maskWrites bool, maxFrameSize int, subprotocol string) *Conn {
	if maxFrameSize <= 0 {
		maxFrameSize = defaultMaxMessageSize
	}
	c := &Conn{
		raw: raw, reader: rw.Reader, writer: rw.Writer,
		maskWrites: maskWrites, subprotocol: subprotocol,
		done:        make(chan struct{}),
		activePings: make(map[string]chan struct{}),
	}
	c.maxMessage.Store(uint64(maxFrameSize))
	return c
}

func (c *Conn) Read(p []byte) (int, error) {
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
				applyMask(chunk, c.frame.maskingKey, c.frameOffset)
			}
			c.frameOffset += uint64(n)
			c.frameRemaining -= uint64(n)
			if c.messageOpcode == textFrame && !c.textUTF8.check(chunk) {
				return n, c.setReadFailure(&protocolError{status: 1007, text: "invalid UTF-8 text message"})
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

func (c *Conn) readDataFrame() error {
	for {
		h, err := readFrameHeader(c.reader)
		if err != nil {
			return err
		}
		// Clients mask writes and require unmasked server frames; servers
		// require masked client frames and never mask their own writes.
		if h.masked == c.maskWrites {
			return newProtocolError("invalid masking direction")
		}
		switch h.opcode {
		case pingFrame, pongFrame, closeFrame:
			payload, err := c.readControlPayload(h)
			if err != nil {
				return err
			}
			switch h.opcode {
			case pingFrame:
				if err := c.writeControl(pongFrame, payload); err != nil {
					return err
				}
			case pongFrame:
				c.deliverPong(payload)
			case closeFrame:
				ce := CloseError{Code: StatusCodeNormalClosure}
				if len(payload) == 1 {
					return newProtocolError("close payload has one byte")
				}
				if len(payload) >= 2 {
					ce.Code = StatusCode(binary.BigEndian.Uint16(payload[:2]))
					ce.Reason = string(payload[2:])
					if !validCloseStatus(uint16(ce.Code)) || !utf8.ValidString(ce.Reason) {
						return newProtocolError("invalid close status or reason")
					}
				}
				if !c.closeSent.Load() {
					_ = c.writeClose(uint16(ce.Code))
				}
				c.doneOnce.Do(func() { close(c.done) })
				return &ce
			}
			continue
		case continuationFrame:
			if !c.inMessage {
				return newProtocolError("continuation without an active message")
			}
		case textFrame, binaryFrame:
			if c.inMessage {
				return newProtocolError("new data frame during a fragmented message")
			}
			c.inMessage = true
			c.messageOpcode = h.opcode
			c.messageSize = 0
			c.textUTF8.reset()
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

func (c *Conn) readControlPayload(h frameHeader) ([]byte, error) {
	payload := make([]byte, int(h.length))
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return nil, err
	}
	if h.masked {
		applyMask(payload, h.maskingKey, 0)
	}
	return payload, nil
}

func (c *Conn) finishDataFrame() error {
	c.frameActive = false
	if !c.frame.fin {
		return nil
	}
	if c.messageOpcode == textFrame && !c.textUTF8.finish() {
		return &protocolError{status: 1007, text: "incomplete UTF-8 text message"}
	}
	c.inMessage = false
	c.messageOpcode = 0
	return nil
}

func (c *Conn) setReadFailure(err error) error {
	if c.readErr != nil {
		return c.readErr
	}
	var protocolErr *protocolError
	switch {
	case errors.Is(err, ErrFrameTooLarge):
		_ = c.writeClose(1009)
	case errors.As(err, &protocolErr):
		_ = c.writeClose(protocolErr.status)
	}
	c.readErr = err
	return err
}

func (c *Conn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	if err := writeFrame(c.writer, c.maskWrites, true, binaryFrame, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *Conn) writeControl(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed.Load() {
		return net.ErrClosed
	}
	return writeFrame(c.writer, c.maskWrites, true, opcode, payload)
}

func (c *Conn) writeClose(status uint16) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed.Load() || c.closeSent.Load() {
		return nil
	}
	payload := []byte{byte(status >> 8), byte(status)}
	err := writeFrame(c.writer, c.maskWrites, true, closeFrame, payload)
	c.closeSent.Store(true)
	return err
}

func (c *Conn) Close() error {
	return c.CloseWithStatus(StatusCodeNormalClosure, "")
}

// WebSocketConn is the public WebSocket net.Conn adapter.
type WebSocketConn = Conn

// StatusCode is a WebSocket close status code.
type StatusCode uint16

const (
	StatusCodeNormalClosure   StatusCode = 1000
	StatusCodeGoingAway       StatusCode = 1001
	StatusCodeProtocolError   StatusCode = 1002
	StatusCodeUnsupportedData StatusCode = 1003
	StatusCodeInvalidPayload  StatusCode = 1007
	StatusCodePolicyViolation StatusCode = 1008
	StatusCodeMessageTooBig   StatusCode = 1009
	StatusCodeInternalError   StatusCode = 1011
)

// CloseError reports a peer close status.
type CloseError struct {
	Code   StatusCode
	Reason string
}

func (e *CloseError) Error() string {
	return fmt.Sprintf("gnety: websocket closed: %d %s", e.Code, e.Reason)
}

// Unwrap preserves io.EOF compatibility for net.Conn-style readers.
func (e *CloseError) Unwrap() error { return io.EOF }

// CloseStatus extracts the peer close status from an error.
func CloseStatus(err error) StatusCode {
	var ce *CloseError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return 0
}

// CloseWithStatus performs the close handshake frame write and closes the raw
// connection. The reason is truncated to the RFC 6455 control payload limit.
func (c *Conn) CloseWithStatus(status StatusCode, reason string) error {
	c.closeOnce.Do(func() {
		c.writeMu.Lock()
		var closeFrameErr error
		if !c.closed.Load() && !c.closeSent.Load() {
			if !validCloseStatus(uint16(status)) {
				status = StatusCodeNormalClosure
			}
			if !utf8.ValidString(reason) {
				reason = ""
			}
			if len(reason) > 123 {
				reason = reason[:123]
			}
			payload := []byte{byte(status >> 8), byte(status)}
			payload = append(payload, reason...)
			closeFrameErr = writeFrame(c.writer, c.maskWrites, true, closeFrame, payload)
			c.closeSent.Store(true)
		}
		c.writeMu.Unlock()

		// Best-effort peer close wait: only when no reader owns readMu. An active
		// reader already consumes control frames and will close the transport.
		if c.readMu.TryLock() {
			_ = c.raw.SetReadDeadline(time.Now().Add(time.Second))
			for {
				if err := c.readDataFrame(); err != nil {
					break
				}
			}
			_ = c.raw.SetReadDeadline(time.Time{})
			c.readMu.Unlock()
		}

		c.closed.Store(true)
		c.doneOnce.Do(func() { close(c.done) })
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
func (c *Conn) SetReadLimit(n int64) {
	if n < 0 {
		c.maxMessage.Store(^uint64(0))
		return
	}
	if n == 0 {
		c.maxMessage.Store(defaultMaxMessageSize)
		return
	}
	c.maxMessage.Store(uint64(n))
}

// Subprotocol returns the negotiated WebSocket subprotocol.
func (c *Conn) Subprotocol() string { return c.subprotocol }

// Ping sends a ping and waits for the matching pong. A concurrent Read is
// required to process the pong.
func (c *Conn) Ping(ctx context.Context, payload []byte) error {
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
	if err := c.writeControl(pingFrame, []byte(key)); err != nil {
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

func (c *Conn) deliverPong(payload []byte) {
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

func (c *Conn) LocalAddr() net.Addr                { return c.raw.LocalAddr() }
func (c *Conn) RemoteAddr() net.Addr               { return c.raw.RemoteAddr() }
func (c *Conn) SetDeadline(t time.Time) error      { return c.raw.SetDeadline(t) }
func (c *Conn) SetReadDeadline(t time.Time) error  { return c.raw.SetReadDeadline(t) }
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.raw.SetWriteDeadline(t) }

func validCloseStatus(status uint16) bool {
	if status < 1000 || status >= 5000 {
		return false
	}
	switch status {
	case 1004, 1005, 1006, 1015:
		return false
	}
	return status < 1016 || status >= 3000
}
