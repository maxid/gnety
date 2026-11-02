package gnety

import (
	"testing"

	"github.com/maxid/gnety/hook"
)

// The observability contract is defined in gnety/hook and re-exported here as aliases.
// These assertions fail to compile the moment one of them becomes a distinct named type,
// which is the compatibility promise this package makes.
var (
	_ hook.Metrics      = (Metrics)(nil)
	_ hook.MetricsFuncs = MetricsFuncs{}
	_ hook.NoopMetrics  = NoopMetrics{}

	_ Metrics      = hook.NoopMetrics{}
	_ Metrics      = hook.MetricsFuncs{}
	_ MetricsFuncs = hook.MetricsFuncs{}
)

func TestReasonLabelsAreAliased(t *testing.T) {
	for _, pair := range [][2]string{
		{ReasonClosed, hook.ReasonClosed},
		{ReasonReadError, hook.ReasonReadError},
		{ReasonDecoderError, hook.ReasonDecoderError},
		{ReasonWriteError, hook.ReasonWriteError},
		{ReasonWriteTimeout, hook.ReasonWriteTimeout},
		{ReasonBackpressure, hook.ReasonBackpressure},
		{ReasonIdleTimeout, hook.ReasonIdleTimeout},
		{ReasonHeartbeatTimeout, hook.ReasonHeartbeatTimeout},
		{ReasonShutdown, hook.ReasonShutdown},
		{ReasonMaxSessions, hook.ReasonMaxSessions},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("label mismatch: %q != %q", pair[0], pair[1])
		}
	}
}

func TestMetricsFuncsSkipNilFields(t *testing.T) {
	var metrics Metrics = hook.MetricsFuncs{}
	// Every call with a nil field must be a no-op, not a panic.
	metrics.SessionOpen("tcp")
	metrics.SessionClose("tcp", ReasonClosed)
	metrics.SessionRejected("tcp", ReasonMaxSessions)
	metrics.MessageIn("tcp")
	metrics.MessageOut("tcp")
	metrics.WriteFailed("tcp", ReasonWriteError)
}
