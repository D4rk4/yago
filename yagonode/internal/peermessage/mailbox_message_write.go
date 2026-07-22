package peermessage

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type retainedMailboxState struct {
	records int
	bytes   int
}

type pendingMessageWrite struct {
	key          vault.Key
	message      Message
	encodedBytes int
	base         retainedMailboxState
	retained     retainedMailboxState
}

func (m *Mailbox) storeRetainedMessage(
	ctx context.Context,
	message Message,
	raw []byte,
) (retainedMailboxState, error) {
	write := pendingMessageWrite{
		key:          messageKey(message),
		message:      message,
		encodedBytes: len(raw),
		base: retainedMailboxState{
			records: m.retainedRecords,
			bytes:   m.retainedBytes,
		},
	}
	if err := m.vault.Update(ctx, func(tx *vault.Txn) error {
		write.retained = write.base

		return m.applyMessageWrite(ctx, tx, &write)
	}); err != nil {
		return retainedMailboxState{}, fmt.Errorf("store retained message: %w", err)
	}

	return write.retained, nil
}

func (m *Mailbox) applyMessageWrite(
	ctx context.Context,
	tx *vault.Txn,
	write *pendingMessageWrite,
) error {
	previousBytes, existed, err := m.messages.EncodedSize(tx, write.key)
	if err != nil {
		return fmt.Errorf("measure replaced message: %w", err)
	}
	if existed {
		write.retained.bytes -= previousBytes
	} else {
		write.retained.records++
	}
	if err := m.messages.Put(tx, write.key, write.message); err != nil {
		return fmt.Errorf("store message: %w", err)
	}
	write.retained.bytes += write.encodedBytes

	return m.evictMessagesAboveRetention(ctx, tx, &write.retained)
}

func (m *Mailbox) evictMessagesAboveRetention(
	ctx context.Context,
	tx *vault.Txn,
	retained *retainedMailboxState,
) error {
	for retained.records > m.retention.records || retained.bytes > m.retention.bytes {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("evict retained messages: %w", err)
		}
		page, err := tx.ReadBucketKeyPage(messagesBucket, nil, 1)
		if err != nil {
			return fmt.Errorf("read oldest message: %w", err)
		}
		if len(page.Keys) == 0 {
			return fmt.Errorf("evict oldest message: nonzero retention without rows")
		}
		oldest := page.Keys[0]
		size, _, err := m.messages.EncodedSize(tx, oldest)
		if err != nil {
			return fmt.Errorf("measure oldest message: %w", err)
		}
		if _, err := m.messages.Delete(tx, oldest); err != nil {
			return fmt.Errorf("evict oldest message: %w", err)
		}
		retained.records--
		retained.bytes -= size
	}

	return nil
}
