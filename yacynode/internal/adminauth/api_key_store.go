package adminauth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/vault"
)

//nolint:gosec // G101: storage bucket name, not a credential value.
const adminAPIKeysBucket vault.Name = "adminauth_api_keys"

type apiKeyRecord struct {
	SecretHash string    `json:"secretHash"`
	Scopes     []Scope   `json:"scopes"`
	Label      string    `json:"label"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}

type apiKeyRecordCodec struct{}

func (apiKeyRecordCodec) Encode(record apiKeyRecord) ([]byte, error) {
	data, _ := json.Marshal(record)

	return data, nil
}

func (apiKeyRecordCodec) Decode(raw []byte) (apiKeyRecord, error) {
	var record apiKeyRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return apiKeyRecord{}, fmt.Errorf("decode api key record: %w", err)
	}

	return record, nil
}

type apiKeyInfo struct {
	ID         string
	Scopes     []Scope
	Label      string
	CreatedAt  time.Time
	LastUsedAt time.Time
}

func (i apiKeyInfo) hasScope(required Scope) bool {
	for _, scope := range i.Scopes {
		if scope == required {
			return true
		}
	}

	return false
}

type createdAPIKey struct {
	ID        string
	Key       string
	Scopes    []Scope
	Label     string
	CreatedAt time.Time
}

type apiKeyStore struct {
	vault   *vault.Vault
	records *vault.Collection[apiKeyRecord]
	now     func() time.Time
}

func newAPIKeyStore(storage *vault.Vault, now func() time.Time) (*apiKeyStore, error) {
	records, err := vault.Register(storage, adminAPIKeysBucket, apiKeyRecordCodec{})
	if err != nil {
		return nil, fmt.Errorf("register api keys: %w", err)
	}

	return &apiKeyStore{vault: storage, records: records, now: now}, nil
}

func (s *apiKeyStore) create(
	ctx context.Context,
	label string,
	scopes []Scope,
) (createdAPIKey, error) {
	id, err := newRandomToken(apiKeyIDBytes)
	if err != nil {
		return createdAPIKey{}, err
	}
	secret, err := newRandomToken(apiKeySecretBytes)
	if err != nil {
		return createdAPIKey{}, err
	}
	record := apiKeyRecord{
		SecretHash: hashToken(secret),
		Scopes:     scopes,
		Label:      label,
		CreatedAt:  s.now(),
	}
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := s.records.Put(tx, vault.Key(id), record); err != nil {
			return fmt.Errorf("store api key record: %w", err)
		}

		return nil
	}); err != nil {
		return createdAPIKey{}, fmt.Errorf("update api keys: %w", err)
	}

	return createdAPIKey{
		ID:        id,
		Key:       formatAPIKey(id, secret),
		Scopes:    scopes,
		Label:     label,
		CreatedAt: record.CreatedAt,
	}, nil
}

func (s *apiKeyStore) authenticate(
	ctx context.Context,
	presented string,
) (apiKeyInfo, bool, error) {
	id, secret, ok := parseAPIKey(presented)
	if !ok {
		return apiKeyInfo{}, false, nil
	}

	var record apiKeyRecord
	found := false
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		got, ok, err := s.records.Get(tx, vault.Key(id))
		if err != nil {
			return fmt.Errorf("read api key record: %w", err)
		}
		record, found = got, ok

		return nil
	}); err != nil {
		return apiKeyInfo{}, false, fmt.Errorf("view api keys: %w", err)
	}
	if !found || subtle.ConstantTimeCompare(
		[]byte(hashToken(secret)),
		[]byte(record.SecretHash),
	) != 1 {
		return apiKeyInfo{}, false, nil
	}

	record.LastUsedAt = s.now()
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := s.records.Put(tx, vault.Key(id), record); err != nil {
			return fmt.Errorf("touch api key record: %w", err)
		}

		return nil
	}); err != nil {
		return apiKeyInfo{}, false, fmt.Errorf("update api keys: %w", err)
	}

	return infoFromRecord(id, record), true, nil
}

func (s *apiKeyStore) list(ctx context.Context) ([]apiKeyInfo, error) {
	var infos []apiKeyInfo
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		return s.records.Scan(tx, nil, func(key vault.Key, record apiKeyRecord) (bool, error) {
			infos = append(infos, infoFromRecord(string(key), record))

			return true, nil
		})
	}); err != nil {
		return nil, fmt.Errorf("view api keys: %w", err)
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].CreatedAt.Equal(infos[j].CreatedAt) {
			return infos[i].ID < infos[j].ID
		}

		return infos[i].CreatedAt.Before(infos[j].CreatedAt)
	})

	return infos, nil
}

func (s *apiKeyStore) delete(ctx context.Context, id string) (bool, error) {
	deleted := false
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		ok, err := s.records.Delete(tx, vault.Key(id))
		if err != nil {
			return fmt.Errorf("delete api key record: %w", err)
		}
		deleted = ok

		return nil
	}); err != nil {
		return false, fmt.Errorf("update api keys: %w", err)
	}

	return deleted, nil
}

func infoFromRecord(id string, record apiKeyRecord) apiKeyInfo {
	return apiKeyInfo{
		ID:         id,
		Scopes:     record.Scopes,
		Label:      record.Label,
		CreatedAt:  record.CreatedAt,
		LastUsedAt: record.LastUsedAt,
	}
}
