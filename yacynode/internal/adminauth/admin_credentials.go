package adminauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yacynode/internal/vault"
)

//nolint:gosec // G101: storage bucket name, not a credential value.
const adminCredentialsBucket vault.Name = "adminauth_credentials"

var adminKey = vault.Key("admin")

var errAdminExists = errors.New("an administrator already exists")

// dummyPasswordHash lets verify spend comparable Argon2id time when the admin
// record is missing or the username does not match, so a failed login does not
// reveal whether the account exists through response timing. The salt is fixed
// because the recomputed key is always discarded.
var dummyPasswordHash = encodeArgon2id(
	"timing-equalization-placeholder",
	make([]byte, argonSaltLength),
	argon2Params{
		memory:      argonMemoryKiB,
		iterations:  argonIterations,
		parallelism: argonParallelism,
	},
	argonKeyLength,
)

type adminRecord struct {
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
}

type adminRecordCodec struct{}

func (adminRecordCodec) Encode(record adminRecord) ([]byte, error) {
	data, _ := json.Marshal(record)

	return data, nil
}

func (adminRecordCodec) Decode(raw []byte) (adminRecord, error) {
	var record adminRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return adminRecord{}, fmt.Errorf("decode admin record: %w", err)
	}

	return record, nil
}

type credentialStore struct {
	vault   *vault.Vault
	records *vault.Collection[adminRecord]
}

func newCredentialStore(storage *vault.Vault) (*credentialStore, error) {
	records, err := vault.Register(storage, adminCredentialsBucket, adminRecordCodec{})
	if err != nil {
		return nil, fmt.Errorf("register admin credentials: %w", err)
	}

	return &credentialStore{vault: storage, records: records}, nil
}

func (s *credentialStore) exists(ctx context.Context) (bool, error) {
	found := false
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		_, ok, err := s.records.Get(tx, adminKey)
		if err != nil {
			return fmt.Errorf("read admin record: %w", err)
		}
		found = ok

		return nil
	}); err != nil {
		return false, fmt.Errorf("view admin credentials: %w", err)
	}

	return found, nil
}

func (s *credentialStore) setAdmin(ctx context.Context, username, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := s.records.Put(tx, adminKey, adminRecord{
			Username:     username,
			PasswordHash: hash,
		}); err != nil {
			return fmt.Errorf("store admin record: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("update admin credentials: %w", err)
	}

	return nil
}

func (s *credentialStore) createIfAbsent(ctx context.Context, username, password string) error {
	present, err := s.exists(ctx)
	if err != nil {
		return err
	}
	if present {
		return errAdminExists
	}

	return s.setAdmin(ctx, username, password)
}

func (s *credentialStore) verify(ctx context.Context, username, password string) (bool, error) {
	var record adminRecord
	found := false
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		got, ok, err := s.records.Get(tx, adminKey)
		if err != nil {
			return fmt.Errorf("read admin record: %w", err)
		}
		record, found = got, ok

		return nil
	}); err != nil {
		return false, fmt.Errorf("view admin credentials: %w", err)
	}

	if !found || record.Username != username {
		_, _ = verifyPassword(dummyPasswordHash, password)

		return false, nil
	}

	ok, err := verifyPassword(record.PasswordHash, password)
	if err != nil {
		return false, err
	}

	return ok, nil
}
