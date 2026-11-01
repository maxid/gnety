package gnety

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// AnyAddress is the wildcard bind address used by the port helpers.
const AnyAddress = "0.0.0.0"

// PortProtocol selects which transport a port must be free for.
type PortProtocol string

const (
	// PortTCP requires the TCP port to be free.
	PortTCP PortProtocol = "tcp"
	// PortUDP requires the UDP port to be free.
	PortUDP PortProtocol = "udp"
	// PortTCPAndUDP requires the same numeric port to be free for both.
	PortTCPAndUDP PortProtocol = "tcp+udp"
)

// Port helper errors.
var (
	ErrInvalidPort     = errors.New("gnety: invalid port")
	ErrInvalidProtocol = errors.New("gnety: invalid port protocol")
	ErrNoAvailablePort = errors.New("gnety: no available port")
)

const maxPort = 65535

// TCPPortInUse reports whether a TCP port is already bound on AnyAddress.
// It probes by binding, not by connecting: 0.0.0.0 is not a valid connect
// target on every platform, while binding matches what a real server does.
func TCPPortInUse(port int) bool { return TCPPortInUseOn(AnyAddress, port) }

// TCPPortInUseOn reports whether a TCP port is already bound on host.
func TCPPortInUseOn(host string, port int) bool {
	return PortInUse(PortTCP, host, port)
}

// UDPPortInUse reports whether a UDP port is already bound on AnyAddress.
func UDPPortInUse(port int) bool { return UDPPortInUseOn(AnyAddress, port) }

// UDPPortInUseOn reports whether a UDP port is already bound on host.
func UDPPortInUseOn(host string, port int) bool {
	return PortInUse(PortUDP, host, port)
}

// PortInUse reports whether a port is unavailable for protocol. TCP and UDP
// are probed by binding, not by connecting.
func PortInUse(protocol PortProtocol, host string, port int) bool {
	switch protocol {
	case PortTCP:
		return probePort("tcp", host, port)
	case PortUDP:
		return probePort("udp", host, port)
	case PortTCPAndUDP:
		return probePort("tcp", host, port) || probePort("udp", host, port)
	default:
		return true
	}
}

func probePort(network, host string, port int) bool {
	if port < 0 || port > maxPort {
		return true
	}
	host = bindHost(host)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	if network == "tcp" {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return true
		}
		_ = ln.Close()
		return false
	}
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return true
	}
	_ = pc.Close()
	return false
}

// AvailablePort returns the first free TCP port at or after start on
// AnyAddress. start == 0 returns 0 (ephemeral); callers that need a concrete
// port should start at a positive value.
func AvailablePort(start int) (int, error) {
	return AvailablePortOn(AnyAddress, start, nil)
}

// AvailablePortOn returns the first free TCP port at or after start on host.
// allocated, when non-nil, is checked and updated with the chosen port so a
// batch of related ports (for example 1883/1884) does not pick the same value.
//
// The probe-to-bind window is inherently racy; callers must still handle a
// bind failure.
func AvailablePortOn(host string, start int, allocated map[int]struct{}) (int, error) {
	return AvailablePortFor(PortTCP, host, start, allocated)
}

// AvailableUDPPort returns the first free UDP port at or after start on
// AnyAddress.
func AvailableUDPPort(start int) (int, error) {
	return AvailableUDPPortOn(AnyAddress, start, nil)
}

// AvailableUDPPortOn returns the first free UDP port at or after start on host.
func AvailableUDPPortOn(host string, start int, allocated map[int]struct{}) (int, error) {
	return AvailablePortFor(PortUDP, host, start, allocated)
}

// AvailablePortBoth returns the first port free for both TCP and UDP on
// AnyAddress. Use it when one service binds the same numeric port for both.
func AvailablePortBoth(start int) (int, error) {
	return AvailablePortFor(PortTCPAndUDP, AnyAddress, start, nil)
}

// AvailablePortBothOn is AvailablePortBoth with an explicit host and
// allocation set.
func AvailablePortBothOn(host string, start int, allocated map[int]struct{}) (int, error) {
	return AvailablePortFor(PortTCPAndUDP, host, start, allocated)
}

// AvailablePortFor returns the first port free for protocol at or after start.
// An empty protocol defaults to TCP; use PortTCPAndUDP to require both. The
// allocated set is checked and updated like AvailablePortOn.
func AvailablePortFor(protocol PortProtocol, host string, start int, allocated map[int]struct{}) (int, error) {
	if protocol == "" {
		protocol = PortTCP
	}
	if protocol != PortTCP && protocol != PortUDP && protocol != PortTCPAndUDP {
		return 0, fmt.Errorf("%w: %q", ErrInvalidProtocol, protocol)
	}
	if start < 0 || start > maxPort {
		return 0, fmt.Errorf("%w: %d", ErrInvalidPort, start)
	}
	if start == 0 {
		if allocated != nil {
			allocated[0] = struct{}{}
		}
		return 0, nil
	}
	host = bindHost(host)
	for candidate := start; candidate <= maxPort; candidate++ {
		if allocated != nil {
			if _, used := allocated[candidate]; used {
				continue
			}
		}
		if !PortInUse(protocol, host, candidate) {
			if allocated != nil {
				allocated[candidate] = struct{}{}
			}
			return candidate, nil
		}
	}
	return 0, fmt.Errorf("%w: from %d on %s/%s", ErrNoAvailablePort, start, host, protocol)
}

func bindHost(host string) string {
	if host = strings.TrimSpace(host); host == "" {
		return AnyAddress
	}
	return host
}
