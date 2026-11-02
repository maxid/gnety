// Package hook is the extension contract of this package: the interfaces a consumer
// implements to observe the transport.
//
// It exists as its own package so the extension surface is one visible directory instead of
// a section of an implementation file. Nothing here imports the root package, so an
// implementation cannot reach internals it is not supposed to see, and the root package
// re-exports every name as an alias — existing code keeps using gnety.Metrics.
package hook

// Reasons are the low-cardinality labels attached to session events. They are
// deliberately coarse: never encode a client ID, peer address, topic or payload
// into a metric label.
const (
	ReasonClosed           = "closed"
	ReasonReadError        = "read_error"
	ReasonDecoderError     = "decoder_error"
	ReasonWriteError       = "write_error"
	ReasonWriteTimeout     = "write_timeout"
	ReasonBackpressure     = "backpressure"
	ReasonIdleTimeout      = "idle_timeout"
	ReasonHeartbeatTimeout = "heartbeat_timeout"
	ReasonShutdown         = "shutdown"
	ReasonMaxSessions      = "max_sessions"
)

// Metrics receives transport-level events. Implementations must be safe for
// concurrent use and must not block: the hooks run on session goroutines.
//
// The interface is counters only. Byte and latency accounting is left to the
// protocol libraries, which know what a meaningful label is for their own
// exchanges.
type Metrics interface {
	SessionOpen(transport string)
	SessionClose(transport, reason string)
	SessionRejected(transport, reason string)
	MessageIn(transport string)
	MessageOut(transport string)
	WriteFailed(transport, reason string)
}

// NoopMetrics drops every event. It is the default.
type NoopMetrics struct{}

// SessionOpen implements Metrics.
func (NoopMetrics) SessionOpen(string) {}

// SessionClose implements Metrics.
func (NoopMetrics) SessionClose(string, string) {}

// SessionRejected implements Metrics.
func (NoopMetrics) SessionRejected(string, string) {}

// MessageIn implements Metrics.
func (NoopMetrics) MessageIn(string) {}

// MessageOut implements Metrics.
func (NoopMetrics) MessageOut(string) {}

// WriteFailed implements Metrics.
func (NoopMetrics) WriteFailed(string, string) {}

// MetricsFuncs implements Metrics with optional function fields, so a consumer implements
// only the events it cares about instead of six methods. A nil field is skipped.
//
// The field names carry a Func suffix because a field and a method cannot share a name on
// one type.
type MetricsFuncs struct {
	SessionOpenFunc     func(transport string)
	SessionCloseFunc    func(transport, reason string)
	SessionRejectedFunc func(transport, reason string)
	MessageInFunc       func(transport string)
	MessageOutFunc      func(transport string)
	WriteFailedFunc     func(transport, reason string)
}

// SessionOpen calls SessionOpenFunc when it is set.
func (m MetricsFuncs) SessionOpen(transport string) {
	if m.SessionOpenFunc != nil {
		m.SessionOpenFunc(transport)
	}
}

// SessionClose calls SessionCloseFunc when it is set.
func (m MetricsFuncs) SessionClose(transport, reason string) {
	if m.SessionCloseFunc != nil {
		m.SessionCloseFunc(transport, reason)
	}
}

// SessionRejected calls SessionRejectedFunc when it is set.
func (m MetricsFuncs) SessionRejected(transport, reason string) {
	if m.SessionRejectedFunc != nil {
		m.SessionRejectedFunc(transport, reason)
	}
}

// MessageIn calls MessageInFunc when it is set.
func (m MetricsFuncs) MessageIn(transport string) {
	if m.MessageInFunc != nil {
		m.MessageInFunc(transport)
	}
}

// MessageOut calls MessageOutFunc when it is set.
func (m MetricsFuncs) MessageOut(transport string) {
	if m.MessageOutFunc != nil {
		m.MessageOutFunc(transport)
	}
}

// WriteFailed calls WriteFailedFunc when it is set.
func (m MetricsFuncs) WriteFailed(transport, reason string) {
	if m.WriteFailedFunc != nil {
		m.WriteFailedFunc(transport, reason)
	}
}
