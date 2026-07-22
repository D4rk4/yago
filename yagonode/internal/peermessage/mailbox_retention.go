package peermessage

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	maximumMailboxRecords     = 1024
	maximumMailboxBytes       = 8 << 20
	maximumStoredMessageBytes = 96 << 10
	mailboxRetentionPage      = 128
	mailboxScrubPage          = 512
	mailboxStartupTimeout     = 30 * time.Second
)

type mailboxRetention struct {
	records int
	bytes   int
}

type retainedMessage struct {
	key   vault.Key
	bytes int
}

func (m *Mailbox) prune(ctx context.Context) error {
	if err := m.trimMessageRecords(ctx); err != nil {
		return err
	}
	if err := m.scrubMessages(ctx); err != nil {
		return err
	}
	var retained *retainedMessageTail
	if err := m.vault.View(ctx, func(tx *vault.Txn) error {
		var err error
		retained, err = m.retainedMessages(ctx, tx)

		return err
	}); err != nil {
		return fmt.Errorf("inspect message retention: %w", err)
	}
	oldest := retained.OldestKey()
	if retained.observed == retained.length {
		m.retainedRecords = retained.length
		m.retainedBytes = retained.bytes

		return nil
	}
	for {
		deleted := 0
		if err := m.vault.Update(ctx, func(tx *vault.Txn) error {
			var err error
			deleted, err = m.deleteMessageBatchBefore(tx, oldest)

			return err
		}); err != nil {
			return fmt.Errorf("evict retained messages: %w", err)
		}
		if deleted == 0 {
			m.retainedRecords = retained.length
			m.retainedBytes = retained.bytes

			return nil
		}
	}
}

func (m *Mailbox) trimMessageRecords(ctx context.Context) error {
	for {
		trimmed := false
		if err := m.vault.Update(ctx, func(tx *vault.Txn) error {
			var err error
			trimmed, err = m.trimMessageRecordPage(ctx, tx)

			return err
		}); err != nil {
			return fmt.Errorf("trim message records: %w", err)
		}
		if !trimmed {
			return nil
		}
	}
}

func (m *Mailbox) trimMessageRecordPage(ctx context.Context, tx *vault.Txn) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("trim message records: %w", err)
	}
	records, err := m.messages.Len(tx)
	if err != nil {
		return false, fmt.Errorf("count retained messages: %w", err)
	}
	excess := records - m.retention.records
	if excess <= 0 {
		return false, nil
	}
	page, err := tx.ReadBucketKeyPage(messagesBucket, nil, min(excess, mailboxRetentionPage))
	if err != nil {
		return false, fmt.Errorf("read oldest message keys: %w", err)
	}
	if len(page.Keys) == 0 {
		return false, fmt.Errorf("trim message records: nonzero length without rows")
	}
	for _, key := range page.Keys {
		_, err := m.messages.Delete(tx, key)
		if err != nil {
			return false, fmt.Errorf("trim oldest message: %w", err)
		}
	}

	return true, nil
}

func storedMessageAdmitted(raw []byte) bool {
	if len(raw) > maximumStoredMessageBytes {
		return false
	}
	message, err := (messageCodec{}).Decode(raw)

	return err == nil && storedMessageContentAdmitted(message)
}

func storedMessageContentAdmitted(message Message) bool {
	return len(message.Subject) <= acceptedSubjectSize && len(message.Body) <= acceptedMessageSize
}

func (m *Mailbox) retainedMessages(
	ctx context.Context,
	tx *vault.Txn,
) (*retainedMessageTail, error) {
	tail := newRetainedMessageTail(m.retention)
	var after vault.Key
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("inspect message retention: %w", err)
		}
		page, err := tx.ReadBucketKeyPage(messagesBucket, after, mailboxRetentionPage)
		if err != nil {
			return nil, fmt.Errorf("read message retention page: %w", err)
		}
		for _, key := range page.Keys {
			size, _, err := m.messages.EncodedSize(tx, key)
			if err != nil {
				return nil, fmt.Errorf("measure retained message: %w", err)
			}
			tail.Add(retainedMessage{key: key, bytes: size})
		}
		if len(page.Keys) == 0 || !page.More {
			return tail, nil
		}
		after = page.Keys[len(page.Keys)-1]
	}
}

func (m *Mailbox) deleteMessageBatchBefore(tx *vault.Txn, oldest vault.Key) (int, error) {
	page, err := tx.ReadBucketKeyPage(messagesBucket, nil, mailboxRetentionPage)
	if err != nil {
		return 0, fmt.Errorf("read oldest message page: %w", err)
	}
	deleted := 0
	for _, key := range page.Keys {
		if oldest != nil && bytes.Compare(key, oldest) >= 0 {
			break
		}
		_, err := m.messages.Delete(tx, key)
		if err != nil {
			return 0, fmt.Errorf("evict oldest message: %w", err)
		}
		deleted++
	}

	return deleted, nil
}

type retainedMessageTail struct {
	records  []retainedMessage
	head     int
	length   int
	bytes    int
	observed int
	limit    mailboxRetention
}

func newRetainedMessageTail(retention mailboxRetention) *retainedMessageTail {
	capacity := max(retention.records, 0)

	return &retainedMessageTail{
		records: make([]retainedMessage, capacity),
		limit:   retention,
	}
}

func (t *retainedMessageTail) Add(record retainedMessage) {
	t.observed++
	if len(t.records) == 0 || t.limit.bytes <= 0 {
		return
	}
	if t.length == len(t.records) {
		t.removeOldest()
	}
	index := (t.head + t.length) % len(t.records)
	t.records[index] = record
	t.length++
	t.bytes += record.bytes
	for t.length > 0 && t.bytes > t.limit.bytes {
		t.removeOldest()
	}
}

func (t *retainedMessageTail) removeOldest() {
	oldest := t.records[t.head]
	t.records[t.head] = retainedMessage{}
	t.head = (t.head + 1) % len(t.records)
	t.length--
	t.bytes -= oldest.bytes
}

func (t *retainedMessageTail) OldestKey() vault.Key {
	if t.length == 0 {
		return nil
	}

	return append(vault.Key(nil), t.records[t.head].key...)
}
