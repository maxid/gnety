package gnety

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/dtls/v2"
)

// TLSConfig configures TLS for "tcp-tls" and "wss" networks.
type TLSConfig struct {
	CertFile          string
	KeyFile           string
	CAFile            string
	RequireClientCert bool
}

// Config configures a server.
type Config struct {
	// Network is "tcp", "tcp-tls", "udp", "ws" or "wss".
	Network string
	// Addr is the listen address, e.g. ":9000" or "127.0.0.1:0".
	Addr string
	// Path is the WebSocket path (default "/").
	Path string
	// Subprotocol is the required WebSocket subprotocol (optional).
	Subprotocol string
	// TLS configures "tcp-tls"/"wss" listeners.
	TLS TLSConfig
	// Codec is required for TCP.
	Codec Codec
	// DatagramCodec is required for UDP.
	DatagramCodec DatagramCodec
	// Handler receives lifecycle and messages.
	Handler Handler
	// IdleTimeout closes idle TCP connections / UDP peers (0 = disabled).
	IdleTimeout time.Duration
	// MaxFrameSize is passed to codecs (0 = codec default).
	MaxFrameSize int
	// RequestKeyPolicy enables request/response correlation on sessions.
	RequestKeyPolicy RequestKeyPolicy
	// DeliverResponsesToHandler also forwards correlated responses to Handler.
	DeliverResponsesToHandler bool
	// HeartbeatInterval sends a heartbeat message when > 0.
	HeartbeatInterval time.Duration
	// HeartbeatTimeout closes the session when no inbound message arrives.
	HeartbeatTimeout time.Duration
	// Heartbeat builds the heartbeat message (required when HeartbeatInterval > 0).
	Heartbeat func(*Session) any
	// RawHandler, when set, bypasses codec/session handling and receives the
	// established stream connection directly. Used by protocol libraries that
	// own their framing/state machine (e.g. MQTT broker). The handler must
	// return when the connection closes; the server tracks and closes it.
	RawHandler func(net.Conn)
}

// Server is a TCP or UDP transport with codec, sessions and lifecycle hooks.
type Server struct {
	cfg Config

	mu       sync.RWMutex
	ln       net.Listener
	pc       net.PacketConn
	sessions map[string]*Session
	conns    map[net.Conn]struct{}
	httpSrv  *http.Server
	done     chan struct{}
	wg       sync.WaitGroup
	started  bool
	seq      atomic.Uint64
}

// New validates cfg and returns a Server.
func New(cfg Config) (*Server, error) {
	switch cfg.Network {
	case "tcp", "tcp-tls", "ws", "wss":
		if cfg.Codec == nil && cfg.RawHandler == nil {
			return nil, errors.New("gnety: stream network requires Codec or RawHandler")
		}
		if (cfg.Network == "tcp-tls" || cfg.Network == "wss") && (cfg.TLS.CertFile == "" || cfg.TLS.KeyFile == "") {
			return nil, errors.New("gnety: TLS requires CertFile and KeyFile")
		}
	case "udp":
		if cfg.DatagramCodec == nil {
			return nil, errors.New("gnety: UDP requires DatagramCodec")
		}
		if cfg.RawHandler != nil {
			return nil, errors.New("gnety: RawHandler is not supported for UDP")
		}
	case "dtls":
		if cfg.DatagramCodec == nil {
			return nil, errors.New("gnety: DTLS requires DatagramCodec")
		}
		if cfg.TLS.CertFile == "" || cfg.TLS.KeyFile == "" {
			return nil, errors.New("gnety: DTLS requires CertFile and KeyFile")
		}
	default:
		return nil, fmt.Errorf("gnety: unsupported network %q", cfg.Network)
	}
	if cfg.Handler == nil {
		cfg.Handler = HandlerFuncs{}
	}
	if cfg.Path == "" {
		cfg.Path = "/"
	}
	return &Server{cfg: cfg, sessions: make(map[string]*Session), conns: make(map[net.Conn]struct{}), done: make(chan struct{})}, nil
}

// Start starts listening. It returns once the socket is ready.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("gnety: already started")
	}
	var err error
	switch s.cfg.Network {
	case "tcp":
		s.ln, err = net.Listen("tcp", s.cfg.Addr)
	case "tcp-tls":
		var tlsCfg *tls.Config
		tlsCfg, err = loadTLSConfig(s.cfg.TLS)
		if err == nil {
			s.ln, err = tls.Listen("tcp", s.cfg.Addr, tlsCfg)
		}
	case "udp":
		s.pc, err = net.ListenPacket("udp", s.cfg.Addr)
	case "dtls":
		var laddr *net.UDPAddr
		laddr, err = net.ResolveUDPAddr("udp", s.cfg.Addr)
		if err == nil {
			var dtlsCfg *dtls.Config
			dtlsCfg, err = loadDTLSConfig(s.cfg.TLS, false, s.cfg.Addr)
			if err == nil {
				s.ln, err = dtls.Listen("udp", laddr, dtlsCfg)
			}
		}
	case "ws", "wss":
		var ln net.Listener
		ln, err = net.Listen("tcp", s.cfg.Addr)
		if err == nil && s.cfg.Network == "wss" {
			var tlsCfg *tls.Config
			tlsCfg, err = loadTLSConfig(s.cfg.TLS)
			if err == nil {
				ln = tls.NewListener(ln, tlsCfg)
			} else {
				_ = ln.Close()
			}
		}
		if err == nil {
			s.ln = ln
			s.httpSrv = &http.Server{Handler: s.wsHandler(), ReadHeaderTimeout: 10 * time.Second}
		}
	}
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.started = true
	s.mu.Unlock()

	switch s.cfg.Network {
	case "tcp", "tcp-tls":
		s.wg.Add(1)
		go s.acceptTCP()
	case "dtls":
		s.wg.Add(1)
		go s.acceptDTLS()
	case "udp":
		s.wg.Add(1)
		go s.readUDP()
		if s.cfg.IdleTimeout > 0 {
			s.wg.Add(1)
			go s.cleanupUDP()
		}
	case "ws", "wss":
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			_ = s.httpSrv.Serve(s.ln)
		}()
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Stop(context.Background())
		case <-s.done:
		}
	}()
	return nil
}

