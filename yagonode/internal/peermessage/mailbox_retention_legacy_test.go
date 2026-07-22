package peermessage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestRetainedMessageTailNeverExceedsItsRecordBound(t *testing.T) {
	tail := newRetainedMessageTail(mailboxRetention{records: 8, bytes: maximumMailboxBytes})
	for index := range 10_000 {
		tail.Add(retainedMessage{key: vault.Key(fmt.Sprintf("%08d", index)), bytes: 32})
	}
	if tail.length != 8 || len(tail.records) != 8 || cap(tail.records) != 8 {
		t.Fatalf(
			"tail shape = %d/%d/%d, want 8/8/8",
			tail.length,
			len(tail.records),
			cap(tail.records),
		)
	}
	if got := string(tail.OldestKey()); got != "00009992" {
		t.Fatalf("oldest retained key = %q", got)
	}
}

func TestMailboxPrunesOverCapLegacyRowsOnOpen(t *testing.T) {
	probe := newMailboxStorageProbe()
	storage := newMailboxProbeVault(t, probe)
	messages, err := registerMessages(storage)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		for index := range maximumMailboxRecords * 2 {
			message := Message{
				ReceivedAt: base.Add(time.Duration(index) * time.Second),
				FromHash:   hashFor("sender"), ToHash: hashFor("self"),
				Subject: fmt.Sprintf("message-%04d", index), Body: "body",
			}
			if err := messages.Put(tx, messageKey(message), message); err != nil {
				return fmt.Errorf("put legacy message: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	storage = newMailboxProbeVault(t, probe)
	t.Cleanup(func() { _ = storage.Close() })
	mailbox, err := OpenMailbox(storage, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := mailbox.Messages(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(retained) != maximumMailboxRecords ||
		retained[0].Subject != "message-1024" ||
		retained[len(retained)-1].Subject != "message-2047" {
		t.Fatalf(
			"retained = %d/%q/%q",
			len(retained), retained[0].Subject, retained[len(retained)-1].Subject,
		)
	}
}

func TestMailboxPruneHonorsCancellation(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	mailbox, err := OpenMailbox(storage, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := mailbox.prune(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("prune error = %v, want context cancellation", err)
	}
}

func TestMailboxReceiveCancellationDoesNotWaitForWriter(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	mailbox, err := OpenMailbox(storage, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := mailbox.writePermit.Acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer mailbox.writePermit.Release()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = mailbox.Receive(ctx, Message{FromHash: hashFor("sender"), Subject: "s", Body: "b"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("receive error = %v, want context cancellation", err)
	}
}
