package gnety

import "errors"

var (
	// ErrTooManySessions is returned when MaxSessions would be exceeded.
	ErrTooManySessions = errors.New("gnety: too many sessions")
	// ErrTooManyPending is returned when a session exceeds MaxPendingRequests.
	ErrTooManyPending = errors.New("gnety: too many pending requests")
	// ErrWriteTimeout is returned when a write exceeds WriteTimeout.
	ErrWriteTimeout = errors.New("gnety: write timeout")
	// ErrWriteBusy is returned when a session already has MaxQueuedWrites
	// goroutines waiting to write. The caller decides whether to drop the
	// message or retry; the server never queues without bound.
	ErrWriteBusy = errors.New("gnety: write queue full")
	// ErrShutdownTimeout is returned when Stop exceeds ShutdownTimeout.
	ErrShutdownTimeout = errors.New("gnety: shutdown timeout")
)
