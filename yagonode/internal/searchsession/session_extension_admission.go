package searchsession

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func (s *stableSearcher) extend(ctx context.Context, entry *session, req searchcore.Request) error {
	if window := entry.visible.Load(); window != nil && !window.needsExtension(req) {
		return nil
	}
	if err := entry.acquireExtension(ctx); err != nil {
		return err
	}
	defer func() {
		retained := retainedSessionBytes(entry)
		entry.replaceVisibleWindowLocked()
		<-entry.extension
		s.refreshRetention(entry, retained)
	}()

	return s.extendWindow(ctx, entry, req)
}

func (entry *session) acquireExtension(ctx context.Context) error {
	select {
	case entry.extension <- struct{}{}:
		if cause := context.Cause(ctx); cause != nil {
			<-entry.extension
			return fmt.Errorf("session extension: %w", cause)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("session extension: %w", context.Cause(ctx))
	}
}

func (window *sessionWindow) needsExtension(request searchcore.Request) bool {
	return request.Offset < window.total &&
		min(requestedLookaheadEnd(request), window.total) > len(window.results)
}
