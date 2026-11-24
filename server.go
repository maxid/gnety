package gnety

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/dtls/v2"

	"github.com/maxid/glimit"
	ws "github.com/maxid/gnety/codec/http/websocket"
)

// TLSConfig configures TLS for "tcp-tls" and "wss" networks.
type TLSConfig struct {
	CertFile          string
	KeyFile           string
	CAFile            string
	RequireClientCert bool
	// InsecureSkipVerify is only valid for DTLS clients and must be explicit.
	// Production should provide CAFile instead.
	InsecureSkipVerify bool
}

// Config configures a server.
type Config struct {
	// Network is "tcp", "tcp-tls", "udp", "ws" or "wss".
	Network string
	// Addr is the listen address, e.g. ":9000" or "127.0.0.1:0". It is ignored
	// when Listener or PacketConn is supplied.
	Addr string
	// Listener, when non-nil, is used instead of opening a stream listener.
	// It is transport-ready: for tcp-tls/wss it must already be a TLS listener.
	// gnety does not wrap it in TLS. Ownership transfers to the Server, which
	// closes it in Stop.
	Listener net.Listener
	// PacketConn, when non-nil, is used instead of opening a UDP listener for
	// Network "udp". Ownership transfers to the Server.
	PacketConn net.PacketConn
	// Socket configures listener and accepted-connection socket options.
	Socket SocketOptions
	// Path is the WebSocket path (default "/").
	Path string
	// Subprotocol is the required WebSocket subprotocol (optional).
	Subprotocol string
	// OriginPatterns authorizes cross-origin WebSocket handshakes. Each pattern
	// is matched case-insensitively with path.Match against the Origin host (or
	// scheme://host when the pattern contains "://"). Same-host Origin is always
	// allowed. Empty means cross-origin requests are rejected.
	OriginPatterns []string
	// AllowAnyOrigin disables Origin verification. Use only for non-browser
	// device gateways where Origin is not a security boundary.
	AllowAnyOrigin bool
	// TLS configures "tcp-tls"/"wss" listeners from files.
	TLS TLSConfig
	// TLSConfig, when non-nil, replaces the file-based TLS configuration for
	// "tcp-tls" and "wss". The server uses it as given; the caller keeps
	// ownership.
	TLSConfig *tls.Config
	// DTLSConfig, when non-nil, replaces the file-based DTLS configuration for
	// "dtls". The server uses it as given; the caller keeps ownership.
	DTLSConfig *dtls.Config
	// Codec is required for TCP.
	Codec Codec
	// DatagramCodec is required for UDP.
	DatagramCodec DatagramCodec
	// Handler receives lifecycle and messages.
	Handler Handler
	// Metrics receives transport events. Nil drops them (NoopMetrics).
	Metrics Metrics
	// IdleTimeout closes idle TCP connections / UDP peers (0 = disabled).
	IdleTimeout time.Duration
	// MaxFrameSize is passed to codecs (0 = codec default).
	MaxFrameSize int
	// MaxSessions limits concurrent connections/peers (0 = unlimited).
	MaxSessions int
	// Limiter, when set, is consulted in addition to MaxSessions and the
	// per-session limits: the connection count and accept rate at accept time,
	// the byte and frame rates on a session. Both layers must admit; nil keeps
	// the behaviour unchanged. See github.com/maxid/glimit.
	Limiter glimit.Limiter
	// Rejecter, when set, may write a protocol-level refusal before a
	// connection or session is closed for a limit. Nil closes without a frame.
	Rejecter Rejecter
	// MaxPendingRequests limits per-session Request correlation entries
	// (0 = unlimited).
	MaxPendingRequests int
	// MaxQueuedWrites bounds how many goroutines may wait to write on one
	// session; beyond that Session.Send fails with ErrWriteBusy instead of
	// queueing without bound. 0 uses DefaultMaxQueuedWrites, negative disables
	// the gate.
	MaxQueuedWrites int
	// WriteTimeout bounds each Session/Client write (0 = no deadline).
	WriteTimeout time.Duration
	// ShutdownTimeout bounds Stop; 0 uses a 5s compatibility default.
	ShutdownTimeout time.Duration
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
	// limits is the credit a raw connection holds; sessions keep theirs on the
	// session so it is returned exactly once, on close.
	limits  map[net.Conn][]glimit.Key
	httpSrv *http.Server
	done    chan struct{}
	wg      sync.WaitGroup
	started bool
	seq     atomic.Uint64
}

