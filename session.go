package gnety

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	ws "github.com/maxid/gnety/codec/http/websocket"
)

// ErrSessionClosed is returned when sending on a closed session.
var ErrSessionClosed = errors.New("gnety: session closed")

// ErrNoRequestPolicy is returned when Request is called without a policy.
var ErrNoRequestPolicy = errors.New("gnety: no RequestKeyPolicy configured")

// RequestKeyPolicy extracts correlation keys from requests and responses.
type RequestKeyPolicy interface {
	RequestKey(msg any) (string, bool)
	ResponseKey(msg any) (string, bool)
}

// RequestKeyPolicyFuncs adapts functions to RequestKeyPolicy.
type RequestKeyPolicyFuncs struct {
	Request  func(any) (string, bool)
	Response func(any) (string, bool)
}

func (p RequestKeyPolicyFuncs) RequestKey(msg any) (string, bool) {
	if p.Request == nil {
		return "", false
	}
	return p.Request(msg)
}

func (p RequestKeyPolicyFuncs) ResponseKey(msg any) (string, bool) {
	if p.Response == nil {
		return "", false
	}
	return p.Response(msg)
}

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

	srv      *Server
	conn     net.Conn
	datagram bool

	writeMu sync.Mutex
	attrs   sync.Map
	closed  atomic.Bool

	closeOnce sync.Once
	lastSeen  atomic.Int64

	pendingMu sync.Mutex
	pending   map[string]chan any
	hbDone    chan struct{}
}

func (s *Session) init() {
	s.pending = make(map[string]chan any)
	s.hbDone = make(chan struct{})
}

// Send encodes and writes a message to the peer.
func (s *Session) Send(msg any) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.conn != nil {
		if s.datagram {
			if s.srv.cfg.DatagramCodec == nil {
				return errors.New("gnety: no datagram codec configured")
			}
			raw, err := s.srv.cfg.DatagramCodec.Encode(msg)
			if err != nil {
				return err
			}
			_, err = s.conn.Write(raw)
			return err
		}
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

// WebSocket returns the underlying WebSocket connection when this session is
// carried over ws/wss.
func (s *Session) WebSocket() (*ws.Conn, bool) {
	w, ok := s.conn.(*ws.Conn)
	return w, ok
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

// Request sends req and waits for a correlated response. A
// RequestKeyPolicy must be configured on the server.
func (s *Session) Request(ctx context.Context, req any) (any, error) {
	policy := s.srv.cfg.RequestKeyPolicy
	if policy == nil {
		return nil, ErrNoRequestPolicy
	}
	key, ok := policy.RequestKey(req)
	if !ok || key == "" {
		return nil, errors.New("gnety: RequestKeyPolicy returned no key")
	}
	ch := make(chan any, 1)
	s.pendingMu.Lock()
	if s.pending == nil {
		s.pending = make(map[string]chan any)
	}
	if _, exists := s.pending[key]; exists {
		s.pendingMu.Unlock()
		return nil, errors.New("gnety: duplicate pending request key")
	}
	s.pending[key] = ch
	s.pendingMu.Unlock()
	defer func() {
		s.pendingMu.Lock()
		if current, ok := s.pending[key]; ok && current == ch {
			delete(s.pending, key)
		}
		s.pendingMu.Unlock()
	}()
	if err := s.Send(req); err != nil {
		return nil, err
	}
	select {
	case v, ok := <-ch:
		if !ok {
			return nil, ErrSessionClosed
		}
		return v, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// deliverResponse completes a pending request when msg is a correlated response.
func (s *Session) deliverResponse(msg any) bool {
	policy := s.srv.cfg.RequestKeyPolicy
	if policy == nil {
		return false
	}
	key, ok := policy.ResponseKey(msg)
	if !ok || key == "" {
		return false
	}
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	ch, exists := s.pending[key]
	if !exists {
		return false
	}
	delete(s.pending, key)
	select {
	case ch <- msg:
	default:
	}
	return true
}

func (s *Session) startHeartbeat() {
	interval := s.srv.cfg.HeartbeatInterval
	if interval <= 0 {
		return
	}
	s.hbDone = make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.hbDone:
				return
			case <-ticker.C:
				if timeout := s.srv.cfg.HeartbeatTimeout; timeout > 0 {
					last := time.Unix(0, s.lastSeen.Load())
					if time.Since(last) > timeout {
						_ = s.Close()
						return
					}
				}
				if s.srv.cfg.Heartbeat == nil {
					continue
				}
				if err := s.Send(s.srv.cfg.Heartbeat(s)); err != nil {
					_ = s.Close()
					return
				}
			}
		}
	}()
}

func (s *Session) closeOnceWith(err error) {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		if s.hbDone != nil {
			close(s.hbDone)
		}
		s.pendingMu.Lock()
		for key, ch := range s.pending {
			close(ch)
			delete(s.pending, key)
		}
		s.pendingMu.Unlock()
		s.srv.cfg.Handler.OnClose(s, err)
	})
}
