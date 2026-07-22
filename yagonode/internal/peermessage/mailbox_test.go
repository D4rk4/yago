package peermessage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type failingPutEngine struct{}

func (failingPutEngine) Update(_ context.Context, fn func(vault.EngineTxn) error) error {
	return fn(failingPutTxn{})
}

func (failingPutEngine) View(_ context.Context, fn func(vault.EngineTxn) error) error {
	return fn(failingPutTxn{})
}
func (failingPutEngine) Provision(vault.Name) error               { return nil }
func (failingPutEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (failingPutEngine) QuotaBytes() int64                        { return 0 }
func (failingPutEngine) Close() error                             { return nil }

type failingPutTxn struct{}

func (failingPutTxn) Bucket(vault.Name) vault.EngineBucket { return failingPutBucket{} }
func (failingPutTxn) Writable() bool                       { return true }

type failingPutBucket struct{}

func (failingPutBucket) Get(vault.Key) []byte { return nil }
func (failingPutBucket) Put(vault.Key, []byte) error {
	return errors.New("put failed")
}
func (failingPutBucket) Delete(vault.Key) error { return nil }
func (failingPutBucket) Scan(vault.Key, func(vault.Key, []byte) (bool, error)) error {
	return nil
}

func (failingPutBucket) ReadPageAfter(vault.Key, int) (vault.BucketPage, error) {
	return vault.BucketPage{}, nil
}

func TestMailboxStoresMessagesDurably(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 1, 21, 0, 0, 0, time.UTC)
	mailbox, err := OpenMailbox(v, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	message := Message{
		FromName: "sender-peer",
		FromHash: hashFor("sender"),
		ToName:   "self-peer",
		ToHash:   hashFor("self"),
		Subject:  "hello",
		Body:     "body",
	}
	if err := mailbox.Receive(t.Context(), message); err != nil {
		t.Fatal(err)
	}

	messages, err := mailbox.Messages(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	got := messages[0]
	if !got.ReceivedAt.Equal(now) {
		t.Fatalf("ReceivedAt = %s, want %s", got.ReceivedAt, now)
	}
	if got.Subject != message.Subject || got.Body != message.Body {
		t.Fatalf("message = %+v, want %+v", got, message)
	}
}

func TestMailboxHonorsReadLimit(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 7, 1, 21, 0, 0, 0, time.UTC)
	mailbox, err := OpenMailbox(v, func() time.Time {
		clock = clock.Add(time.Second)

		return clock
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, subject := range []string{"first", "second"} {
		if err := mailbox.Receive(t.Context(), Message{
			FromHash: hashFor("sender"),
			ToHash:   hashFor("self"),
			Subject:  subject,
			Body:     "body",
		}); err != nil {
			t.Fatal(err)
		}
	}

	messages, err := mailbox.Messages(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	if messages[0].Subject != "first" {
		t.Fatalf("first subject = %q", messages[0].Subject)
	}
}

func TestMailboxEvictsOldestRecordsDeterministically(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	mailbox, err := OpenMailbox(v, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	mailbox.retention = mailboxRetention{records: 2, bytes: maximumMailboxBytes}
	base := time.Date(2026, 7, 1, 21, 0, 0, 0, time.UTC)
	for index, subject := range []string{"first", "second", "third"} {
		if err := mailbox.Receive(t.Context(), Message{
			ReceivedAt: base.Add(time.Duration(index) * time.Second),
			FromHash:   hashFor("sender"), ToHash: hashFor("self"), Subject: subject, Body: "body",
		}); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := mailbox.Messages(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Subject != "second" || messages[1].Subject != "third" {
		t.Fatalf("retained messages = %#v", messages)
	}
}

func TestMailboxEvictsOldestRecordsByEncodedBytes(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	mailbox, err := OpenMailbox(v, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	first := Message{
		ReceivedAt: time.Date(2026, 7, 1, 21, 0, 0, 0, time.UTC),
		FromHash:   hashFor("sender"), ToHash: hashFor("self"), Subject: "first", Body: "body",
	}
	second := first
	second.ReceivedAt = first.ReceivedAt.Add(time.Second)
	second.Subject = "second"
	encodedSecond, err := messageCodec{}.Encode(second)
	if err != nil {
		t.Fatal(err)
	}
	mailbox.retention = mailboxRetention{records: maximumMailboxRecords, bytes: len(encodedSecond)}
	if err := mailbox.Receive(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := mailbox.Receive(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	messages, err := mailbox.Messages(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Subject != "second" {
		t.Fatalf("retained messages = %#v", messages)
	}
}

func TestMailboxConcurrentReceiveKeepsBoundedNewestRecords(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	mailbox, err := OpenMailbox(v, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	mailbox.retention = mailboxRetention{records: 16, bytes: maximumMailboxBytes}
	base := time.Date(2026, 7, 1, 21, 0, 0, 0, time.UTC)
	var group sync.WaitGroup
	for index := range 64 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := mailbox.Receive(t.Context(), Message{
				ReceivedAt: base.Add(time.Duration(index) * time.Second),
				FromHash:   hashFor("sender"), ToHash: hashFor("self"),
				Subject: fmt.Sprintf("message-%02d", index), Body: "body",
			}); err != nil {
				t.Errorf("Receive(%d): %v", index, err)
			}
		}()
	}
	group.Wait()
	messages, err := mailbox.Messages(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 16 || messages[0].Subject != "message-48" ||
		messages[15].Subject != "message-63" {
		t.Fatalf(
			"retained range = %d/%q/%q",
			len(messages),
			messages[0].Subject,
			messages[len(messages)-1].Subject,
		)
	}
}

func TestMailboxRetentionSurvivesRestart(t *testing.T) {
	probe := newMailboxStorageProbe()
	storage := newMailboxProbeVault(t, probe)
	mailbox, err := OpenMailbox(storage, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	mailbox.retention = mailboxRetention{records: 2, bytes: maximumMailboxBytes}
	base := time.Date(2026, 7, 1, 21, 0, 0, 0, time.UTC)
	for index := range 3 {
		if err := mailbox.Receive(t.Context(), Message{
			ReceivedAt: base.Add(time.Duration(index) * time.Second),
			FromHash:   hashFor("sender"), ToHash: hashFor("self"),
			Subject: fmt.Sprintf("message-%d", index), Body: "body",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	storage = newMailboxProbeVault(t, probe)
	t.Cleanup(func() { _ = storage.Close() })
	reopened, err := OpenMailbox(storage, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := reopened.Messages(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Subject != "message-1" ||
		messages[1].Subject != "message-2" {
		t.Fatalf("reopened messages = %#v", messages)
	}
}

func TestOpenMailboxReturnsRegisterError(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := OpenMailbox(v, time.Now); err != nil {
		t.Fatalf("first OpenMailbox: %v", err)
	}
	if _, err := OpenMailbox(v, time.Now); err == nil {
		t.Fatal("expected duplicate mailbox register error")
	}
}

func TestMailboxReceiveReturnsWriteError(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	mailbox, err := OpenMailbox(v, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	err = mailbox.Receive(
		context.Background(),
		Message{FromHash: hashFor("sender"), Subject: "hello", Body: "body"},
	)
	if err == nil {
		t.Fatal("expected receive error after vault close")
	}
}

func TestMailboxReceiveReturnsStoreError(t *testing.T) {
	v, err := vault.New(failingPutEngine{})
	if err != nil {
		t.Fatal(err)
	}
	mailbox, err := OpenMailbox(v, time.Now)
	if err != nil {
		t.Fatal(err)
	}

	err = mailbox.Receive(
		context.Background(),
		Message{FromHash: hashFor("sender"), Subject: "hello", Body: "body"},
	)
	if err == nil {
		t.Fatal("expected store error")
	}
}

func TestMailboxMessagesReturnsReadError(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	mailbox, err := OpenMailbox(v, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := mailbox.Messages(context.Background(), 1); err == nil {
		t.Fatal("expected messages error after vault close")
	}
}

func TestMessageCodecDecodeReturnsJSONError(t *testing.T) {
	_, err := messageCodec{}.Decode([]byte("{"))
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestWrapReceiveMessageIgnoresNil(t *testing.T) {
	if err := wrapReceiveMessage(nil); err != nil {
		t.Fatalf("wrapReceiveMessage(nil) = %v", err)
	}
}

func TestWrapReceiveMessageWrapsError(t *testing.T) {
	sentinel := errors.New("failed")
	if err := wrapReceiveMessage(sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("wrapReceiveMessage = %v, want %v", err, sentinel)
	}
}
