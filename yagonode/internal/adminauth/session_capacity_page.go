package adminauth

import (
	"bytes"
	"sort"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type sessionCapacityPage struct {
	now     time.Time
	active  []retainedSession
	expired []vault.Key
}

func newSessionCapacityPage(now time.Time) *sessionCapacityPage {
	return &sessionCapacityPage{
		now:     now,
		active:  make([]retainedSession, 0, maximumAdminSessions),
		expired: make([]vault.Key, 0, maximumAdminSessions),
	}
}

func (p *sessionCapacityPage) observe(key vault.Key, record sessionRecord) {
	if !p.now.Before(record.ExpiresAt) {
		p.expired = append(p.expired, key)

		return
	}
	p.active = append(p.active, retainedSession{key: key, expiresAt: record.ExpiresAt})
}

func (p *sessionCapacityPage) removals() []vault.Key {
	remove := append([]vault.Key(nil), p.expired...)
	excess := len(p.active) - maximumAdminSessions + 1
	if excess <= 0 {
		return remove
	}
	sort.Slice(p.active, func(left, right int) bool {
		if p.active[left].expiresAt.Equal(p.active[right].expiresAt) {
			return bytes.Compare(p.active[left].key, p.active[right].key) < 0
		}

		return p.active[left].expiresAt.Before(p.active[right].expiresAt)
	})
	for index := range excess {
		remove = append(remove, p.active[index].key)
	}

	return remove
}
