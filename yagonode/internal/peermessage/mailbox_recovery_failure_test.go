package peermessage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestMailboxCleanupCodecBounds(t *testing.T) {
	codec := mailboxCleanupCodec{}
	for _, value := range []string{"", strings.Repeat("x", maximumMailboxCleanupValueBytes+1)} {
		if _, err := codec.Encode(value); err == nil {
			t.Fatalf("Encode(%d bytes) succeeded", len(value))
		}
		if _, err := codec.Decode([]byte(value)); err == nil {
			t.Fatalf("Decode(%d bytes) succeeded", len(value))
		}
	}
}

func TestMailboxAdmissionRecoverySurfacesStorageFailures(t *testing.T) {
	failure := errors.New("message recovery storage failed")
	tests := []struct {
		name     string
		exercise func(*testing.T, error)
	}{
		{name: "read cancellation", exercise: mailboxAdmissionReadCancellation},
		{name: "reconcile read cancellation", exercise: mailboxAdmissionReconcileCancellation},
		{name: "discard invalid", exercise: mailboxAdmissionDiscardInvalid},
		{name: "discard invalid failure", exercise: mailboxAdmissionDiscardFailure},
		{name: "clear failure", exercise: mailboxAdmissionClearFailure},
		{name: "clear cancellation", exercise: mailboxAdmissionClearCancellation},
		{name: "restore failure", exercise: mailboxAdmissionRestoreFailure},
		{name: "prune failure", exercise: mailboxAdmissionPruneFailure},
		{name: "finish failure", exercise: mailboxAdmissionFinishFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.exercise(t, failure)
		})
	}
}

func mailboxAdmissionReadCancellation(t *testing.T, _ error) {
	mailbox, _ := openMailboxProbe(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := mailbox.readMessageAdmission(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("read error = %v", err)
	}
}

func mailboxAdmissionReconcileCancellation(t *testing.T, _ error) {
	mailbox, _ := openMailboxProbe(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := mailbox.reconcilePendingMessage(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("reconcile error = %v", err)
	}
}

func mailboxAdmissionDiscardInvalid(t *testing.T, _ error) {
	mailbox, probe := openMailboxProbe(t)
	probe.buckets[mailboxCleanupBucket][string(mailboxAdmissionKey)] = []byte("{")
	message, found, err := mailbox.readMessageAdmission(t.Context())
	if err != nil || found || message != (Message{}) ||
		len(probe.buckets[mailboxCleanupBucket]) != 0 {
		t.Fatalf("invalid admission = %#v/%t/%v", message, found, err)
	}
}

func mailboxAdmissionDiscardFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	probe.buckets[mailboxCleanupBucket][string(mailboxAdmissionKey)] = []byte("{")
	probe.deleteFailures[mailboxCleanupBucket] = failure
	if _, _, err := mailbox.readMessageAdmission(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("discard error = %v", err)
	}
}

func mailboxAdmissionClearFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	probe.buckets[mailboxCleanupBucket][string(mailboxAdmissionKey)] = []byte("pending")
	probe.deleteFailures[mailboxCleanupBucket] = failure
	if err := mailbox.clearMessageAdmission(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("clear error = %v", err)
	}
}

func mailboxAdmissionClearCancellation(t *testing.T, _ error) {
	mailbox, _ := openMailboxProbe(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := mailbox.clearMessageAdmission(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("clear error = %v", err)
	}
}

func mailboxAdmissionRestoreFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	storeMailboxPendingMessage(t, mailbox)
	probe.putFailures[messagesBucket] = failure
	if err := mailbox.reconcilePendingMessage(t.Context()); !errors.Is(err, failure) ||
		!mailbox.retentionNeedsReconciliation {
		t.Fatalf("restore error = %v dirty=%t", err, mailbox.retentionNeedsReconciliation)
	}
}

func mailboxAdmissionPruneFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	probe.keyPageFailureOn = true
	probe.keyPageFailure = failure
	if err := mailbox.reconcilePendingMessage(t.Context()); !errors.Is(err, failure) ||
		!mailbox.retentionNeedsReconciliation {
		t.Fatalf("prune error = %v dirty=%t", err, mailbox.retentionNeedsReconciliation)
	}
}

func mailboxAdmissionFinishFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	storeMailboxPendingMessage(t, mailbox)
	probe.deleteFailures[mailboxCleanupBucket] = failure
	if err := mailbox.reconcilePendingMessage(t.Context()); !errors.Is(err, failure) ||
		!mailbox.retentionNeedsReconciliation {
		t.Fatalf("finish error = %v dirty=%t", err, mailbox.retentionNeedsReconciliation)
	}
}

func storeMailboxPendingMessage(t *testing.T, mailbox *Mailbox) {
	t.Helper()
	message := Message{FromHash: hashFor("sender"), Subject: "pending", Body: "body"}
	raw, err := (messageCodec{}).Encode(message)
	if err != nil {
		t.Fatal(err)
	}
	if err := mailbox.storeMessageAdmission(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
}

func TestMailboxCleanupPointReadsRejectOversizedValuesBeforeDecode(t *testing.T) {
	for _, test := range []struct {
		name string
		key  vault.Key
		read func(*Mailbox) error
	}{
		{
			name: "admission", key: mailboxAdmissionKey,
			read: func(mailbox *Mailbox) error {
				_, _, err := mailbox.readMessageAdmission(t.Context())

				return err
			},
		},
		{
			name: "scrub cursor", key: mailboxScrubCursorKey,
			read: func(mailbox *Mailbox) error {
				_, err := mailbox.scrubCursor(t.Context())

				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			mailbox, probe := openMailboxProbe(t)
			probe.buckets[mailboxCleanupBucket][string(test.key)] = []byte(
				strings.Repeat("x", maximumMailboxCleanupValueBytes+1),
			)
			probe.gets[mailboxCleanupBucket] = 0
			probe.valueSizes[mailboxCleanupBucket] = 0
			if err := test.read(mailbox); err != nil {
				t.Fatal(err)
			}
			if probe.gets[mailboxCleanupBucket] != 0 ||
				probe.valueSizes[mailboxCleanupBucket] == 0 {
				t.Fatalf(
					"cleanup read = gets %d sizes %d",
					probe.gets[mailboxCleanupBucket], probe.valueSizes[mailboxCleanupBucket],
				)
			}
		})
	}
}

func TestMailboxReceiveReconciliationAndCompletionFailures(t *testing.T) {
	failure := errors.New("mailbox receive storage failed")
	t.Run("reconciliation", func(t *testing.T) {
		mailbox, probe := openMailboxProbe(t)
		mailbox.retentionNeedsReconciliation = true
		probe.keyPageFailureOn = true
		probe.keyPageFailure = failure
		if err := mailbox.Receive(t.Context(), Message{}); !errors.Is(err, failure) {
			t.Fatalf("Receive error = %v", err)
		}
	})
	t.Run("completion", func(t *testing.T) {
		mailbox, probe := openMailboxProbe(t)
		probe.deleteFailures[mailboxCleanupBucket] = failure
		if err := mailbox.Receive(t.Context(), Message{
			FromHash: hashFor("sender"), Subject: "subject", Body: "body",
		}); !errors.Is(err, failure) || !mailbox.retentionNeedsReconciliation {
			t.Fatalf("Receive error = %v dirty=%t", err, mailbox.retentionNeedsReconciliation)
		}
	})
	t.Run("message put", func(t *testing.T) {
		mailbox, probe := openMailboxProbe(t)
		probe.putFailures[messagesBucket] = failure
		if err := mailbox.Receive(t.Context(), Message{
			FromHash: hashFor("sender"), Subject: "subject", Body: "body",
		}); !errors.Is(err, failure) {
			t.Fatalf("Receive error = %v", err)
		}
	})
}

func TestMailboxCleanupCursorFailurePaths(t *testing.T) {
	failure := errors.New("mailbox cursor storage failed")
	t.Run("duplicate bucket", func(t *testing.T) {
		probe := newMailboxStorageProbe()
		storage := newMailboxProbeVault(t, probe)
		if _, err := registerMailboxCleanup(storage); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenMailbox(storage, mailboxProbeClock); err == nil {
			t.Fatal("duplicate cleanup bucket was accepted")
		}
	})
	t.Run("read cancellation", func(t *testing.T) {
		mailbox, _ := openMailboxProbe(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := mailbox.scrubCursor(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("cursor error = %v", err)
		}
	})
	t.Run("discard corrupt failure", func(t *testing.T) {
		mailbox, probe := openMailboxProbe(t)
		probe.buckets[mailboxCleanupBucket][string(mailboxScrubCursorKey)] = []byte(
			strings.Repeat("x", maximumMailboxCleanupValueBytes+1),
		)
		probe.deleteFailures[mailboxCleanupBucket] = failure
		if _, err := mailbox.scrubCursor(t.Context()); !errors.Is(err, failure) {
			t.Fatalf("cursor error = %v", err)
		}
	})
	t.Run("store failure", func(t *testing.T) {
		mailbox, probe := openMailboxProbe(t)
		probe.putFailures[mailboxCleanupBucket] = failure
		if err := mailbox.storeScrubCursor(
			t.Context(),
			vault.Key("row"),
		); !errors.Is(
			err,
			failure,
		) {
			t.Fatalf("cursor error = %v", err)
		}
	})
	t.Run("clear failure", func(t *testing.T) {
		mailbox, probe := openMailboxProbe(t)
		probe.buckets[mailboxCleanupBucket][string(mailboxScrubCursorKey)] = []byte("row")
		probe.deleteFailures[mailboxCleanupBucket] = failure
		if err := mailbox.clearScrubCursor(t.Context()); !errors.Is(err, failure) {
			t.Fatalf("cursor error = %v", err)
		}
	})
	t.Run("open finalization", func(t *testing.T) {
		probe := newMailboxStorageProbe()
		if err := probe.Provision(mailboxCleanupBucket); err != nil {
			t.Fatal(err)
		}
		probe.buckets[mailboxCleanupBucket][string(mailboxScrubCursorKey)] = []byte("z")
		probe.deleteFailures[mailboxCleanupBucket] = failure
		if _, err := OpenMailbox(
			newMailboxProbeVault(t, probe),
			mailboxProbeClock,
		); !errors.Is(
			err,
			failure,
		) {
			t.Fatalf("OpenMailbox error = %v", err)
		}
	})
}

func TestMailboxScrubPrefixAndCheckpointFailures(t *testing.T) {
	failure := errors.New("mailbox scrub storage failed")
	tests := []struct {
		name     string
		exercise func(*testing.T, error)
	}{
		{name: "nil prefix", exercise: mailboxNilScrubPrefix},
		{name: "invalid prefix", exercise: mailboxInvalidScrubPrefix},
		{name: "prefix read failure", exercise: mailboxScrubPrefixReadFailure},
		{name: "prefix cancellation", exercise: mailboxScrubPrefixCancellation},
		{name: "multi-page prefix", exercise: mailboxMultiPageScrubPrefix},
		{name: "cursor read failure", exercise: mailboxScrubCursorReadFailure},
		{name: "prefix validation failure", exercise: mailboxScrubPrefixValidationFailure},
		{name: "reset stale", exercise: mailboxResetStaleScrubCursor},
		{name: "reset stale failure", exercise: mailboxResetStaleScrubCursorFailure},
		{name: "checkpoint failure", exercise: mailboxScrubCheckpointFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.exercise(t, failure)
		})
	}
}

func mailboxNilScrubPrefix(t *testing.T, _ error) {
	mailbox, _ := openMailboxProbe(t)
	valid, err := mailbox.scrubbedPrefixValid(t.Context(), nil)
	if err != nil || !valid {
		t.Fatalf("nil prefix = %t/%v", valid, err)
	}
}

func mailboxInvalidScrubPrefix(t *testing.T, _ error) {
	mailbox, probe := openMailboxProbe(t)
	probe.buckets[messagesBucket]["a"] = []byte("{")
	valid, err := mailbox.scrubbedPrefixValid(t.Context(), vault.Key("a"))
	if err != nil || valid {
		t.Fatalf("invalid prefix = %t/%v", valid, err)
	}
}

func mailboxScrubPrefixReadFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	probe.keyPageFailureOn = true
	probe.keyPageFailure = failure
	if _, err := mailbox.scrubbedPrefixValid(
		t.Context(), vault.Key("z"),
	); !errors.Is(err, failure) {
		t.Fatalf("prefix error = %v", err)
	}
}

