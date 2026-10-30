package gnety

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const webSocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func acceptWebSocket(w http.ResponseWriter, r *http.Request, subprotocol string, maxFrameSize int, originPatterns []string, allowAnyOrigin bool) (*webSocketConn, error) {
	if r.Method != http.MethodGet {
		http.Error(w, "WebSocket requires GET", http.StatusMethodNotAllowed)
		return nil, errors.New("gnety: websocket handshake requires GET")
	}
	if !hasWebSocketHeaderToken(r.Header, "Connection", "upgrade") ||
		!hasWebSocketHeaderToken(r.Header, "Upgrade", "websocket") {
		http.Error(w, "invalid WebSocket upgrade", http.StatusBadRequest)
		return nil, errors.New("gnety: invalid WebSocket upgrade")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		http.Error(w, "unsupported WebSocket version", http.StatusUpgradeRequired)
		return nil, errors.New("gnety: unsupported WebSocket version")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	decodedKey, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decodedKey) != 16 {
		http.Error(w, "invalid WebSocket key", http.StatusBadRequest)
		return nil, errors.New("gnety: invalid WebSocket key")
	}
	if subprotocol != "" && !validWebSocketToken(subprotocol) {
		http.Error(w, "invalid WebSocket subprotocol", http.StatusInternalServerError)
		return nil, errors.New("gnety: invalid configured WebSocket subprotocol")
	}

	if err := validWebSocketOrigin(r, originPatterns, allowAnyOrigin); err != nil {
		http.Error(w, "WebSocket origin is not allowed", http.StatusForbidden)
		return nil, err
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "WebSocket hijacking unsupported", http.StatusInternalServerError)
		return nil, errors.New("gnety: WebSocket hijacking unsupported")
	}
	raw, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(rw.Writer,
		"HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: %s\r\n", webSocketAccept(key)); err != nil {
		_ = raw.Close()
		return nil, err
	}
	if subprotocol != "" {
		if _, err := fmt.Fprintf(rw.Writer, "Sec-WebSocket-Protocol: %s\r\n", subprotocol); err != nil {
			_ = raw.Close()
			return nil, err
		}
	}
	if _, err := rw.Writer.WriteString("\r\n"); err != nil {
		_ = raw.Close()
		return nil, err
	}
	if err := rw.Writer.Flush(); err != nil {
		_ = raw.Close()
		return nil, err
	}
	_ = raw.SetDeadline(time.Time{})
	return newWebSocketConn(raw, rw, false, maxFrameSize, subprotocol), nil
}

func dialWebSocket(ctx context.Context, dialer *net.Dialer, cfg ClientConfig) (net.Conn, error) {
	path := cfg.Path
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		return nil, errors.New("gnety: WebSocket Path must start with /")
	}
	if cfg.Subprotocol != "" && !validWebSocketToken(cfg.Subprotocol) {
		return nil, errors.New("gnety: invalid WebSocket subprotocol")
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
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = raw.SetDeadline(deadline)
	stopCancel := context.AfterFunc(ctx, func() { _ = raw.SetDeadline(time.Now()) })
	conn, err := clientWebSocketHandshake(raw, location, origin, cfg.Subprotocol, cfg.MaxFrameSize)
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

func clientWebSocketHandshake(raw net.Conn, location *url.URL, origin, subprotocol string, maxFrameSize int) (*webSocketConn, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(nonce[:])
	rw := bufio.NewReadWriter(bufio.NewReader(raw), bufio.NewWriter(raw))
	if _, err := fmt.Fprintf(rw.Writer,
		"GET %s HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: 13\r\n"+
			"Origin: %s\r\n", location.RequestURI(), location.Host, key, origin); err != nil {
		return nil, err
	}
	if subprotocol != "" {
		if _, err := fmt.Fprintf(rw.Writer, "Sec-WebSocket-Protocol: %s\r\n", subprotocol); err != nil {
			return nil, err
		}
	}
	if _, err := rw.Writer.WriteString("\r\n"); err != nil {
		return nil, err
	}
	if err := rw.Writer.Flush(); err != nil {
		return nil, err
	}
	resp, err := http.ReadResponse(rw.Reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols ||
		!hasWebSocketHeaderToken(resp.Header, "Connection", "upgrade") ||
		!hasWebSocketHeaderToken(resp.Header, "Upgrade", "websocket") {
		return nil, errors.New("gnety: invalid WebSocket upgrade response")
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != webSocketAccept(key) {
		return nil, errors.New("gnety: invalid WebSocket accept key")
	}
	if resp.Header.Get("Sec-WebSocket-Extensions") != "" {
		return nil, errors.New("gnety: server selected an unsupported WebSocket extension")
	}
	selected := resp.Header.Get("Sec-WebSocket-Protocol")
	if selected != subprotocol {
		return nil, fmt.Errorf("gnety: WebSocket subprotocol %q does not match requested %q", selected, subprotocol)
	}
	return newWebSocketConn(raw, rw, true, maxFrameSize, subprotocol), nil
}

func validWebSocketOrigin(r *http.Request, patterns []string, allowAny bool) error {
	origin := r.Header.Get("Origin")
	if origin == "" || allowAny {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil {
		return errors.New("gnety: invalid WebSocket Origin")
	}
	if strings.EqualFold(u.Host, r.Host) {
		return nil
	}
	for _, pattern := range patterns {
		target := u.Host
		if strings.Contains(pattern, "://") {
			target = u.Scheme + "://" + u.Host
		}
		if ok, _ := path.Match(strings.ToLower(pattern), strings.ToLower(target)); ok {
			return nil
		}
	}
	return fmt.Errorf("gnety: WebSocket Origin %q is not allowed", origin)
}

func webSocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + webSocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func hasWebSocketHeaderToken(header http.Header, name, want string) bool {
	for _, value := range header.Values(name) {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), want) {
				return true
			}
		}
	}
	return false
}

func hasWebSocketSubprotocol(header http.Header, want string) bool {
	for _, value := range header.Values("Sec-WebSocket-Protocol") {
		for _, offered := range strings.Split(value, ",") {
			if strings.TrimSpace(offered) == want {
				return true
			}
		}
	}
	return false
}

func validWebSocketToken(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		switch c {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		}
		return false
	}
	return true
}