// Addr returns the actual listen address.
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ln != nil {
		return s.ln.Addr().String()
	}
	if s.pc != nil {
		return s.pc.LocalAddr().String()
	}
	return ""
}

// SessionCount returns the current session count.
func (s *Server) SessionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// Stop closes the listener and all sessions.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = false
	close(s.done)
	if s.ln != nil {
		_ = s.ln.Close()
	}
	if s.pc != nil {
		_ = s.pc.Close()
	}
	httpSrv := s.httpSrv
	sessions := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	conns := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.mu.Unlock()

	if httpSrv != nil {
		_ = httpSrv.Close()
	}
	for _, sess := range sessions {
		_ = sess.Close()
	}
	for _, conn := range conns {
		_ = conn.Close()
	}
	wait := make(chan struct{})
	go func() { s.wg.Wait(); close(wait) }()
	select {
	case <-wait:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return errors.New("gnety: stop timeout")
	}
	return nil
}

func loadTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("gnety: load TLS key pair: %w", err)
	}
	out := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("gnety: read TLS CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("gnety: invalid TLS CA PEM")
		}
		out.ClientCAs = pool
		if cfg.RequireClientCert {
			out.ClientAuth = tls.RequireAndVerifyClientCert
		}
	}
	return out, nil
}

func loadDTLSConfig(cfg TLSConfig, client bool, addr string) (*dtls.Config, error) {
	out := &dtls.Config{}
	if cfg.CertFile != "" || cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("gnety: load DTLS key pair: %w", err)
		}
		out.Certificates = []tls.Certificate{cert}
	}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("gnety: read DTLS CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("gnety: invalid DTLS CA PEM")
		}
		if client {
			out.RootCAs = pool
			if host, _, err := net.SplitHostPort(addr); err == nil {
				out.ServerName = host
			}
		} else {
			out.ClientCAs = pool
			if cfg.RequireClientCert {
				out.ClientAuth = dtls.RequireAndVerifyClientCert
			}
		}
	}
	if client && cfg.CAFile == "" {
		out.InsecureSkipVerify = true // explicit dev-only fallback
	}
	return out, nil
}

func (s *Server) acceptDTLS() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			continue
		}
		s.mu.Lock()
		select {
		case <-s.done:
			s.mu.Unlock()
			_ = conn.Close()
			return
		default:
		}
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		sess := s.newDatagramSession(conn)
		s.addSession(sess)
		s.wg.Add(1)
		go s.serveDatagram(sess)
	}
}

func (s *Server) newDatagramSession(conn net.Conn) *Session {
	sess := s.newSession(conn)
	sess.datagram = true
	return sess
}

func (s *Server) serveDatagram(sess *Session) {
	defer s.wg.Done()
	var closeErr error
	defer func() {
		_ = sess.conn.Close()
		s.mu.Lock()
		delete(s.conns, sess.conn)
		s.mu.Unlock()
		s.removeSession(sess.ID)
		sess.closeOnceWith(closeErr)
	}()
	size := s.cfg.MaxFrameSize
	if size <= 0 {
		size = 64 * 1024
	}
	buf := make([]byte, size)
	for {
		if s.cfg.IdleTimeout > 0 {
			_ = sess.conn.SetReadDeadline(time.Now().Add(s.cfg.IdleTimeout))
		}
		n, err := sess.conn.Read(buf)
		if err != nil {
			closeErr = err
			return
		}
		msg, err := s.cfg.DatagramCodec.Decode(buf[:n])
		if err != nil {
			continue
		}
		sess.touch()
		if !sess.deliverResponse(msg) || s.cfg.DeliverResponsesToHandler {
			s.cfg.Handler.OnMessage(sess, msg)
		}
	}
}

