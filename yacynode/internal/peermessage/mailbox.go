package peermessage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type Mailbox struct {
	vault    *vault.Vault
	messages *vault.Collection[Message]
	now      func() time.Time
}

func (m *Mailbox) Receive(ctx context.Context, message Message) error {
	if message.ReceivedAt.IsZero() {
		message.ReceivedAt = m.now().UTC()
	}

	if err := m.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := m.messages.Put(tx, messageKey(message), message); err != nil {
			return fmt.Errorf("store message: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("write message: %w", err)
	}

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
