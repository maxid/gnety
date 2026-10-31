//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package gnety

import "syscall"

func platformControl(_ syscall.RawConn, reuseAddr, reusePort bool) error {
	if reuseAddr || reusePort {
		return ErrSocketOptionsUnsupported
	}
	return nil
}