// New validates cfg and returns a Server.
func New(cfg Config) (*Server, error) {
	if cfg.Listener != nil && cfg.PacketConn != nil {
		return nil, errors.New("gnety: Listener and PacketConn are mutually exclusive")
	}
	switch cfg.Network {
	case "tcp", "tcp-tls", "ws", "wss":
		if cfg.PacketConn != nil {
			return nil, errors.New("gnety: PacketConn is only valid for udp")
		}
		if cfg.Codec == nil && cfg.RawHandler == nil {
			return nil, errors.New("gnety: stream network requires Codec or RawHandler")
		}
		if (cfg.Network == "tcp-tls" || cfg.Network == "wss") && cfg.Listener == nil && cfg.TLSConfig == nil &&
			(cfg.TLS.CertFile == "" || cfg.TLS.KeyFile == "") {
			return nil, errors.New("gnety: TLS requires TLSConfig or CertFile and KeyFile")
		}
	case "udp":
		if cfg.Listener != nil {
			return nil, errors.New("gnety: Listener is only valid for stream networks")
		}
		if cfg.DatagramCodec == nil {
			return nil, errors.New("gnety: UDP requires DatagramCodec")
		}
		if cfg.RawHandler != nil {
			return nil, errors.New("gnety: RawHandler is not supported for UDP")
		}
	case "dtls":
		if cfg.PacketConn != nil {
			return nil, errors.New("gnety: PacketConn is only valid for udp")
		}
		if cfg.DatagramCodec == nil {
			return nil, errors.New("gnety: DTLS requires DatagramCodec")
		}
		if cfg.Listener == nil && cfg.DTLSConfig == nil && (cfg.TLS.CertFile == "" || cfg.TLS.KeyFile == "") {
			return nil, errors.New("gnety: DTLS requires DTLSConfig or CertFile and KeyFile")
		}
	default:
		return nil, fmt.Errorf("gnety: unsupported network %q", cfg.Network)
	}
	if err := cfg.Socket.validate(cfg.Network); err != nil {
		return nil, err
	}
	if cfg.Handler == nil {
		cfg.Handler = HandlerFuncs{}
	}
	if cfg.Path == "" {
		cfg.Path = "/"
	}
	if cfg.Network == "ws" || cfg.Network == "wss" {
		if cfg.Path[0] != '/' {
			return nil, errors.New("gnety: WebSocket Path must start with /")
		}
		if cfg.Subprotocol != "" && !ws.ValidSubprotocol(cfg.Subprotocol) {
			return nil, errors.New("gnety: invalid WebSocket subprotocol")
		}
	}
	return &Server{cfg: cfg, sessions: make(map[string]*Session), conns: make(map[net.Conn]struct{}),
		limits: make(map[net.Conn][]glimit.Key), done: make(chan struct{})}, nil
}

