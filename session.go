package gnety

import (
	"bytes"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ErrSessionClosed is returned when sending on a closed session.
var ErrSessionClosed = errors.New("gnety: session closed")

// Handler receives session lifecycle and decoded messages.
type Handler interface {
	OnOpen(*Session)
	OnMessage(*Session, any)
	OnClose(*Session, error)
}

// HandlerFuncs adapts funcs to Handler.
type HandlerFuncs struct {
	Open    func(*Session)
	Message func(*Session, any)
	Close   func(*Session, error)
}

func (h HandlerFuncs) OnOpen(s *Session) {
	if h.Open != nil {
		h.Open(s)
	}
}
func (h HandlerFuncs) OnMessage(s *Session, msg any) {
	if h.Message != nil {
		h.Message(s, msg)
	}
}
func (h HandlerFuncs) OnClose(s *Session, err error) {
	if h.Close != nil {
		h.Close(s, err)
	}
}

// Session is one TCP connection or one UDP peer.
type Session struct {
	ID         string
	RemoteAddr net.Addr

	srv  *Server
	conn net.Conn

	writeMu sync.Mutex
	attrs   sync.Map
	closed  atomic.Bool

	closeOnce sync.Once
	lastSeen  atomic.Int64
}

// Send encodes and writes a message to the peer.
func (s *Session) Send(msg any) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.conn != nil {
		var buf bytes.Buffer
		if s.srv.cfg.Codec == nil {
			return errors.New("gnety: no TCP codec configured")
		}
		if err := s.srv.cfg.Codec.Encode(&buf, msg); err != nil {
			return err
		}
		_, err := s.conn.Write(buf.Bytes())
		return err
	}
	if s.srv.pc == nil || s.srv.cfg.DatagramCodec == nil {
		return errors.New("gnety: no UDP datagram codec configured")
	}
	b, err := s.srv.cfg.DatagramCodec.Encode(msg)
	if err != nil {
		return err
	}
	_, err = s.srv.pc.WriteTo(b, s.RemoteAddr)
	return err
}

// Close closes the session (TCP) or forgets the UDP peer.
func (s *Session) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	if s.conn != nil {
		return s.conn.Close()
	}
	s.srv.removeUDP(s.ID)
	return nil
}

// Closed reports whether the session is closed.
func (s *Session) Closed() bool { return s.closed.Load() }

// Set stores a session attribute.
func (s *Session) Set(key string, value any) { s.attrs.Store(key, value) }

// Get loads a session attribute.
func (s *Session) Get(key string) (any, bool) { return s.attrs.Load(key) }

// Delete removes an attribute.
func (s *Session) Delete(key string) { s.attrs.Delete(key) }

// Touch updates the last-seen time (used by UDP idle cleanup).
func (s *Session) Touch() { s.lastSeen.Store(time.Now().UnixNano()) }

func (s *Session) touch() { s.Touch() }

func (s *Session) closeOnceWith(err error) {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.srv.cfg.Handler.OnClose(s, err)
	})
}
