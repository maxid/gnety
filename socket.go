package gnety

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

// ErrSocketOptionsUnsupported is returned when the platform cannot apply a
// requested socket option (for example SO_REUSEPORT on Windows).
var ErrSocketOptionsUnsupported = errors.New("gnety: socket option is not supported on this platform")

// SocketOptions configures the kernel socket behind a listener, accepted
// connection or client dial. Zero values keep the Go/runtime default.
//
// The Control hook is the escape hatch for platform-specific options that are
// not modeled here (TCP_FASTOPEN, IP_TRANSPARENT, SO_BINDTODEVICE, ...).
type SocketOptions struct {
	// ReuseAddr sets SO_REUSEADDR before bind.
	ReuseAddr bool
	// ReusePort sets SO_REUSEPORT before bind. Linux, macOS and the BSDs
	// support it; Windows rejects it. Multiple listeners sharing a port must
	// all enable it.
	ReusePort bool

	// KeepAlive: >0 enables TCP keepalive with this period, <0 disables it,
	// 0 keeps the Go default. It is valid for TCP/TLS/WS only.
	KeepAlive time.Duration
	// NoDelay sets TCP_NODELAY after accept/dial. nil keeps the Go default
	// (enabled).
	NoDelay *bool
	// Linger maps to SO_LINGER: <0 disables lingering, >0 is the timeout in
	// seconds, 0 keeps the default.
	Linger int

	// ReadBuffer/WriteBuffer set SO_RCVBUF/SO_SNDBUF when >0.
	ReadBuffer  int
	WriteBuffer int

	// Control runs after gnety applies ReuseAddr/ReusePort. Returning an error
	// aborts the listen/dial.
	Control func(network, address string, c syscall.RawConn) error
}

func (o SocketOptions) isZero() bool {
	return !o.ReuseAddr && !o.ReusePort &&
		o.KeepAlive == 0 && o.NoDelay == nil && o.Linger == 0 &&
		o.ReadBuffer == 0 && o.WriteBuffer == 0 && o.Control == nil
}

func (o SocketOptions) validate(network string) error {
	if o.ReadBuffer < 0 || o.WriteBuffer < 0 {
		return fmt.Errorf("gnety: socket buffer sizes must not be negative")
	}
	switch network {
	case "udp":
		if o.KeepAlive != 0 || o.NoDelay != nil || o.Linger != 0 {
			return fmt.Errorf("gnety: TCP socket options are not valid for udp")
		}
	case "dtls":
		if !o.isZero() {
			return fmt.Errorf("%w: DTLS does not expose socket options", ErrSocketOptionsUnsupported)
		}
	}
	return nil
}

// control applies gnety's built-in options, then the caller's Control hook.
func (o SocketOptions) control(network, address string, c syscall.RawConn) error {
	if o.ReuseAddr || o.ReusePort {
		if err := platformControl(c, o.ReuseAddr, o.ReusePort); err != nil {
			return err
		}
	}
	if o.Control != nil {
		return o.Control(network, address, c)
	}
	return nil
}

func (o SocketOptions) listenConfig() *net.ListenConfig {
	lc := &net.ListenConfig{}
	if !o.isZero() {
		lc.Control = o.control
	}
	return lc
}

func (o SocketOptions) dialer(timeout time.Duration, local net.Addr) *net.Dialer {
	d := &net.Dialer{Timeout: timeout, LocalAddr: local}
	if !o.isZero() {
		d.Control = o.control
	}
	if o.KeepAlive != 0 {
		d.KeepAlive = o.KeepAlive
	}
	return d
}

// wrapListener applies accepted-connection options (keepalive, nodelay, ...)
// to every TCP connection. Listener-level options (reuse) were already applied
// by ListenConfig.Control.
func (o SocketOptions) wrapListener(ln net.Listener) net.Listener {
	if o.KeepAlive == 0 && o.NoDelay == nil && o.Linger == 0 && o.ReadBuffer == 0 && o.WriteBuffer == 0 {
		return ln
	}
	return &socketListener{Listener: ln, opts: o}
}

type socketListener struct {
	net.Listener
	opts SocketOptions
}

func (l *socketListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if err := l.opts.applyConn(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (o SocketOptions) applyConn(conn net.Conn) error {
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return nil
	}
	if o.KeepAlive != 0 {
		if err := tcp.SetKeepAlive(o.KeepAlive > 0); err != nil {
			return err
		}
		if o.KeepAlive > 0 {
			if err := tcp.SetKeepAlivePeriod(o.KeepAlive); err != nil {
				return err
			}
		}
	}
	if o.NoDelay != nil {
		if err := tcp.SetNoDelay(*o.NoDelay); err != nil {
			return err
		}
	}
	if o.Linger != 0 {
		if err := tcp.SetLinger(o.Linger); err != nil {
			return err
		}
	}
	if o.ReadBuffer > 0 {
		if err := tcp.SetReadBuffer(o.ReadBuffer); err != nil {
			return err
		}
	}
	if o.WriteBuffer > 0 {
		if err := tcp.SetWriteBuffer(o.WriteBuffer); err != nil {
			return err
		}
	}
	return nil
}

func (o SocketOptions) applyPacketConn(conn net.PacketConn) error {
	udp, ok := conn.(*net.UDPConn)
	if !ok {
		return nil
	}
	if o.ReadBuffer > 0 {
		if err := udp.SetReadBuffer(o.ReadBuffer); err != nil {
			return err
		}
	}
	if o.WriteBuffer > 0 {
		if err := udp.SetWriteBuffer(o.WriteBuffer); err != nil {
			return err
		}
	}
	return nil
}