// Start starts listening. It returns once the socket is ready.
func (s *Server) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("gnety: already started")
	}
	var err error
	if s.cfg.Listener != nil {
		s.ln = s.cfg.Socket.wrapListener(s.cfg.Listener)
	}
	if s.cfg.PacketConn != nil {
		if applyErr := s.cfg.Socket.applyPacketConn(s.cfg.PacketConn); applyErr != nil {
			err = applyErr
		} else {
			s.pc = s.cfg.PacketConn
		}
	}
	if err == nil {
		switch s.cfg.Network {
		case "tcp":
			if s.ln == nil {
				s.ln, err = s.listenTCP(ctx)
			}
		case "tcp-tls":
			if s.ln == nil {
				tlsCfg := s.cfg.TLSConfig
				if tlsCfg == nil {
					tlsCfg, err = loadTLSConfig(s.cfg.TLS)
				}
				if err == nil {
					var ln net.Listener
					ln, err = s.listenTCP(ctx)
					if err == nil {
						s.ln = tls.NewListener(ln, tlsCfg)
					}
				}
			}
		case "udp":
			if s.pc == nil {
				var pc net.PacketConn
				pc, err = s.cfg.Socket.listenConfig().ListenPacket(ctx, "udp", s.cfg.Addr)
				if err == nil {
					if applyErr := s.cfg.Socket.applyPacketConn(pc); applyErr != nil {
						_ = pc.Close()
						err = applyErr
					} else {
						s.pc = pc
					}
				}
			}
		case "dtls":
			if s.ln == nil {
				var laddr *net.UDPAddr
				laddr, err = net.ResolveUDPAddr("udp", s.cfg.Addr)
				if err == nil {
					dtlsCfg := s.cfg.DTLSConfig
					if dtlsCfg == nil {
						dtlsCfg, err = loadDTLSConfig(s.cfg.TLS, false, s.cfg.Addr)
					}
					if err == nil {
						s.ln, err = dtls.Listen("udp", laddr, dtlsCfg)
					}
				}
			}
		case "ws", "wss":
			if s.ln == nil {
				var ln net.Listener
				ln, err = s.listenTCP(ctx)
				if err == nil && s.cfg.Network == "wss" {
					tlsCfg := s.cfg.TLSConfig
					if tlsCfg == nil {
						tlsCfg, err = loadTLSConfig(s.cfg.TLS)
					}
					if err == nil {
						ln = tls.NewListener(ln, tlsCfg)
					} else {
						_ = ln.Close()
					}
				}
				s.ln = ln
			}
			if err == nil {
				s.httpSrv = &http.Server{Handler: s.wsHandler(), ReadHeaderTimeout: 10 * time.Second}
			}
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

// listenTCP opens a TCP listener with the configured socket options and wraps
// accepted connections so TCP-level options are applied before any TLS or
// WebSocket layer wraps them.
func (s *Server) listenTCP(ctx context.Context) (net.Listener, error) {
	ln, err := s.cfg.Socket.listenConfig().Listen(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return nil, err
	}
	return s.cfg.Socket.wrapListener(ln), nil
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
		sess.closeWith(ReasonShutdown)
	}
	for _, conn := range conns {
		_ = conn.Close()
	}
	wait := make(chan struct{})
	go func() { s.wg.Wait(); close(wait) }()
	timeout := s.cfg.ShutdownTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	select {
	case <-wait:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(timeout):
		return ErrShutdownTimeout
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
		if !cfg.InsecureSkipVerify {
			return nil, errors.New("gnety: DTLS client requires CAFile or explicit InsecureSkipVerify")
		}
		out.InsecureSkipVerify = true
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
		if max := s.cfg.MaxSessions; max > 0 && len(s.conns) >= max {
			s.mu.Unlock()
			s.metric().SessionRejected(s.transport(), ReasonMaxSessions)
			_ = conn.Close()
			continue
		}
		// The limiter is consulted under mu: it takes no server lock, and
		// admission must stay one step with registration.
		d, credit := s.admit(conn.RemoteAddr())
		if !d.Allow {
			s.mu.Unlock()
			s.reject(conn, d)
			continue
		}
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		sess := s.newDatagramSession(conn)
		sess.limitKeys = credit
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
		if d := s.admitBytes(sess, glimit.DirIn, n); !d.Allow {
			closeErr = ErrRateLimited
			s.deny(sess, d)
			return
		}
		msg, err := s.cfg.DatagramCodec.Decode(buf[:n])
		if err != nil {
			continue
		}
		if d := s.admitFrame(sess); !d.Allow {
			closeErr = ErrRateLimited
			s.deny(sess, d)
			return
		}
		s.metric().MessageIn(s.transport())
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
		if s.cfg.Subprotocol != "" && !ws.HasSubprotocol(r.Header, s.cfg.Subprotocol) {
			http.Error(w, "missing WebSocket subprotocol", http.StatusBadRequest)
			return
		}
		conn, err := ws.Accept(w, r, ws.Options{
			Subprotocol:    s.cfg.Subprotocol,
			OriginPatterns: s.cfg.OriginPatterns,
			AllowAnyOrigin: s.cfg.AllowAnyOrigin,
			MaxMessageSize: s.cfg.MaxFrameSize,
		})
		if err != nil {
			return
		}
		s.mu.Lock()
		select {
		case <-s.done:
			s.mu.Unlock()
			_ = conn.Close()
			return
		default:
		}
		if max := s.cfg.MaxSessions; max > 0 && len(s.conns) >= max {
			s.mu.Unlock()
			s.metric().SessionRejected(s.transport(), ReasonMaxSessions)
			_ = conn.Close()
			return
		}
		d, credit := s.admit(conn.RemoteAddr())
		if !d.Allow {
			s.mu.Unlock()
			s.reject(conn, d)
			return
		}
		s.conns[conn] = struct{}{}
		if s.cfg.RawHandler != nil {
			// No session object exists on the raw path, so the connection
			// itself carries the credit.
			s.limits[conn] = credit
		}
		s.mu.Unlock()
		if s.cfg.RawHandler != nil {
			s.wg.Add(1)
			defer s.wg.Done()
			defer func() {
				_ = conn.Close()
				s.forgetConn(conn)
			}()
			s.cfg.RawHandler(conn)
			return
		}
		sess := s.newSession(conn)
		sess.limitKeys = credit
		s.addSession(sess)
		s.wg.Add(1)
		s.serveTCP(sess)
	})
}

