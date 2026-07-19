package adminauth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const adminSessionsBucket vault.Name = "adminauth_sessions"

type sessionRecord struct {
	Username  string    `json:"username"`
	CSRFToken string    `json:"csrfToken"`
	ExpiresAt time.Time `json:"expiresAt"`
	RenewAt   time.Time `json:"renewAt,omitempty"`
}

type sessionRecordCodec struct{}

func (sessionRecordCodec) Encode(record sessionRecord) ([]byte, error) {
	data, _ := json.Marshal(record)

	return data, nil
}

func (sessionRecordCodec) Decode(raw []byte) (sessionRecord, error) {
	var record sessionRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return sessionRecord{}, fmt.Errorf("decode session record: %w", err)
	}

	return record, nil
}

type sessionStore struct {
	vault   *vault.Vault
	records *vault.Collection[sessionRecord]
	ttl     time.Duration
	renewal time.Duration
	now     func() time.Time
}

func newSessionStore(
	storage *vault.Vault,
	ttl time.Duration,
	now func() time.Time,
) (*sessionStore, error) {
	records, err := vault.Register(storage, adminSessionsBucket, sessionRecordCodec{})
	if err != nil {
		return nil, fmt.Errorf("register admin sessions: %w", err)
	}

	return &sessionStore{
		vault: storage, records: records, ttl: ttl, renewal: sessionRenewalInterval(ttl), now: now,
	}, nil
}

func (s *sessionStore) create(ctx context.Context, username string) (session, error) {
	token, err := newRandomToken(sessionTokenBytes)
	if err != nil {
		return session{}, err
	}
	csrf, err := newRandomToken(csrfTokenBytes)
	if err != nil {
		return session{}, err
	}
	now := s.now()
	record := sessionRecord{
		Username:  username,
		CSRFToken: csrf,
		ExpiresAt: now.Add(s.ttl),
		RenewAt:   now.Add(s.renewal),
	}
	admitted := false
	for !admitted {
		if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
			admitted = false
			ready, err := s.prepareSessionCapacity(tx, now)
			if err != nil {
				return err
			}
			if !ready {
				return nil
			}
			if err := s.records.Put(tx, vault.Key(hashToken(token)), record); err != nil {
				return fmt.Errorf("store session record: %w", err)
			}
			admitted = true

			return nil
		}); err != nil {
			return session{}, fmt.Errorf("update admin sessions: %w", err)
		}
	}

	return session{
		Token:     token,
		Username:  record.Username,
		CSRFToken: record.CSRFToken,
		ExpiresAt: record.ExpiresAt,
	}, nil
}

func sessionRenewalInterval(ttl time.Duration) time.Duration {
	const maximum = time.Hour
	interval := ttl / 2
	if interval > maximum {
		return maximum
	}

	return interval
}

func (s *sessionStore) rotate(
	ctx context.Context,
	token string,
	record sessionRecord,
) (session, bool, error) {
	now := s.now()
	if !record.RenewAt.IsZero() && now.Before(record.RenewAt) {
		return session{}, false, nil
	}
	if !now.Before(record.ExpiresAt) {
		return session{}, false, nil
	}

	replacement, err := newRandomToken(sessionTokenBytes)
	if err != nil {
		return session{}, false, err
	}
	expected := record
	record.RenewAt = now.Add(s.renewal)
	if record.RenewAt.After(record.ExpiresAt) {
		record.RenewAt = record.ExpiresAt
	}

	rotated := false
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		current, found, err := s.records.Get(tx, vault.Key(hashToken(token)))
		if err != nil {
			return fmt.Errorf("read session for rotation: %w", err)
		}
		if !found || current != expected {
			return nil
		}
		if err := s.records.Put(tx, vault.Key(hashToken(replacement)), record); err != nil {
			return fmt.Errorf("store rotated session: %w", err)
		}
		if _, err := s.records.Delete(tx, vault.Key(hashToken(token))); err != nil {
			return fmt.Errorf("delete replaced session: %w", err)
		}
		rotated = true

		return nil
	}); err != nil {
		return session{}, false, fmt.Errorf("rotate admin session: %w", err)
	}
	if !rotated {
		return session{}, false, nil
	}

	return session{
		Token: replacement, Username: record.Username, CSRFToken: record.CSRFToken,
		ExpiresAt: record.ExpiresAt,
	}, true, nil
}

func (s *sessionStore) lookup(ctx context.Context, token string) (sessionRecord, bool, error) {
	var record sessionRecord
	found := false
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		got, ok, err := s.records.Get(tx, vault.Key(hashToken(token)))
		if err != nil {
			return fmt.Errorf("read session record: %w", err)
		}
		record, found = got, ok

		return nil
	}); err != nil {
		return sessionRecord{}, false, fmt.Errorf("view admin sessions: %w", err)
	}
	if !found {
		return sessionRecord{}, false, nil
	}
	if !s.now().Before(record.ExpiresAt) {
		if err := s.delete(ctx, token); err != nil {
			return sessionRecord{}, false, err
		}

		return sessionRecord{}, false, nil
	}

	return record, true, nil
}

func (s *sessionStore) delete(ctx context.Context, token string) error {
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := s.records.Delete(tx, vault.Key(hashToken(token))); err != nil {
			return fmt.Errorf("delete session record: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("update admin sessions: %w", err)
	}

	return nil
}
