package peermessage

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestMailboxStartupOperationalReadsPreserveDurableState(t *testing.T) {
	failure := errors.New("mailbox startup read failed")
	for _, test := range []struct {
		name     string
		exercise func(*testing.T, error)
	}{
		{name: "admission size", exercise: mailboxAdmissionSizeFailurePreservesState},
		{name: "cursor value", exercise: mailboxCursorReadFailurePreservesState},
		{name: "retained size", exercise: mailboxRetainedSizeFailurePreservesState},
		{name: "retained value", exercise: mailboxRetainedReadFailurePreservesState},
		{name: "scrubbed prefix value", exercise: mailboxPrefixReadFailurePreservesState},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.exercise(t, failure)
		})
	}
}

func mailboxAdmissionSizeFailurePreservesState(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	storeMailboxPendingMessage(t, mailbox)
	mailboxSetValueSizeFailure(probe, mailboxCleanupBucket, mailboxAdmissionKey, failure)
	mailboxAssertFailedReopenPreservesState(t, mailbox, probe, failure)
}

func mailboxCursorReadFailurePreservesState(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	if err := mailbox.storeScrubCursor(t.Context(), vault.Key("retained")); err != nil {
		t.Fatal(err)
	}
	mailboxSetValueReadFailure(probe, mailboxCleanupBucket, mailboxScrubCursorKey, failure)
	mailboxAssertFailedReopenPreservesState(t, mailbox, probe, failure)
}

func mailboxRetainedSizeFailurePreservesState(t *testing.T, failure error) {
	probe := newMailboxStorageProbe()
	populateMailboxProbe(t, probe, mailboxProbeMessages()[:1])
	mailbox := mailboxOpenWithoutFailure(t, probe)
	probe.valueSizeFailure = failure
	mailboxAssertFailedReopenPreservesState(t, mailbox, probe, failure)
}

func mailboxRetainedReadFailurePreservesState(t *testing.T, failure error) {
	probe := newMailboxStorageProbe()
	messages := mailboxProbeMessages()[:1]
	populateMailboxProbe(t, probe, messages)
	mailbox := mailboxOpenWithoutFailure(t, probe)
	mailboxSetValueReadFailure(probe, messagesBucket, messageKey(messages[0]), failure)
	mailboxAssertFailedReopenPreservesState(t, mailbox, probe, failure)
}

