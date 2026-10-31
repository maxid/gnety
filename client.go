package gnety

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/pion/dtls/v2"

	ws "github.com/maxid/gnety/codec/http/websocket"
)

// ClientConfig configures a client connector.
type ClientConfig struct {
	Network       string // "tcp", "tcp-tls", "udp", "dtls", "ws" or "wss"
	Addr          string
	LocalAddr     net.Addr // optional local bind address for TCP/UDP/WS
	Socket        SocketOptions
	Path          string // WebSocket path (default "/")
	Subprotocol   string // WebSocket subprotocol (optional)
	Origin        string // WebSocket Origin override (optional)
	Codec         Codec
	DatagramCodec DatagramCodec
	TLS           TLSConfig
	Timeout       time.Duration
	WriteTimeout  time.Duration
	MaxFrameSize  int // WebSocket inbound message limit (0 = 4 MiB)
}

// Client is a connected stream or datagram client.
type Client struct {
	cfg  ClientConfig
	conn net.Conn
	r    *bufio.Reader
	mu   sync.Mutex
}

// Dial connects to a server.
func Dial(ctx context.Context, cfg ClientConfig) (*Client, error) {
	if cfg.Addr == "" {
		return nil, errors.New("gnety: client Addr is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if err := cfg.Socket.validate(cfg.Network); err != nil {
		return nil, err
	}
	if cfg.Network == "dtls" && cfg.LocalAddr != nil {
		return nil, fmt.Errorf("%w: DTLS local bind address is not supported", ErrSocketOptionsUnsupported)
	}
	dialer := cfg.Socket.dialer(cfg.Timeout, cfg.LocalAddr)
	var (
		conn net.Conn
		err  error
	)
	switch cfg.Network {
	case "tcp":
		if cfg.Codec == nil {
			return nil, errors.New("gnety: TCP client requires Codec")
		}
		conn, err = dialer.DialContext(ctx, "tcp", cfg.Addr)
		if err == nil {
			if applyErr := cfg.Socket.applyConn(conn); applyErr != nil {
				_ = conn.Close()
				return nil, applyErr
			}
		}
	case "tcp-tls":
		if cfg.Codec == nil {
			return nil, errors.New("gnety: TCP client requires Codec")
		}
		tlsCfg, cfgErr := clientTLSConfig(cfg.TLS, cfg.Addr)
		if cfgErr != nil {
			return nil, cfgErr
		}
		var raw net.Conn
		raw, err = dialer.DialContext(ctx, "tcp", cfg.Addr)
		if err == nil {
			if applyErr := cfg.Socket.applyConn(raw); applyErr != nil {
				_ = raw.Close()
				return nil, applyErr
			}
			tlsConn := tls.Client(raw, tlsCfg)
			if handshakeErr := tlsConn.HandshakeContext(ctx); handshakeErr != nil {
				_ = raw.Close()
				return nil, handshakeErr
			}
			conn = tlsConn
		}
	case "udp":
		if cfg.DatagramCodec == nil {
			return nil, errors.New("gnety: UDP client requires DatagramCodec")
		}
		conn, err = dialer.DialContext(ctx, "udp", cfg.Addr)
		if err == nil {
			if udp, ok := conn.(net.PacketConn); ok {
				if applyErr := cfg.Socket.applyPacketConn(udp); applyErr != nil {
					_ = conn.Close()
					return nil, applyErr
				}
			}
		}
	case "dtls":
		if cfg.DatagramCodec == nil {
			return nil, errors.New("gnety: DTLS client requires DatagramCodec")
		}
		var udpAddr *net.UDPAddr
		udpAddr, err = net.ResolveUDPAddr("udp", cfg.Addr)
		if err == nil {
			var dtlsCfg *dtls.Config
			dtlsCfg, err = loadDTLSConfig(cfg.TLS, true, cfg.Addr)
			if err == nil {
				conn, err = dtls.Dial("udp", udpAddr, dtlsCfg)
			}
		}
	case "ws", "wss":
		if cfg.Codec == nil {
			return nil, errors.New("gnety: WebSocket client requires Codec")
		}
		conn, err = dialWebSocket(ctx, dialer, cfg)
	default:
		return nil, fmt.Errorf("gnety: unsupported client network %q", cfg.Network)
	}
	if err != nil {
		return nil, err
	}
	c := &Client{cfg: cfg, conn: conn}
	if cfg.Codec != nil {
		c.r = bufio.NewReaderSize(conn, 4096)
	}
	return c, nil
}

// NewClient wraps an existing connection instead of dialing. It is the
// custom-socket entry point: pass a net.Pipe, Unix socket, an already
// handshaken TLS conn, or any other net.Conn. The caller keeps ownership until
// Client.Close.
//
// Network selects the codec path: tcp/tcp-tls/ws/wss require Codec, udp/dtls
// require DatagramCodec. Socket options that only make sense while creating a
// socket (ReuseAddr/ReusePort/Control) are not applied; accepted-connection
// options (keepalive/nodelay/linger/buffers) are applied when the Conn is a
// *net.TCPConn.
func NewClient(conn net.Conn, cfg ClientConfig) (*Client, error) {
	if conn == nil {
		return nil, errors.New("gnety: client connection is required")
	}
	if cfg.Network == "" {
		cfg.Network = "tcp"
	}
	if err := cfg.Socket.validate(cfg.Network); err != nil {
		return nil, err
	}
	switch cfg.Network {
	case "tcp", "tcp-tls", "ws", "wss":
		if cfg.Codec == nil {
			return nil, errors.New("gnety: stream client requires Codec")
		}
	case "udp", "dtls":
		if cfg.DatagramCodec == nil {
			return nil, errors.New("gnety: datagram client requires DatagramCodec")
		}
	default:
		return nil, fmt.Errorf("gnety: unsupported client network %q", cfg.Network)
	}
	if err := cfg.Socket.applyConn(conn); err != nil {
		return nil, err
	}
	if pc, ok := conn.(net.PacketConn); ok {
		if err := cfg.Socket.applyPacketConn(pc); err != nil {
			return nil, err
		}
	}
	c := &Client{cfg: cfg, conn: conn}
	if cfg.Codec != nil {
		c.r = bufio.NewReaderSize(conn, 4096)
	}
	return c, nil
}

func clientTLSConfig(cfg TLSConfig, addr string) (*tls.Config, error) {
	out := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("gnety: read TLS CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("gnety: invalid TLS CA PEM")
		}
		out.RootCAs = pool
	}
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("gnety: load client key pair: %w", err)
		}
		out.Certificates = []tls.Certificate{cert}
	}
	if host, _, err := net.SplitHostPort(addr); err == nil && net.ParseIP(host) == nil {
		out.ServerName = host
	}
	return out, nil
}

