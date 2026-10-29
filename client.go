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
)

// ClientConfig configures a client connector.
type ClientConfig struct {
	Network       string // "tcp", "tcp-tls" or "udp"
	Addr          string
	Codec         Codec
	DatagramCodec DatagramCodec
	TLS           TLSConfig
	Timeout       time.Duration
}

// Client is a connected TCP/UDP client.
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
	dialer := &net.Dialer{Timeout: cfg.Timeout}
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
	case "tcp-tls":
		if cfg.Codec == nil {
			return nil, errors.New("gnety: TCP client requires Codec")
		}
		tlsCfg, cfgErr := clientTLSConfig(cfg.TLS, cfg.Addr)
		if cfgErr != nil {
			return nil, cfgErr
		}
		conn, err = tls.DialWithDialer(dialer, "tcp", cfg.Addr, tlsCfg)
	case "udp":
		if cfg.DatagramCodec == nil {
			return nil, errors.New("gnety: UDP client requires DatagramCodec")
		}
		conn, err = dialer.DialContext(ctx, "udp", cfg.Addr)
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
	if c.cfg.Network == "udp" {
		raw, err := c.cfg.DatagramCodec.Encode(msg)
		if err != nil {
			return err
		}
		_, err = c.conn.Write(raw)
		return err
	}
	var buf bytes.Buffer
	if err := c.cfg.Codec.Encode(&buf, msg); err != nil {
		return err
	}
	_, err := c.conn.Write(buf.Bytes())
	return err
}

// Receive reads and decodes one message.
func (c *Client) Receive(ctx context.Context) (any, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetReadDeadline(deadline)
	}
	if c.cfg.Network == "udp" {
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

// LocalAddr returns the local address.
func (c *Client) LocalAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}
