//go:build windows

package gnety

import (
	"syscall"

	"golang.org/x/sys/windows"
)

func platformControl(c syscall.RawConn, reuseAddr, reusePort bool) error {
	if reusePort {
		return ErrSocketOptionsUnsupported
	}
	if !reuseAddr {
		return nil
	}
	var optionErr error
	if err := c.Control(func(fd uintptr) {
		optionErr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
	}); err != nil {
		return err
	}
	return optionErr
}