func mailboxPrefixReadFailurePreservesState(t *testing.T, failure error) {
	probe := newMailboxStorageProbe()
	messages := mailboxProbeMessages()[:1]
	populateMailboxProbe(t, probe, messages)
	mailbox := mailboxOpenWithoutFailure(t, probe)
	key := messageKey(messages[0])
	if err := mailbox.storeScrubCursor(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	mailboxSetValueReadFailure(probe, messagesBucket, key, failure)
	mailboxAssertFailedReopenPreservesState(t, mailbox, probe, failure)
}

func mailboxOpenWithoutFailure(t *testing.T, probe *mailboxStorageProbe) *Mailbox {
	t.Helper()
	mailbox, err := OpenMailbox(newMailboxProbeVault(t, probe), mailboxProbeClock)
	if err != nil {
		t.Fatal(err)
	}

	return mailbox
}

func mailboxAssertFailedReopenPreservesState(
	t *testing.T,
	mailbox *Mailbox,
	probe *mailboxStorageProbe,
	failure error,
) {
	t.Helper()
	want := cloneMailboxProbeBuckets(probe.buckets)
	if err := mailbox.vault.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenMailbox(
		newMailboxProbeVault(t, probe),
		mailboxProbeClock,
	); !errors.Is(err, failure) {
		t.Fatalf("OpenMailbox error = %v, want %v", err, failure)
	}
	if !reflect.DeepEqual(probe.buckets, want) {
		t.Fatalf("failed reopen mutated durable state\ngot:  %#v\nwant: %#v", probe.buckets, want)
	}
}

func mailboxSetValueSizeFailure(
	probe *mailboxStorageProbe,
	bucket vault.Name,
	key vault.Key,
	failure error,
) {
	probe.valueSizeFailures[bucket] = map[string]error{string(key): failure}
}

func mailboxSetValueReadFailure(
	probe *mailboxStorageProbe,
	bucket vault.Name,
	key vault.Key,
	failure error,
) {
	probe.valueReadFailures[bucket] = map[string]error{string(key): failure}
}

func TestMailboxStartupRepairsValuesMissingAfterSizeInspection(t *testing.T) {
	for _, test := range []struct {
		name     string
		exercise func(*testing.T)
	}{
		{name: "admission", exercise: mailboxRepairMissingAdmissionValue},
		{name: "cursor", exercise: mailboxRepairMissingCursorValue},
		{name: "retained size", exercise: mailboxRepairMissingRetainedSize},
		{name: "retained row", exercise: mailboxRepairMissingRetainedValue},
		{name: "invalid admission content", exercise: mailboxRepairInvalidAdmissionContent},
		{name: "invalid retained content", exercise: mailboxRepairInvalidRetainedContent},
	} {
		t.Run(test.name, test.exercise)
	}
}

func mailboxRepairMissingAdmissionValue(t *testing.T) {
	mailbox, probe := openMailboxProbe(t)
	storeMailboxPendingMessage(t, mailbox)
	mailboxSetValueReadMissing(probe, mailboxCleanupBucket, mailboxAdmissionKey)
	message, found, err := mailbox.readMessageAdmission(t.Context())
	if err != nil || found || message != (Message{}) {
		t.Fatalf("repaired admission = %#v/%t/%v", message, found, err)
	}
	mailboxAssertValueAbsent(t, probe, mailboxCleanupBucket, mailboxAdmissionKey)
}

func mailboxRepairMissingCursorValue(t *testing.T) {
	mailbox, probe := openMailboxProbe(t)
	if err := mailbox.storeScrubCursor(t.Context(), vault.Key("retained")); err != nil {
		t.Fatal(err)
	}
	mailboxSetValueReadMissing(probe, mailboxCleanupBucket, mailboxScrubCursorKey)
	after, err := mailbox.scrubCursor(t.Context())
	if err != nil || after != nil {
		t.Fatalf("repaired cursor = %q/%v", after, err)
	}
	mailboxAssertValueAbsent(t, probe, mailboxCleanupBucket, mailboxScrubCursorKey)
}

func mailboxRepairMissingRetainedValue(t *testing.T) {
	probe := newMailboxStorageProbe()
	messages := mailboxProbeMessages()[:1]
	populateMailboxProbe(t, probe, messages)
	key := messageKey(messages[0])
	mailboxSetValueReadMissing(probe, messagesBucket, key)
	mailbox, err := OpenMailbox(newMailboxProbeVault(t, probe), mailboxProbeClock)
	if err != nil {
		t.Fatal(err)
	}
	if mailbox.retainedRecords != 0 {
		t.Fatalf("retained records = %d, want 0", mailbox.retainedRecords)
	}
	mailboxAssertValueAbsent(t, probe, messagesBucket, key)
}

func mailboxRepairMissingRetainedSize(t *testing.T) {
	probe := newMailboxStorageProbe()
	messages := mailboxProbeMessages()[:1]
	populateMailboxProbe(t, probe, messages)
	key := messageKey(messages[0])
	probe.valueSizeMissing[messagesBucket] = map[string]bool{string(key): true}
	mailbox, err := OpenMailbox(newMailboxProbeVault(t, probe), mailboxProbeClock)
	if err != nil {
		t.Fatal(err)
	}
	if mailbox.retainedRecords != 0 {
		t.Fatalf("retained records = %d, want 0", mailbox.retainedRecords)
	}
	mailboxAssertValueAbsent(t, probe, messagesBucket, key)
}

func mailboxRepairInvalidAdmissionContent(t *testing.T) {
	mailbox, probe := openMailboxProbe(t)
	invalid := Message{Subject: fmt.Sprintf("%0*d", acceptedSubjectSize+1, 0), Body: "body"}
	raw, err := (messageCodec{}).Encode(invalid)
	if err != nil {
		t.Fatal(err)
	}
	if err := mailbox.storeMessageAdmission(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	_, found, err := mailbox.readMessageAdmission(t.Context())
	if err != nil || found {
		t.Fatalf("invalid admission = %t/%v", found, err)
	}
	mailboxAssertValueAbsent(t, probe, mailboxCleanupBucket, mailboxAdmissionKey)
}

func mailboxRepairInvalidRetainedContent(t *testing.T) {
	probe := newMailboxStorageProbe()
	invalid := Message{
		ReceivedAt: mailboxProbeTime,
		Subject:    fmt.Sprintf("%0*d", acceptedSubjectSize+1, 0),
		Body:       "body",
	}
	populateMailboxProbe(t, probe, []Message{invalid})
	key := messageKey(invalid)
	mailbox, err := OpenMailbox(newMailboxProbeVault(t, probe), mailboxProbeClock)
	if err != nil {
		t.Fatal(err)
	}
	if mailbox.retainedRecords != 0 {
		t.Fatalf("retained records = %d, want 0", mailbox.retainedRecords)
	}
	mailboxAssertValueAbsent(t, probe, messagesBucket, key)
}

func mailboxSetValueReadMissing(
	probe *mailboxStorageProbe,
	bucket vault.Name,
	key vault.Key,
) {
	probe.valueReadMissing[bucket] = map[string]bool{string(key): true}
}

func mailboxAssertValueAbsent(
	t *testing.T,
	probe *mailboxStorageProbe,
	bucket vault.Name,
	key vault.Key,
) {
	t.Helper()
	if _, found := probe.buckets[bucket][string(key)]; found {
		t.Fatalf("durable value %s/%q was retained", bucket, key)
	}
}

func TestOpenMailboxReconcilesExactPartialAdmissionAcrossReopens(t *testing.T) {
	probe := newMailboxStorageProbe()
	retained := mailboxCapacityFixture()
	populateMailboxProbe(t, probe, retained)
	mailbox := mailboxOpenWithoutFailure(t, probe)
	accepted := retained[len(retained)-1]
	accepted.ReceivedAt = accepted.ReceivedAt.Add(time.Second)
	accepted.Subject = "accepted-after-partial-commit"
	recoveredMessages := append([]Message(nil), retained[1:]...)
	recoveredMessages = append(recoveredMessages, accepted)
	expectation := mailboxAdmissionRecoveryExpectation{
		oldest:        retained[0],
		accepted:      accepted,
		retainedBytes: mailboxEncodedFixtureBytes(t, recoveredMessages),
	}
	failure := errors.New("injected partial message commit")
	probe.partialMessageCommit = failure
	if err := mailbox.Receive(t.Context(), accepted); !errors.Is(err, failure) {
		t.Fatalf("Receive error = %v, want %v", err, failure)
	}
	mailboxAssertExactPartialAdmission(t, mailbox, probe, expectation)
	mailboxClose(t, mailbox)

	reopened := mailboxOpenWithoutFailure(t, probe)
	mailboxAssertRecoveredAdmission(t, reopened, probe, expectation)
	durableAfterFirstReopen := cloneMailboxProbeBuckets(probe.buckets)
	mailboxClose(t, reopened)

	reopenedAgain := mailboxOpenWithoutFailure(t, probe)
	mailboxAssertRecoveredAdmission(t, reopenedAgain, probe, expectation)
	if !reflect.DeepEqual(probe.buckets, durableAfterFirstReopen) {
		t.Fatalf(
			"second reopen changed durable state\ngot:  %#v\nwant: %#v",
			probe.buckets,
			durableAfterFirstReopen,
		)
	}
}

type mailboxAdmissionRecoveryExpectation struct {
	oldest        Message
	accepted      Message
	retainedBytes int
}

func mailboxCapacityFixture() []Message {
	messages := make([]Message, 0, maximumMailboxRecords)
	for index := range maximumMailboxRecords {
		messages = append(messages, Message{
			ReceivedAt: mailboxProbeTime.Add(time.Duration(index) * time.Second),
			FromHash:   hashFor("sender"),
			ToHash:     hashFor("self"),
			Subject:    fmt.Sprintf("retained-%04d", index),
			Body:       "body",
		})
	}

	return messages
}

func mailboxEncodedFixtureBytes(t *testing.T, messages []Message) int {
	t.Helper()
	total := 0
	for _, message := range messages {
		raw, err := (messageCodec{}).Encode(message)
		if err != nil {
			t.Fatal(err)
		}
		total += len(raw)
	}

	return total
}

func mailboxAssertExactPartialAdmission(
	t *testing.T,
	mailbox *Mailbox,
	probe *mailboxStorageProbe,
	expectation mailboxAdmissionRecoveryExpectation,
) {
	t.Helper()
	if len(probe.buckets[messagesBucket]) != maximumMailboxRecords+1 ||
		!mailbox.retentionNeedsReconciliation {
		t.Fatalf(
			"partial mailbox = %d rows, dirty=%t",
			len(probe.buckets[messagesBucket]),
			mailbox.retentionNeedsReconciliation,
		)
	}
	mailboxAssertMessagePresence(t, probe, expectation.oldest, true)
	mailboxAssertMessagePresence(t, probe, expectation.accepted, true)
	if len(probe.buckets[mailboxCleanupBucket][string(mailboxAdmissionKey)]) == 0 {
		t.Fatal("partial mailbox lost its admission intent")
	}
}

func mailboxAssertRecoveredAdmission(
	t *testing.T,
	mailbox *Mailbox,
	probe *mailboxStorageProbe,
	expectation mailboxAdmissionRecoveryExpectation,
) {
	t.Helper()
	if len(probe.buckets[messagesBucket]) != maximumMailboxRecords ||
		mailbox.retainedRecords != maximumMailboxRecords ||
		mailbox.retainedBytes != expectation.retainedBytes ||
		mailbox.retentionNeedsReconciliation {
		t.Fatalf(
			"recovered mailbox = %d rows, retained=%d records/%d bytes, want %d bytes, dirty=%t",
			len(probe.buckets[messagesBucket]),
			mailbox.retainedRecords,
			mailbox.retainedBytes,
			expectation.retainedBytes,
			mailbox.retentionNeedsReconciliation,
		)
	}
	mailboxAssertMessagePresence(t, probe, expectation.oldest, false)
	mailboxAssertMessagePresence(t, probe, expectation.accepted, true)
	mailboxAssertValueAbsent(t, probe, mailboxCleanupBucket, mailboxAdmissionKey)
}

func mailboxAssertMessagePresence(
	t *testing.T,
	probe *mailboxStorageProbe,
	message Message,
	want bool,
) {
	t.Helper()
	_, found := probe.buckets[messagesBucket][string(messageKey(message))]
	if found != want {
		t.Fatalf("message %q presence = %t, want %t", message.Subject, found, want)
	}
}

func mailboxClose(t *testing.T, mailbox *Mailbox) {
	t.Helper()
	if err := mailbox.vault.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMailboxCleanupReadMissingIsTypedCorruption(t *testing.T) {
	mailbox, probe := openMailboxProbe(t)
	key := vault.Key("missing-after-size")
	probe.buckets[mailboxCleanupBucket][string(key)] = []byte("value")
	mailboxSetValueReadMissing(probe, mailboxCleanupBucket, key)
	err := mailbox.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, err := mailbox.storedMailboxCleanup(tx, key)

		return err
	})
	if !errors.Is(err, vault.ErrCorruptValue) {
		t.Fatalf("cleanup read error = %v", err)
	}
}