// Send encodes and writes a message.
func (c *Client) Send(msg any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return ErrSessionClosed
	}
	if timeout := c.cfg.WriteTimeout; timeout > 0 {
		_ = c.conn.SetWriteDeadline(time.Now().Add(timeout))
	}
	defer func() { _ = c.conn.SetWriteDeadline(time.Time{}) }()
	if c.cfg.Network == "udp" || c.cfg.Network == "dtls" {
		raw, err := c.cfg.DatagramCodec.Encode(msg)
		if err != nil {
			return err
		}
		_, err = c.conn.Write(raw)
		return wrapWriteError(err)
	}
	var buf bytes.Buffer
	if err := c.cfg.Codec.Encode(&buf, msg); err != nil {
		return err
	}
	_, err := c.conn.Write(buf.Bytes())
	return wrapWriteError(err)
}

// Receive reads and decodes one message.
func (c *Client) Receive(ctx context.Context) (any, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetReadDeadline(deadline)
	}
	if c.cfg.Network == "udp" || c.cfg.Network == "dtls" {
		buf := make([]byte, 64*1024)
		n, err := c.conn.Read(buf)
		if err != nil {
			return nil, err
		}
		return c.cfg.DatagramCodec.Decode(buf[:n])
	}
	return c.cfg.Codec.Decode(c.r)
}

// Close closes the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// WebSocket returns the underlying WebSocket connection when Network is ws/wss.
func (c *Client) WebSocket() (*ws.Conn, bool) {
	w, ok := c.conn.(*ws.Conn)
	return w, ok
}

// LocalAddr returns the local address.
func (c *Client) LocalAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}
