package gnety

import (
	"bufio"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/maxid/glimit"
)

func mustLimiter(t *testing.T, policies ...glimit.Policy) glimit.Limiter {
	t.Helper()
	gate, err := glimit.Compile(glimit.Spec{Policies: policies})
	if err != nil {
		t.Fatalf("glimit.Compile: %v", err)
	}
	return gate
}

func waitSessionCount(t *testing.T, srv *Server, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.SessionCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("session count = %d, want %d", srv.SessionCount(), want)
}

func TestLimiterConnectionCap(t *testing.T) {
	rec := &recorder{}
	srv, err := New(Config{
		Network: "tcp",
		Addr:    "127.0.0.1:0",
		Codec:   LineCodec{},
		Handler: echoHandler(),
		Metrics: rec,
		Limiter: mustLimiter(t, glimit.Policy{Kind: glimit.KindConnections, Scope: glimit.ScopeGlobal, Max: 1}),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(context.Background())

	first, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer first.Close()
	waitSessionCount(t, srv, 1)

	second, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	defer second.Close()
	_ = second.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = second.Write([]byte("x\n"))
	buf := make([]byte, 8)
	if _, err := second.Read(buf); err == nil {
		t.Fatal("the limiter must refuse the second connection")
	}
	// The limiter counter maps onto the historical max_sessions reason.
	rec.waitFor(t, "reject:tcp:max_sessions")

	_ = first.Close()
	waitSessionCount(t, srv, 0)
	third, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("third dial: %v", err)
	}
	defer third.Close()
	_ = third.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := third.Write([]byte("hi\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	line, err := bufio.NewReader(third).ReadString('\n')
	if err != nil || line != "hi\n" {
		t.Fatalf("released credit must admit a new session: %q %v", line, err)
	}
}

func TestLimiterPeerConnectionCap(t *testing.T) {
	rec := &recorder{}
	srv, err := New(Config{
		Network: "tcp",
		Addr:    "127.0.0.1:0",
		Codec:   LineCodec{},
		Handler: echoHandler(),
		Metrics: rec,
		Limiter: mustLimiter(t, glimit.Policy{Kind: glimit.KindConnections, Scope: glimit.ScopePeer, Max: 1}),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(context.Background())

	first, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer first.Close()
	waitSessionCount(t, srv, 1)

	second, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	defer second.Close()
	_ = second.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := second.Read(make([]byte, 1)); err == nil {
		t.Fatal("both connections come from one peer, so the second must be refused")
	}
	rec.waitFor(t, "reject:tcp:max_sessions")
}

func TestLimiterAcceptRate(t *testing.T) {
	rec := &recorder{}
	srv, err := New(Config{
		Network: "tcp",
		Addr:    "127.0.0.1:0",
		Codec:   LineCodec{},
		Handler: echoHandler(),
		Metrics: rec,
		Limiter: mustLimiter(t, glimit.Policy{Kind: glimit.KindRequests, Scope: glimit.ScopeGlobal, Rate: 1, Burst: 1}),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(context.Background())

	first, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer first.Close()
	waitSessionCount(t, srv, 1)

	second, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	defer second.Close()
	_ = second.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := second.Read(make([]byte, 1)); err == nil {
		t.Fatal("the accept rate must refuse the second connection")
	}
	rec.waitFor(t, "reject:tcp:rate_limited")
}

func TestLimiterReadBytesClosesSession(t *testing.T) {
	rec := &recorder{}
	srv, err := New(Config{
		Network: "tcp",
		Addr:    "127.0.0.1:0",
		Codec:   LineCodec{},
		Handler: echoHandler(),
		Metrics: rec,
		Limiter: mustLimiter(t, glimit.Policy{
			Kind: glimit.KindBytes, Scope: glimit.ScopeSession, Dir: glimit.DirIn, Rate: 4, Burst: 4,
		}),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(context.Background())

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte("hello\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	rec.waitFor(t, "close:tcp:byte_limited")
}

func TestLimiterWriteBytesRefusesSend(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	srv := &Server{cfg: Config{
		Codec:   LineCodec{},
		Limiter: mustLimiter(t, glimit.Policy{Kind: glimit.KindBytes, Scope: glimit.ScopeSession, Dir: glimit.DirOut, Rate: 4, Burst: 4}),
	}}
	sess := &Session{srv: srv, conn: a}
	sess.init()
	go func() {
		buf := make([]byte, 32)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()
	if err := sess.Send([]byte("hi")); err != nil {
		t.Fatalf("three bytes must fit a burst of four: %v", err)
	}
	err := sess.Send([]byte("hello"))
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestLimiterFrameRateClosesSession(t *testing.T) {
	rec := &recorder{}
	srv, err := New(Config{
		Network: "tcp",
		Addr:    "127.0.0.1:0",
		Codec:   LineCodec{},
		Handler: HandlerFuncs{Message: func(*Session, any) {}},
		Metrics: rec,
		Limiter: mustLimiter(t, glimit.Policy{
			Kind: glimit.KindRequests, Scope: glimit.ScopeSession, Dir: glimit.DirIn, Rate: 1, Burst: 1,
		}),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(context.Background())

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte("a\nb\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	rec.waitFor(t, "close:tcp:rate_limited")
}

// recordingRejecter proves the protocol hook can answer before the close.
type recordingRejecter struct {
	mu        sync.Mutex
	decisions []glimit.Decision
}

func (r *recordingRejecter) Reject(_ context.Context, sess *Session, d glimit.Decision) bool {
	r.mu.Lock()
	r.decisions = append(r.decisions, d)
	r.mu.Unlock()
	return sess.WriteRaw([]byte("busy\n")) == nil
}

func (r *recordingRejecter) first(t *testing.T) glimit.Decision {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		if len(r.decisions) > 0 {
			d := r.decisions[0]
			r.mu.Unlock()
			return d
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the rejecter was never called")
	return glimit.Decision{}
}

func TestLimiterRejecterRendersRefusal(t *testing.T) {
	rejecter := &recordingRejecter{}
	srv, err := New(Config{
		Network:  "tcp",
		Addr:     "127.0.0.1:0",
		Codec:    LineCodec{},
		Handler:  echoHandler(),
		Limiter:  mustLimiter(t, glimit.Policy{Kind: glimit.KindConnections, Scope: glimit.ScopeGlobal, Max: 1}),
		Rejecter: rejecter,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(context.Background())

	first, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer first.Close()
	waitSessionCount(t, srv, 1)

	second, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	defer second.Close()
	_ = second.SetDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(second).ReadString('\n')
	if err != nil || line != "busy\n" {
		t.Fatalf("refusal frame = %q %v, want %q", line, err, "busy\n")
	}
	if d := rejecter.first(t); d.Reason != glimit.ReasonConcurrency {
		t.Fatalf("rejecter decision = %+v, want a concurrency denial", d)
	}
}

func TestLimitReasonMapping(t *testing.T) {
	tests := map[glimit.Reason]string{
		glimit.ReasonRate:        ReasonRateLimited,
		glimit.ReasonBurst:       ReasonBurstLimited,
		glimit.ReasonBytes:       ReasonByteLimited,
		glimit.ReasonConcurrency: ReasonMaxSessions,
		glimit.ReasonQuota:       ReasonQuotaExceeded,
		glimit.ReasonOverload:    ReasonOverloaded,
		glimit.ReasonUnavailable: ReasonUnavailable,
		"":                       ReasonRateLimited,
	}
	for reason, want := range tests {
		if got := limitReason(reason); got != want {
			t.Fatalf("limitReason(%q) = %q, want %q", reason, got, want)
		}
	}
}
