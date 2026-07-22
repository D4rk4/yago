package peermessage

import (
	"context"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var mailboxAdmissionKey = vault.Key("admission")

type storedMessageAdmission struct {
	message Message
	found   bool
}

func (m *Mailbox) storeMessageAdmission(ctx context.Context, raw []byte) error {
	err := m.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := m.cleanup.Put(tx, mailboxAdmissionKey, string(raw)); err != nil {
			return fmt.Errorf("store message admission: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("persist message admission: %w", err)
	}

	return nil
}

func (m *Mailbox) readMessageAdmission(ctx context.Context) (Message, bool, error) {
	var admission storedMessageAdmission
	err := m.vault.View(ctx, func(tx *vault.Txn) error {
		var err error
		admission, err = m.storedMessageAdmission(tx)

		return err
	})
	if err != nil {
		if !errors.Is(err, vault.ErrCorruptValue) {
			return Message{}, false, fmt.Errorf("read message admission: %w", err)
		}
		if err := m.clearMessageAdmission(ctx); err != nil {
			return Message{}, false, fmt.Errorf("discard invalid message admission: %w", err)
		}
	}

	return admission.message, admission.found, nil
}

func (m *Mailbox) storedMessageAdmission(
	tx *vault.Txn,
) (storedMessageAdmission, error) {
	raw, present, err := m.storedMailboxCleanup(tx, mailboxAdmissionKey)
	if err != nil {
		return storedMessageAdmission{}, err
	}
	if !present {
		return storedMessageAdmission{}, nil
	}
	message, err := (messageCodec{}).Decode([]byte(raw))
	if err != nil {
		return storedMessageAdmission{}, fmt.Errorf(
			"%w: decode message admission: %w", vault.ErrCorruptValue, err,
		)
	}
	if !storedMessageContentAdmitted(message) {
		return storedMessageAdmission{}, fmt.Errorf(
			"%w: invalid message admission content", vault.ErrCorruptValue,
		)
	}

	return storedMessageAdmission{message: message, found: true}, nil
}

func (m *Mailbox) clearMessageAdmission(ctx context.Context) error {
	err := m.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := m.cleanup.Delete(tx, mailboxAdmissionKey); err != nil {
			return fmt.Errorf("clear message admission: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("discard message admission: %w", err)
	}

	return nil
}

func (m *Mailbox) reconcilePendingMessage(ctx context.Context) error {
	message, found, err := m.readMessageAdmission(ctx)
	if err != nil {
		return err
	}
	if found {
		if err := m.vault.Update(ctx, func(tx *vault.Txn) error {
			if err := m.messages.Put(tx, messageKey(message), message); err != nil {
				return fmt.Errorf("restore pending message: %w", err)
			}

			return nil
		}); err != nil {
			m.retentionNeedsReconciliation = true

			return fmt.Errorf("reconcile pending message: %w", err)
		}
	}
	if err := m.prune(ctx); err != nil {
		m.retentionNeedsReconciliation = true

		return err
	}
	if found {
		if err := m.clearMessageAdmission(ctx); err != nil {
			m.retentionNeedsReconciliation = true

			return err
		}
	}
	m.retentionNeedsReconciliation = false

	return nil
}
