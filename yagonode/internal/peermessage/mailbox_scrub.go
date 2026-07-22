package peermessage

import (
	"context"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type mailboxScrubProgress struct {
	after vault.Key
	more  bool
}

func (m *Mailbox) scrubMessages(ctx context.Context) error {
	after, err := m.scrubCursor(ctx)
	if err != nil {
		return fmt.Errorf("read message scrub cursor: %w", err)
	}
	valid, err := m.scrubbedPrefixValid(ctx, after)
	if err != nil {
		return fmt.Errorf("validate message scrub cursor: %w", err)
	}
	if !valid {
		if err := m.clearScrubCursor(ctx); err != nil {
			return fmt.Errorf("reset message scrub cursor: %w", err)
		}
		after = nil
	}
	for {
		next, err := m.scrubMessagePage(ctx, after)
		if err != nil {
			return fmt.Errorf("scrub retained messages: %w", err)
		}
		if next.after == nil {
			return nil
		}
		if err := m.storeScrubCursor(ctx, next.after); err != nil {
			return fmt.Errorf("checkpoint retained messages: %w", err)
		}
		if !next.more {
			return nil
		}
		after = next.after
	}
}

func (m *Mailbox) scrubMessagePage(
	ctx context.Context,
	after vault.Key,
) (mailboxScrubProgress, error) {
	var progress mailboxScrubProgress
	err := m.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("read message scrub page: %w", err)
		}
		page, err := tx.ReadBucketKeyPage(messagesBucket, after, mailboxScrubPage)
		if err != nil {
			return fmt.Errorf("read message scrub page: %w", err)
		}
		if len(page.Keys) == 0 {
			return nil
		}
		key := page.Keys[len(page.Keys)-1]
		progress = mailboxScrubProgress{
			after: append(vault.Key(nil), key...),
			more:  page.More,
		}
		for _, candidate := range page.Keys {
			if err := m.validateStoredMessageRecord(tx, candidate); err == nil {
				continue
			} else if !errors.Is(err, vault.ErrCorruptValue) {
				return fmt.Errorf("inspect retained message: %w", err)
			}
			if _, err := m.messages.Delete(tx, candidate); err != nil {
				return fmt.Errorf("evict invalid message: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return mailboxScrubProgress{}, fmt.Errorf("scrub message page: %w", err)
	}

	return progress, nil
}

func (m *Mailbox) validateStoredMessageRecord(
	tx *vault.Txn,
	key vault.Key,
) error {
	size, found, err := m.messages.EncodedSize(tx, key)
	if err != nil {
		return fmt.Errorf("measure stored message: %w", err)
	}
	if !found {
		return fmt.Errorf(
			"%w: retained message disappeared during size inspection", vault.ErrCorruptValue,
		)
	}
	if size > maximumStoredMessageBytes {
		return fmt.Errorf(
			"%w: retained message size %d", vault.ErrCorruptValue, size,
		)
	}
	message, present, err := m.messages.Get(tx, key)
	if err != nil {
		return fmt.Errorf("read stored message: %w", err)
	}
	if !present {
		return fmt.Errorf(
			"%w: retained message disappeared during read", vault.ErrCorruptValue,
		)
	}
	if !storedMessageContentAdmitted(message) {
		return fmt.Errorf(
			"%w: invalid retained message content", vault.ErrCorruptValue,
		)
	}

	return nil
}