func (s *Server) addSession(sess *Session) {
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.mu.Unlock()
	s.metric().SessionOpen(s.transport())
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
		if max := s.cfg.MaxSessions; max > 0 && len(s.conns) >= max {
			s.mu.Unlock()
			s.metric().SessionRejected(s.transport(), ReasonMaxSessions)
			_ = conn.Close()
			continue
		}
		d, credit := s.admit(conn.RemoteAddr())
		if !d.Allow {
			s.mu.Unlock()
			s.reject(conn, d)
			continue
		}
		s.conns[conn] = struct{}{}
		if s.cfg.RawHandler != nil {
			s.limits[conn] = credit
		}
		s.mu.Unlock()
		if s.cfg.RawHandler != nil {
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer func() {
					_ = conn.Close()
					s.forgetConn(conn)
				}()
				s.cfg.RawHandler(conn)
			}()
			continue
		}
		sess := s.newSession(conn)
		sess.limitKeys = credit
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
	meter := &countingReader{r: sess.conn}
	r := bufio.NewReader(meter)
	for {
		if s.cfg.IdleTimeout > 0 {
			_ = sess.conn.SetReadDeadline(time.Now().Add(s.cfg.IdleTimeout))
		}
		msg, err := s.cfg.Codec.Decode(r)
		if err != nil {
			closeErr = err
			return
		}
		if d := s.admitBytes(sess, glimit.DirIn, meter.take()); !d.Allow {
			closeErr = ErrRateLimited
			s.deny(sess, d)
			return
		}
		if d := s.admitFrame(sess); !d.Allow {
			closeErr = ErrRateLimited
			s.deny(sess, d)
			return
		}
		s.metric().MessageIn(s.transport())
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
		if sess == nil {
			continue
		}
		if d := s.admitBytes(sess, glimit.DirIn, n); !d.Allow {
			s.deny(sess, d)
			continue
		}
		msg, err := s.cfg.DatagramCodec.Decode(data)
		if err != nil {
			continue
		}
		if d := s.admitFrame(sess); !d.Allow {
			s.deny(sess, d)
			continue
		}
		s.metric().MessageIn(s.transport())
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
	if s.cfg.MaxSessions > 0 && len(s.sessions) >= s.cfg.MaxSessions {
		s.mu.Unlock()
		s.metric().SessionRejected(s.transport(), ReasonMaxSessions)
		return nil
	}
	d, credit := s.admit(addr)
	if !d.Allow {
		s.mu.Unlock()
		s.metric().SessionRejected(s.transport(), limitReason(d.Reason))
		return nil
	}
	sess := &Session{ID: id, RemoteAddr: addr, srv: s, limitKeys: credit}
	sess.init()
	sess.touch()
	s.sessions[id] = sess
	s.mu.Unlock()
	s.metric().SessionOpen(s.transport())
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
				sess.reason.Store(ReasonIdleTimeout)
				sess.closeOnceWith(errors.New("gnety: udp session idle timeout"))
			}
		}
	}
}

// DefaultMaxQueuedWrites is how many goroutines may wait to write on one
// session when Config.MaxQueuedWrites is zero.
const DefaultMaxQueuedWrites = 8

// metric returns the configured Metrics, or a no-op implementation.
func (s *Server) metric() Metrics {
	if s.cfg.Metrics == nil {
		return NoopMetrics{}
	}
	return s.cfg.Metrics
}

// transport is the configured network name, the only transport label emitted.
func (s *Server) transport() string { return s.cfg.Network }

// maxQueuedWrites resolves the write gate size. A negative value disables it.
func (s *Server) maxQueuedWrites() int {
	switch n := s.cfg.MaxQueuedWrites; {
	case n < 0:
		return 0
	case n == 0:
		return DefaultMaxQueuedWrites
	default:
		return n
	}
}

// countingReader forwards reads and remembers how many bytes passed through, so
// a codec that reads ahead is still metered.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// take returns the bytes read since the previous call.
func (c *countingReader) take() int {
	n := c.n
	c.n = 0
	return n
}

