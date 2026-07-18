package adminauth

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	apiKeyLastUsedRefreshInterval = 5 * time.Minute
	lastUsedUpdateFailedEvent     = "api key last-used update failed"
)

type apiKeyLastUsedRecorder struct {
	mu            sync.Mutex
	nextAttempt   map[string]time.Time
	warningActive atomic.Bool
}

func apiKeyLastUsedRefreshDue(lastUsedAt, now time.Time) bool {
	return lastUsedAt.IsZero() || !now.Before(lastUsedAt.Add(apiKeyLastUsedRefreshInterval))
}

func (r *apiKeyLastUsedRecorder) claim(id string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	next, found := r.nextAttempt[id]
	if found {
		if now.Before(next) {
			return false
		}
	} else if len(r.nextAttempt) >= maximumAPIKeys {
		return false
	}
	if r.nextAttempt == nil {
		r.nextAttempt = make(map[string]time.Time, maximumAPIKeys)
	}
	r.nextAttempt[id] = now.Add(apiKeyLastUsedRefreshInterval)

	return true
}

func (r *apiKeyLastUsedRecorder) forget(id string) {
	r.mu.Lock()
	delete(r.nextAttempt, id)
	r.mu.Unlock()
}

func (s *apiKeyStore) recordLastUsedBestEffort(ctx context.Context, info apiKeyInfo) {
	now := s.now()
	if !apiKeyLastUsedRefreshDue(info.LastUsedAt, now) ||
		!s.lastUsedRecorder.claim(info.ID, now) {
		return
	}
	found, err := s.touchLastUsed(ctx, info.ID)
	if err != nil {
		if s.lastUsedRecorder.warningActive.CompareAndSwap(false, true) {
			slog.WarnContext(ctx, lastUsedUpdateFailedEvent, slog.Any("error", err))
		}

		return
	}
	if !found {
		s.lastUsedRecorder.forget(info.ID)

		return
	}
	s.lastUsedRecorder.warningActive.Store(false)
}
