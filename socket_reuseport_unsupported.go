//go:build aix || solaris

package gnety

func setReusePort(int) error { return ErrSocketOptionsUnsupported }
