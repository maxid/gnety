package gnety

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

// DefaultMaxQueuedWrites is how many goroutines may wait to write on one
// session when Config.MaxQueuedWrites is zero.
const DefaultMaxQueuedWrites = 8

// metric returns the configured Metrics, or a no-op implementation.
func (s *Server) metric() Metrics {
	if s.cfg.Metrics == nil {
		return NoopMetrics{}
	}
	return s.cfg.Metrics
}

// transport is the configured network name, the only transport label emitted.
func (s *Server) transport() string { return s.cfg.Network }

// maxQueuedWrites resolves the write gate size. A negative value disables it.
func (s *Server) maxQueuedWrites() int {
	switch n := s.cfg.MaxQueuedWrites; {
	case n < 0:
		return 0
	case n == 0:
		return DefaultMaxQueuedWrites
	default:
		return n
	}
}
