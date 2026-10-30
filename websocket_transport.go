package gnety

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	ws "github.com/maxid/gnety/codec/http/websocket"
)

func dialWebSocket(ctx context.Context, dialer *net.Dialer, cfg ClientConfig) (net.Conn, error) {
	path := cfg.Path
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		return nil, errors.New("gnety: WebSocket Path must start with /")
	}
	originScheme := "http"
	if cfg.Network == "wss" {
		originScheme = "https"
	}
	origin := cfg.Origin
	if origin == "" {
		origin = originScheme + "://" + cfg.Addr
	}
	location, err := url.Parse(cfg.Network + "://" + cfg.Addr + path)
	if err != nil || location.Host == "" || location.User != nil || location.Fragment != "" {
		return nil, errors.New("gnety: invalid WebSocket location")
	}

	raw, err := dialer.DialContext(ctx, "tcp", cfg.Addr)
	if err != nil {
		return nil, err
	}
	if cfg.Network == "wss" {
		tlsCfg, cfgErr := clientTLSConfig(cfg.TLS, cfg.Addr)
		if cfgErr != nil {
			_ = raw.Close()
			return nil, cfgErr
		}
		if tlsCfg.ServerName == "" {
			if host, _, splitErr := net.SplitHostPort(cfg.Addr); splitErr == nil {
				tlsCfg.ServerName = host
			}
		}
		tlsConn := tls.Client(raw, tlsCfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return nil, err
		}
		raw = tlsConn
	}

	deadline := time.Now().Add(cfg.Timeout)
	if cfg.Timeout <= 0 {
		deadline = time.Now().Add(5 * time.Second)
	}
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = raw.SetDeadline(deadline)
	stopCancel := context.AfterFunc(ctx, func() { _ = raw.SetDeadline(time.Now()) })
	conn, err := ws.ClientHandshake(raw, location, origin, ws.Options{
		Subprotocol:    cfg.Subprotocol,
		MaxMessageSize: cfg.MaxFrameSize,
		AllowAnyOrigin: true, // client-side Origin is informational; server decides policy
	})
	stopCancel()
	if err != nil {
		_ = raw.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
				return nil, context.DeadlineExceeded
			}
		}
		return nil, err
	}
	if ctx.Err() != nil {
		_ = conn.Close()
		return nil, ctx.Err()
	}
	_ = raw.SetDeadline(time.Time{})
	return conn, nil
}
