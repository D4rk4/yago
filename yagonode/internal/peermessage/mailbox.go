package peermessage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type Mailbox struct {
	writePermit                  mailboxWritePermit
	vault                        *vault.Vault
	messages                     *vault.Collection[Message]
	cleanup                      *vault.Keyspace[string]
	now                          func() time.Time
	retention                    mailboxRetention
	retainedRecords              int
	retainedBytes                int
	retentionNeedsReconciliation bool
}

func (m *Mailbox) Receive(ctx context.Context, message Message) error {
	if err := m.writePermit.Acquire(ctx); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	defer m.writePermit.Release()
	if m.retentionNeedsReconciliation {
		if err := m.reconcilePendingMessage(ctx); err != nil {
			return fmt.Errorf("reconcile message retention: %w", err)
		}
	}

	if message.ReceivedAt.IsZero() {
		message.ReceivedAt = m.now().UTC()
	}
	raw, _ := (messageCodec{}).Encode(message)
	if !storedMessageAdmitted(raw) {
		return fmt.Errorf("write message: message exceeds storage admission limits")
	}
	if err := m.storeMessageAdmission(ctx, raw); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	retained, err := m.storeRetainedMessage(ctx, message, raw)
	if err != nil {
		m.retentionNeedsReconciliation = true

		return fmt.Errorf("write message: %w", err)
	}
	if err := m.clearMessageAdmission(ctx); err != nil {
		m.retentionNeedsReconciliation = true

		return fmt.Errorf("write message: finish message admission: %w", err)
	}
	m.retainedRecords = retained.records
	m.retainedBytes = retained.bytes

	return nil
}

func (m *Mailbox) Messages(ctx context.Context, limit int) ([]Message, error) {
	var messages []Message
	if err := m.vault.View(ctx, func(tx *vault.Txn) error {
		return m.messages.Scan(tx, nil, func(_ vault.Key, message Message) (bool, error) {
			messages = append(messages, message)

			return limit <= 0 || len(messages) < limit, nil
		})
	}); err != nil {
		return nil, fmt.Errorf("read messages: %w", err)
	}

	return messages, nil
}

func messageKey(message Message) vault.Key {
	sum := sha256.Sum256(
		[]byte(message.FromHash.String() + "\x00" + message.Subject + "\x00" + message.Body),
	)

	return vault.Key(
		fmt.Sprintf(
			"%020d:%s:%x",
			message.ReceivedAt.UnixNano(),
			message.FromHash.String(),
			sum[:8],
		),
	)
}