func mailboxScrubPrefixCancellation(t *testing.T, _ error) {
	mailbox, _ := openMailboxProbe(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := mailbox.scrubbedPrefixValid(
		ctx,
		vault.Key("z"),
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("prefix error = %v", err)
	}
}

func mailboxMultiPageScrubPrefix(t *testing.T, _ error) {
	probe := newMailboxStorageProbe()
	messages := make([]Message, 0, mailboxScrubPage+1)
	for index := range mailboxScrubPage + 1 {
		messages = append(messages, Message{
			ReceivedAt: mailboxProbeTime.Add(time.Duration(index) * time.Second),
			FromHash:   hashFor("sender"), Subject: "subject", Body: "body",
		})
	}
	populateMailboxProbe(t, probe, messages)
	mailbox := &Mailbox{
		vault: newMailboxProbeVault(t, probe), now: mailboxProbeClock,
		retention: mailboxRetention{records: maximumMailboxRecords, bytes: maximumMailboxBytes},
	}
	var err error
	mailbox.messages, err = registerMessages(mailbox.vault)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := mailbox.scrubbedPrefixValid(t.Context(), vault.Key("z"))
	if err != nil || !valid || probe.keyPages[messagesBucket] < 2 {
		t.Fatalf(
			"multi-page prefix = %t/%v with %d pages",
			valid,
			err,
			probe.keyPages[messagesBucket],
		)
	}
}

func mailboxScrubCursorReadFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	probe.buckets[mailboxCleanupBucket][string(mailboxScrubCursorKey)] = []byte(
		strings.Repeat("x", maximumMailboxCleanupValueBytes+1),
	)
	probe.deleteFailures[mailboxCleanupBucket] = failure
	if err := mailbox.scrubMessages(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("scrub error = %v", err)
	}
}

func mailboxScrubPrefixValidationFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	probe.buckets[mailboxCleanupBucket][string(mailboxScrubCursorKey)] = []byte("z")
	probe.keyPageFailureOn = true
	probe.keyPageFailure = failure
	if err := mailbox.scrubMessages(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("scrub error = %v", err)
	}
}

func mailboxResetStaleScrubCursor(t *testing.T, _ error) {
	mailbox, probe := openMailboxProbe(t)
	probe.buckets[messagesBucket]["a"] = []byte("{")
	probe.buckets[mailboxCleanupBucket][string(mailboxScrubCursorKey)] = []byte("a")
	if err := mailbox.scrubMessages(t.Context()); err != nil ||
		len(probe.buckets[messagesBucket]) != 0 {
		t.Fatalf("scrub error = %v with %d rows", err, len(probe.buckets[messagesBucket]))
	}
}

func mailboxResetStaleScrubCursorFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	probe.buckets[messagesBucket]["a"] = []byte("{")
	probe.buckets[mailboxCleanupBucket][string(mailboxScrubCursorKey)] = []byte("a")
	probe.deleteFailures[mailboxCleanupBucket] = failure
	if err := mailbox.scrubMessages(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("scrub error = %v", err)
	}
}

func mailboxScrubCheckpointFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	probe.buckets[messagesBucket]["a"] = []byte("{")
	probe.putFailures[mailboxCleanupBucket] = failure
	if err := mailbox.scrubMessages(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("scrub error = %v", err)
	}
}

func mailboxProbeClock() time.Time {
	return mailboxProbeTime
}