func (s *Server) wsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != s.cfg.Path {
			http.NotFound(w, r)
			return
		}
		if s.cfg.Subprotocol != "" {
			got := strings.ToLower(r.Header.Get("Sec-WebSocket-Protocol"))
			if !strings.Contains(got, strings.ToLower(s.cfg.Subprotocol)) {
				http.Error(w, "missing WebSocket subprotocol", http.StatusBadRequest)
				return
			}
		}
		opts := &websocket.AcceptOptions{InsecureSkipVerify: true}
		if s.cfg.Subprotocol != "" {
			opts.Subprotocols = []string{s.cfg.Subprotocol}
		}
		c, err := websocket.Accept(w, r, opts)
		if err != nil {
			return
		}
		limit := int64(s.cfg.MaxFrameSize)
		if limit <= 0 {
			limit = 4 << 20
		}
		c.SetReadLimit(limit)
		conn := websocket.NetConn(context.Background(), c, websocket.MessageBinary)
		s.mu.Lock()
		select {
		case <-s.done:
			s.mu.Unlock()
			_ = conn.Close()
			return
		default:
		}
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		if s.cfg.RawHandler != nil {
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer func() {
					_ = conn.Close()
					s.mu.Lock()
					delete(s.conns, conn)
					s.mu.Unlock()
				}()
				s.cfg.RawHandler(conn)
			}()
			return
		}
		sess := s.newSession(conn)
		s.addSession(sess)
		s.wg.Add(1)
		go s.serveTCP(sess)
	})
}

func (s *Server) addSession(sess *Session) {
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.mu.Unlock()
	s.cfg.Handler.OnOpen(sess)
	sess.startHeartbeat()
}

func (s *Server) removeSession(id string) *Session {
	s.mu.Lock()
	sess := s.sessions[id]
	delete(s.sessions, id)
	s.mu.Unlock()
	return sess
}

func (s *Server) removeUDP(id string) {
	if sess := s.removeSession(id); sess != nil {
		sess.closeOnceWith(nil)
	}
}

func (s *Server) newSession(conn net.Conn) *Session {
	id := fmt.Sprintf("%s#%d", conn.RemoteAddr().String(), s.seq.Add(1))
	sess := &Session{ID: id, RemoteAddr: conn.RemoteAddr(), srv: s, conn: conn}
	sess.init()
	sess.touch()
	return sess
}

func (s *Server) acceptTCP() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			continue
		}
		s.mu.Lock()
		select {
		case <-s.done:
			s.mu.Unlock()
			_ = conn.Close()
			return
		default:
		}
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		if s.cfg.RawHandler != nil {
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer func() {
					_ = conn.Close()
					s.mu.Lock()
					delete(s.conns, conn)
					s.mu.Unlock()
				}()
				s.cfg.RawHandler(conn)
			}()
			continue
		}
		sess := s.newSession(conn)
		s.addSession(sess)
		s.wg.Add(1)
		go s.serveTCP(sess)
	}
}

func (s *Server) serveTCP(sess *Session) {
	defer s.wg.Done()
	var closeErr error
	defer func() {
		_ = sess.conn.Close()
		s.mu.Lock()
		delete(s.conns, sess.conn)
		s.mu.Unlock()
		s.removeSession(sess.ID)
		sess.closeOnceWith(closeErr)
	}()
	r := bufio.NewReader(sess.conn)
	for {
		if s.cfg.IdleTimeout > 0 {
			_ = sess.conn.SetReadDeadline(time.Now().Add(s.cfg.IdleTimeout))
		}
		msg, err := s.cfg.Codec.Decode(r)
		if err != nil {
			closeErr = err
			return
		}
		sess.touch()
		if !sess.deliverResponse(msg) || s.cfg.DeliverResponsesToHandler {
			s.cfg.Handler.OnMessage(sess, msg)
		}
	}
}

func (s *Server) readUDP() {
	defer s.wg.Done()
	size := s.cfg.MaxFrameSize
	if size <= 0 {
		size = 64 * 1024
	}
	buf := make([]byte, size)
	for {
		n, addr, err := s.pc.ReadFrom(buf)
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			continue
		}
		data := append([]byte(nil), buf[:n]...)
		sess := s.udpSession(addr)
		msg, err := s.cfg.DatagramCodec.Decode(data)
		if err != nil {
			continue
		}
		sess.touch()
		if !sess.deliverResponse(msg) || s.cfg.DeliverResponsesToHandler {
			s.cfg.Handler.OnMessage(sess, msg)
		}
	}
}

func (s *Server) udpSession(addr net.Addr) *Session {
	s.mu.Lock()
	id := addr.String()
	if sess, ok := s.sessions[id]; ok {
		s.mu.Unlock()
		return sess
	}
	sess := &Session{ID: id, RemoteAddr: addr, srv: s}
	sess.init()
	sess.touch()
	s.sessions[id] = sess
	s.mu.Unlock()
	s.cfg.Handler.OnOpen(sess)
	sess.startHeartbeat()
	return sess
}

func (s *Server) cleanupUDP() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.cfg.IdleTimeout / 2)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-s.cfg.IdleTimeout).UnixNano()
			s.mu.Lock()
			var expired []*Session
			for id, sess := range s.sessions {
				if sess.lastSeen.Load() < cutoff {
					expired = append(expired, sess)
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
			for _, sess := range expired {
				sess.closeOnceWith(errors.New("gnety: udp session idle timeout"))
			}
		}
	}
}
