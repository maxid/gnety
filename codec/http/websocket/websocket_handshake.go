package websocket

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
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

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func Accept(w http.ResponseWriter, r *http.Request, opts Options) (*Conn, error) {
	if r.Method != http.MethodGet {
		http.Error(w, "WebSocket requires GET", http.StatusMethodNotAllowed)
		return nil, errors.New("gnety: websocket handshake requires GET")
	}
	if !hasHeaderToken(r.Header, "Connection", "upgrade") ||
		!hasHeaderToken(r.Header, "Upgrade", "websocket") {
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
	if opts.Subprotocol != "" && !validToken(opts.Subprotocol) {
		http.Error(w, "invalid WebSocket subprotocol", http.StatusInternalServerError)
		return nil, errors.New("gnety: invalid configured WebSocket subprotocol")
	}

	if err := validOrigin(r, opts.OriginPatterns, opts.AllowAnyOrigin); err != nil {
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
			"Sec-WebSocket-Accept: %s\r\n", acceptKey(key)); err != nil {
		_ = raw.Close()
		return nil, err
	}
	if opts.Subprotocol != "" {
		if _, err := fmt.Fprintf(rw.Writer, "Sec-WebSocket-Protocol: %s\r\n", opts.Subprotocol); err != nil {
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
	return newConn(raw, rw, false, opts.MaxMessageSize, opts.Subprotocol), nil
}

func ClientHandshake(raw net.Conn, location *url.URL, origin string, opts Options) (*Conn, error) {
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
	if opts.Subprotocol != "" {
		if _, err := fmt.Fprintf(rw.Writer, "Sec-WebSocket-Protocol: %s\r\n", opts.Subprotocol); err != nil {
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
		!hasHeaderToken(resp.Header, "Connection", "upgrade") ||
		!hasHeaderToken(resp.Header, "Upgrade", "websocket") {
		return nil, errors.New("gnety: invalid WebSocket upgrade response")
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != acceptKey(key) {
		return nil, errors.New("gnety: invalid WebSocket accept key")
	}
	if resp.Header.Get("Sec-WebSocket-Extensions") != "" {
		return nil, errors.New("gnety: server selected an unsupported WebSocket extension")
	}
	selected := resp.Header.Get("Sec-WebSocket-Protocol")
	if selected != opts.Subprotocol {
		return nil, fmt.Errorf("gnety: WebSocket subprotocol %q does not match requested %q", selected, opts.Subprotocol)
	}
	return newConn(raw, rw, true, opts.MaxMessageSize, opts.Subprotocol), nil
}

func validOrigin(r *http.Request, patterns []string, allowAny bool) error {
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

func acceptKey(key string) string {
	sum := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func hasHeaderToken(header http.Header, name, want string) bool {
	for _, value := range header.Values(name) {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), want) {
				return true
			}
		}
	}
	return false
}

func hasSubprotocol(header http.Header, want string) bool {
	for _, value := range header.Values("Sec-WebSocket-Protocol") {
		for _, offered := range strings.Split(value, ",") {
			if strings.TrimSpace(offered) == want {
				return true
			}
		}
	}
	return false
}

func validToken(value string) bool {
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
