package gnety

import "github.com/maxid/gnety/hook"

// The observability contract lives in ./hook: Metrics, NoopMetrics and the coarse session
// labels it reports. Every name below is an alias, so a consumer keeps importing this
// package and still gets the hook type — one definition, two import paths. New code may
// import github.com/maxid/gnety/hook directly; both spellings are the same type.
type (
	Metrics      = hook.Metrics
	MetricsFuncs = hook.MetricsFuncs
	NoopMetrics  = hook.NoopMetrics
)

const (
	ReasonClosed           = hook.ReasonClosed
	ReasonReadError        = hook.ReasonReadError
	ReasonDecoderError     = hook.ReasonDecoderError
	ReasonWriteError       = hook.ReasonWriteError
	ReasonWriteTimeout     = hook.ReasonWriteTimeout
	ReasonBackpressure     = hook.ReasonBackpressure
	ReasonIdleTimeout      = hook.ReasonIdleTimeout
	ReasonHeartbeatTimeout = hook.ReasonHeartbeatTimeout
	ReasonShutdown         = hook.ReasonShutdown
	ReasonMaxSessions      = hook.ReasonMaxSessions
)
