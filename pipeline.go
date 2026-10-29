package gnety

import (
	"log/slog"
	"sync"
)

// Middleware wraps a Handler, enabling composable pipelines.
type Middleware func(next Handler) Handler

// Chain composes middlewares around a Handler. The first middleware is the
// outermost (executed first).
func Chain(h Handler, middlewares ...Middleware) Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// Recover converts panics in handler callbacks into a session close + log.
func Recover(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next Handler) Handler {
		return HandlerFuncs{
			Open: func(s *Session) {
				defer recoverPanic(logger, s, "open")
				next.OnOpen(s)
			},
			Message: func(s *Session, msg any) {
				defer recoverPanic(logger, s, "message")
				next.OnMessage(s, msg)
			},
			Close: func(s *Session, err error) {
				defer recoverPanic(logger, s, "close")
				next.OnClose(s, err)
			},
		}
	}
}

func recoverPanic(logger *slog.Logger, s *Session, stage string) {
	if r := recover(); r != nil {
		logger.Error("gnety handler panic", "stage", stage, "session", s.ID, "panic", r)
		_ = s.Close()
	}
}

// Broadcast sends msg to all active sessions and returns the success count.
func (s *Server) Broadcast(msg any) int {
	s.mu.RLock()
	sessions := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.RUnlock()
	var wg sync.WaitGroup
	var ok sync.Map
	for _, sess := range sessions {
		wg.Add(1)
		go func(sess *Session) {
			defer wg.Done()
			if err := sess.Send(msg); err == nil {
				ok.Store(sess.ID, struct{}{})
			}
		}(sess)
	}
	wg.Wait()
	count := 0
	ok.Range(func(_, _ any) bool { count++; return true })
	return count
}