// peerID is the limiter identity of a remote address: the host part, so every
// connection from one peer shares a key. A port never becomes an identity.
func peerID(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

// connectionKeys are the limiter keys a new session charges: the connection
// gauge and the accept rate, each globally and per peer.
func (s *Server) connectionKeys(addr net.Addr) []glimit.Key {
	peer := peerID(addr)
	return []glimit.Key{
		{Kind: glimit.KindConnections, Scope: glimit.ScopeGlobal},
		{Kind: glimit.KindConnections, Scope: glimit.ScopePeer, ID: peer},
		{Kind: glimit.KindRequests, Scope: glimit.ScopeGlobal, Direction: glimit.DirIn},
		{Kind: glimit.KindRequests, Scope: glimit.ScopePeer, ID: peer, Direction: glimit.DirIn},
	}
}

// admit charges the connection keys for addr and returns the credit the caller
// must hand back on close. A denied admission leaves no credit behind.
func (s *Server) admit(addr net.Addr) (glimit.Decision, []glimit.Key) {
	if s.cfg.Limiter == nil {
		return glimit.Decision{Allow: true}, nil
	}
	var charged []glimit.Key
	for _, key := range s.connectionKeys(addr) {
		d := s.cfg.Limiter.Allow(context.Background(), key, 1)
		if !d.Allow {
			s.release(charged)
			return d, nil
		}
		charged = append(charged, key)
	}
	return glimit.Decision{Allow: true}, charged
}

// release returns the credit taken by admit. Releasing a rate key is a no-op in
// glimit, so the same list serves both shapes.
func (s *Server) release(keys []glimit.Key) {
	if s.cfg.Limiter == nil || len(keys) == 0 {
		return
	}
	for _, key := range keys {
		s.cfg.Limiter.Release(context.Background(), key, 1)
	}
}

// admitBytes charges an inbound or outbound byte rate for a session.
func (s *Server) admitBytes(sess *Session, dir glimit.Direction, n int) glimit.Decision {
	if s.cfg.Limiter == nil || n <= 0 {
		return glimit.Decision{Allow: true}
	}
	global := glimit.Key{Kind: glimit.KindBytes, Scope: glimit.ScopeGlobal, Direction: dir}
	if d := s.cfg.Limiter.Allow(context.Background(), global, n); !d.Allow {
		return d
	}
	session := glimit.Key{Kind: glimit.KindBytes, Scope: glimit.ScopeSession, ID: sess.ID, Direction: dir}
	return s.cfg.Limiter.Allow(context.Background(), session, n)
}

// admitFrame charges one inbound frame for a session.
func (s *Server) admitFrame(sess *Session) glimit.Decision {
	if s.cfg.Limiter == nil {
		return glimit.Decision{Allow: true}
	}
	global := glimit.Key{Kind: glimit.KindRequests, Scope: glimit.ScopeGlobal, Direction: glimit.DirIn}
	if d := s.cfg.Limiter.Allow(context.Background(), global, 1); !d.Allow {
		return d
	}
	session := glimit.Key{Kind: glimit.KindRequests, Scope: glimit.ScopeSession, ID: sess.ID, Direction: glimit.DirIn}
	return s.cfg.Limiter.Allow(context.Background(), session, 1)
}

// deny closes a session a limit refused, after offering the protocol a chance
// to render a refusal frame.
func (s *Server) deny(sess *Session, d glimit.Decision) {
	if s.cfg.Rejecter != nil {
		s.cfg.Rejecter.Reject(context.Background(), sess, d)
	}
	sess.closeWith(limitReason(d.Reason))
}

// reject refuses a connection before it becomes a session: it counts the
// rejection, offers the protocol a refusal frame and closes the connection.
func (s *Server) reject(conn net.Conn, d glimit.Decision) {
	s.metric().SessionRejected(s.transport(), limitReason(d.Reason))
	if s.cfg.Rejecter != nil {
		sess := s.newSession(conn)
		s.cfg.Rejecter.Reject(context.Background(), sess, d)
		_ = sess.Close()
	}
	_ = conn.Close()
}

// forgetConn drops a raw connection and returns the credit it held.
func (s *Server) forgetConn(conn net.Conn) {
	s.mu.Lock()
	delete(s.conns, conn)
	credit := s.limits[conn]
	delete(s.limits, conn)
	s.mu.Unlock()
	s.release(credit)
}

// limitReason maps a limiter denial onto the coarse metric vocabulary. The
// built-in counter keeps its historical name, so an existing alert still fires.
func limitReason(r glimit.Reason) string {
	switch r {
	case glimit.ReasonRate:
		return ReasonRateLimited
	case glimit.ReasonBurst:
		return ReasonBurstLimited
	case glimit.ReasonBytes:
		return ReasonByteLimited
	case glimit.ReasonConcurrency:
		return ReasonMaxSessions
	case glimit.ReasonQuota:
		return ReasonQuotaExceeded
	case glimit.ReasonOverload:
		return ReasonOverloaded
	case glimit.ReasonUnavailable:
		return ReasonUnavailable
	default:
		return ReasonRateLimited
	}
}
