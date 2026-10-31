//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package gnety

import "golang.org/x/sys/unix"

func setReusePort(fd int) error {
	return unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
}
