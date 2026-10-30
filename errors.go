package gnety

import "errors"

var (
	// ErrTooManySessions is returned when MaxSessions would be exceeded.
	ErrTooManySessions = errors.New("gnety: too many sessions")
	// ErrTooManyPending is returned when a session exceeds MaxPendingRequests.
	ErrTooManyPending = errors.New("gnety: too many pending requests")
	// ErrWriteTimeout is returned when a write exceeds WriteTimeout.
	ErrWriteTimeout = errors.New("gnety: write timeout")
	// ErrShutdownTimeout is returned when Stop exceeds ShutdownTimeout.
	ErrShutdownTimeout = errors.New("gnety: shutdown timeout")
)
