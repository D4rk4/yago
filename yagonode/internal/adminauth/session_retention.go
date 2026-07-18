package adminauth

import (
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	maximumAdminSessions      = 256
	sessionRetentionBatchSize = maximumAdminSessions
)

type retainedSession struct {
	key       vault.Key
	expiresAt time.Time
}

func (s *sessionStore) prepareSessionCapacity(
	tx *vault.Txn,
	now time.Time,
) (bool, error) {
	length, err := s.records.Len(tx)
	if err != nil {
		return false, fmt.Errorf("measure session records: %w", err)
	}
	if length > maximumAdminSessions {
		return s.shedLegacySessionCapacity(tx, length)
	}
	page := newSessionCapacityPage(now)
	if err := s.records.Scan(tx, nil, func(key vault.Key, record sessionRecord) (bool, error) {
		page.observe(key, record)

		return true, nil
	}); err != nil {
		return false, fmt.Errorf("scan session records: %w", err)
	}
	for _, key := range page.removals() {
		if _, err := s.records.Delete(tx, key); err != nil {
			return false, fmt.Errorf("delete retained session record: %w", err)
		}
	}

	return true, nil
}

func (s *sessionStore) shedLegacySessionCapacity(
	tx *vault.Txn,
	length int,
) (bool, error) {
	remove := min(length-maximumAdminSessions+1, sessionRetentionBatchSize)
	keys := make([]vault.Key, 0, remove)
	if err := s.records.Scan(tx, nil, func(key vault.Key, _ sessionRecord) (bool, error) {
		keys = append(keys, key)

		return len(keys) < remove, nil
	}); err != nil {
		return false, fmt.Errorf("scan legacy session records: %w", err)
	}
	if len(keys) != remove {
		return false, fmt.Errorf(
			"legacy session length mismatch: measured %d, scanned %d",
			length,
			len(keys),
		)
	}
	for _, key := range keys {
		if _, err := s.records.Delete(tx, key); err != nil {
			return false, fmt.Errorf("delete legacy session record: %w", err)
		}
	}

	return false, nil
}
