//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package gnety

import (
	"golang.org/x/sys/unix"

	"syscall"
)

func platformControl(c syscall.RawConn, reuseAddr, reusePort bool) error {
	var optionErr error
	if err := c.Control(func(fd uintptr) {
		if reuseAddr {
			if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
				optionErr = err
				return
			}
		}
		if reusePort {
			optionErr = setReusePort(int(fd))
		}
	}); err != nil {
		return err
	}
	return optionErr
}
