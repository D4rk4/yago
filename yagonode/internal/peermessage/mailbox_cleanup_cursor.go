package peermessage

import (
	"context"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	mailboxCleanupBucket            vault.Name = "peermessage-cleanup"
	maximumMailboxCleanupValueBytes            = maximumStoredMessageBytes
)

var mailboxScrubCursorKey = vault.Key("scrub")

type mailboxCleanupCodec struct{}

func (mailboxCleanupCodec) Encode(value string) ([]byte, error) {
	if value == "" || len(value) > maximumMailboxCleanupValueBytes {
		return nil, fmt.Errorf("invalid message cleanup cursor")
	}

	return []byte(value), nil
}

func (mailboxCleanupCodec) Decode(raw []byte) (string, error) {
	return mailboxCleanupCodec{}.decode(string(raw))
}

func (mailboxCleanupCodec) decode(value string) (string, error) {
	if value == "" || len(value) > maximumMailboxCleanupValueBytes {
		return "", fmt.Errorf("invalid message cleanup cursor")
	}

	return value, nil
}

func registerMailboxCleanup(v *vault.Vault) (*vault.Keyspace[string], error) {
	cleanup, err := vault.RegisterKeyspace(v, mailboxCleanupBucket, mailboxCleanupCodec{})
	if err != nil {
		return nil, fmt.Errorf("register peer message cleanup: %w", err)
	}

	return cleanup, nil
}

func (m *Mailbox) scrubCursor(ctx context.Context) (vault.Key, error) {
	var after vault.Key
	err := m.vault.View(ctx, func(tx *vault.Txn) error {
		value, found, err := m.storedMailboxCleanup(tx, mailboxScrubCursorKey)
		if err != nil {
			return err
		}
		if found {
			after = vault.Key(value)
		}

		return nil
	})
	if err != nil {
		if !errors.Is(err, vault.ErrCorruptValue) {
			return nil, fmt.Errorf("read message scrub cursor: %w", err)
		}
		if err := m.clearScrubCursor(ctx); err != nil {
			return nil, fmt.Errorf("discard message scrub cursor: %w", err)
		}
	}

	return after, nil
}

func (m *Mailbox) storedMailboxCleanup(
	tx *vault.Txn,
	key vault.Key,
) (string, bool, error) {
	size, found, err := m.cleanup.EncodedSize(tx, key)
	if err != nil {
		return "", found, fmt.Errorf("measure message cleanup value: %w", err)
	}
	if !found {
		return "", false, nil
	}
	if size > maximumMailboxCleanupValueBytes {
		return "", true, fmt.Errorf(
			"%w: invalid message cleanup value size %d", vault.ErrCorruptValue, size,
		)
	}

	value, present, err := m.cleanup.Get(tx, key)
	if err != nil {
		return "", true, fmt.Errorf("read message cleanup value: %w", err)
	}
	if !present {
		return "", true, fmt.Errorf(
			"%w: message cleanup value disappeared during read", vault.ErrCorruptValue,
		)
	}

	return value, true, nil
}

func (m *Mailbox) storeScrubCursor(ctx context.Context, after vault.Key) error {
	err := m.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := m.cleanup.Put(tx, mailboxScrubCursorKey, string(after)); err != nil {
			return fmt.Errorf("store message scrub cursor: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("persist message scrub cursor: %w", err)
	}

	return nil
}

func (m *Mailbox) clearScrubCursor(ctx context.Context) error {
	err := m.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := m.cleanup.Delete(tx, mailboxScrubCursorKey); err != nil {
			return fmt.Errorf("clear message scrub cursor: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("discard message scrub cursor: %w", err)
	}

	return nil
}

func (m *Mailbox) scrubbedPrefixValid(ctx context.Context, through vault.Key) (bool, error) {
	if through == nil {
		return true, nil
	}
	valid := false
	err := m.vault.View(ctx, func(tx *vault.Txn) error {
		var err error
		valid, err = m.scrubbedPrefixValidInTransaction(tx, through)

		return err
	})
	if err != nil {
		return false, fmt.Errorf("validate scrubbed message prefix: %w", err)
	}

	return valid, nil
}

func (m *Mailbox) scrubbedPrefixValidInTransaction(
	tx *vault.Txn,
	through vault.Key,
) (bool, error) {
	var after vault.Key
	for {
		page, err := tx.ReadBucketKeyPage(messagesBucket, after, mailboxScrubPage)
		if err != nil {
			return false, fmt.Errorf("read scrubbed message prefix: %w", err)
		}
		for _, key := range page.Keys {
			if string(key) > string(through) {
				return true, nil
			}
			if err := m.validateStoredMessageRecord(tx, key); err != nil {
				if errors.Is(err, vault.ErrCorruptValue) {
					return false, nil
				}

				return false, fmt.Errorf("inspect scrubbed message prefix: %w", err)
			}
		}
		if len(page.Keys) == 0 || !page.More {
			return true, nil
		}
		after = page.Keys[len(page.Keys)-1]
	}
}
