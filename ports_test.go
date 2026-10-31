package gnety

import (
	"errors"
	"net"
	"testing"
)

func tcpPort(t *testing.T) (int, net.Listener) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln.Addr().(*net.TCPAddr).Port, ln
}

func udpPort(t *testing.T) (int, net.PacketConn) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return pc.LocalAddr().(*net.UDPAddr).Port, pc
}

func TestTCPPortInUse(t *testing.T) {
	port, ln := tcpPort(t)
	if !TCPPortInUseOn("127.0.0.1", port) {
		t.Fatal("listening TCP port reported free")
	}
	_ = ln.Close()
	if TCPPortInUseOn("127.0.0.1", port) {
		t.Fatal("closed TCP port reported in use")
	}

	wildcard, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer wildcard.Close()
	if !TCPPortInUse(wildcard.Addr().(*net.TCPAddr).Port) {
		t.Fatal("wildcard probe missed a wildcard listener")
	}
}

func TestUDPPortInUse(t *testing.T) {
	port, pc := udpPort(t)
	if !UDPPortInUseOn("127.0.0.1", port) {
		t.Fatal("bound UDP port reported free")
	}
	_ = pc.Close()
	if UDPPortInUseOn("127.0.0.1", port) {
		t.Fatal("closed UDP port reported in use")
	}
}

func TestAvailablePortSkipsOccupiedAndAllocated(t *testing.T) {
	port, ln := tcpPort(t)
	defer ln.Close()
	allocated := map[int]struct{}{}
	if port < maxPort {
		allocated[port+1] = struct{}{}
	}
	got, err := AvailablePortOn("127.0.0.1", port, allocated)
	if err != nil {
		t.Fatal(err)
	}
	if got == port || got <= port {
		t.Fatalf("available port = %d, occupied = %d", got, port)
	}
	if _, ok := allocated[got]; !ok {
		t.Fatalf("chosen port %d was not registered", got)
	}
	if _, skipped := allocated[port+1]; skipped && got == port+1 && port < maxPort {
		t.Fatalf("chosen allocated port %d", got)
	}
}

func TestAvailableUDPPortSkipsOccupied(t *testing.T) {
	port, pc := udpPort(t)
	defer pc.Close()
	got, err := AvailableUDPPortOn("127.0.0.1", port, map[int]struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if got == port {
		t.Fatalf("available UDP port = occupied %d", port)
	}
}

func TestAvailablePortEphemeralAndErrors(t *testing.T) {
	if got, err := AvailablePort(0); err != nil || got != 0 {
		t.Fatalf("AvailablePort(0) = %d, %v", got, err)
	}
	for _, port := range []int{-1, 65536} {
		if _, err := AvailablePort(port); !errors.Is(err, ErrInvalidPort) {
			t.Fatalf("AvailablePort(%d) = %v", port, err)
		}
	}
	allocated := map[int]struct{}{maxPort: {}}
	if _, err := AvailablePortOn("127.0.0.1", maxPort, allocated); !errors.Is(err, ErrNoAvailablePort) {
		t.Fatalf("full range = %v", err)
	}
}
