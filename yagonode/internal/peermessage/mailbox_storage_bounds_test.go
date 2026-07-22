package peermessage

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var mailboxProbeTime = time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

type mailboxStorageProbe struct {
	buckets              map[vault.Name]map[string][]byte
	gets                 map[vault.Name]int
	contains             map[vault.Name]int
	scans                map[vault.Name]int
	valueSizes           map[vault.Name]int
	keyPages             map[vault.Name]int
	valuePages           map[vault.Name]int
	valuePageBytes       map[vault.Name]int
	keyPageFailureAfter  int
	keyPageFailure       error
	keyPageFailureOn     bool
	keyPageEmpty         bool
	valuePageFailure     error
	valuePageEmpty       bool
	valueSizeFailure     error
	valueSizeFailures    map[vault.Name]map[string]error
	valueSizeMissing     map[vault.Name]map[string]bool
	valueReadFailures    map[vault.Name]map[string]error
	valueReadMissing     map[vault.Name]map[string]bool
	deleteFailure        error
	putFailures          map[vault.Name]error
	deleteFailures       map[vault.Name]error
	afterMessagePut      func()
	beforeUpdate         func()
	partialMessageCommit error
	updates              int
}

func newMailboxStorageProbe() *mailboxStorageProbe {
	return &mailboxStorageProbe{
		buckets:           make(map[vault.Name]map[string][]byte),
		gets:              make(map[vault.Name]int),
		contains:          make(map[vault.Name]int),
		scans:             make(map[vault.Name]int),
		valueSizes:        make(map[vault.Name]int),
		keyPages:          make(map[vault.Name]int),
		valuePages:        make(map[vault.Name]int),
		valuePageBytes:    make(map[vault.Name]int),
		putFailures:       make(map[vault.Name]error),
		deleteFailures:    make(map[vault.Name]error),
		valueSizeFailures: make(map[vault.Name]map[string]error),
		valueSizeMissing:  make(map[vault.Name]map[string]bool),
		valueReadFailures: make(map[vault.Name]map[string]error),
		valueReadMissing:  make(map[vault.Name]map[string]bool),
	}
}

func (p *mailboxStorageProbe) Provision(name vault.Name) error {
	if p.buckets[name] == nil {
		p.buckets[name] = make(map[string][]byte)
	}

	return nil
}

func (p *mailboxStorageProbe) Update(
	ctx context.Context,
	fn func(vault.EngineTxn) error,
) error {
	p.updates++
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("update mailbox probe: %w", err)
	}
	if p.beforeUpdate != nil {
		p.beforeUpdate()
	}
	snapshot := cloneMailboxProbeBuckets(p.buckets)
	if err := fn(mailboxProbeTxn{probe: p, writable: true}); err != nil {
		p.buckets = snapshot

		return err
	}
	if p.partialMessageCommit != nil && mailboxProbeBucketChanged(
		snapshot[messagesBucket], p.buckets[messagesBucket],
	) {
		failure := p.partialMessageCommit
		p.partialMessageCommit = nil
		committed := cloneMailboxProbeBuckets(p.buckets)
		p.buckets = snapshot
		for key, value := range committed[messagesBucket] {
			p.buckets[messagesBucket][key] = append([]byte(nil), value...)
		}
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(p.buckets[messagesBucket])))
		p.buckets[vault.Name("__lengths__")][string(messagesBucket)] = length[:]

		return failure
	}

	return nil
}

func mailboxProbeBucketChanged(before, after map[string][]byte) bool {
	if len(before) != len(after) {
		return true
	}
	for key, value := range before {
		if !bytes.Equal(value, after[key]) {
			return true
		}
	}

	return false
}

func TestMailboxRetryReconcilesPartialLimitOneWrite(t *testing.T) {
	mailbox, probe := openMailboxProbe(t)
	mailbox.retention = mailboxRetention{records: 1, bytes: maximumMailboxBytes}
	first := Message{
		ReceivedAt: mailboxProbeTime,
		FromHash:   hashFor("sender"), ToHash: hashFor("self"), Subject: "first", Body: "body",
	}
	second := first
	second.ReceivedAt = first.ReceivedAt.Add(time.Second)
	second.Subject = "second"
	if err := mailbox.Receive(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("later shard commit failed")
	probe.partialMessageCommit = failure
	if err := mailbox.Receive(t.Context(), second); !errors.Is(err, failure) {
		t.Fatalf("partial write error = %v, want %v", err, failure)
	}
	if rows := len(probe.buckets[messagesBucket]); rows != 2 {
		t.Fatalf("partial rows = %d, want 2", rows)
	}
	if err := mailbox.Receive(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	retained, err := mailbox.Messages(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(retained) != 1 || retained[0].Subject != "second" ||
		mailbox.retainedRecords != 1 || mailbox.retentionNeedsReconciliation {
		t.Fatalf(
			"reconciled mailbox = %#v records=%d dirty=%t",
			retained,
			mailbox.retainedRecords,
			mailbox.retentionNeedsReconciliation,
		)
	}
}

func TestMailboxScrubResumesFromDurableCursor(t *testing.T) {
	probe := newMailboxStorageProbe()
	storage := newMailboxProbeVault(t, probe)
	if _, err := registerMessages(storage); err != nil {
		t.Fatal(err)
	}
	for index := range mailboxScrubPage + 32 {
		probe.buckets[messagesBucket][fmt.Sprintf("%08d", index)] = []byte("{")
	}
	probe.keyPageFailureOn = true
	probe.keyPageFailureAfter = 1
	probe.keyPageFailure = errors.New("cleanup interrupted")
	if _, err := OpenMailbox(newMailboxProbeVault(t, probe), time.Now); err == nil {
		t.Fatal("interrupted cleanup succeeded")
	}
	cursor := probe.buckets[mailboxCleanupBucket][string(mailboxScrubCursorKey)]
	if len(cursor) == 0 || len(probe.buckets[messagesBucket]) != 32 {
		t.Fatalf(
			"checkpoint = %q with %d rows, want durable cursor and 32 rows",
			cursor,
			len(probe.buckets[messagesBucket]),
		)
	}
	probe.keyPageFailureOn = false
	probe.keyPageFailureAfter = 0
	probe.keyPageFailure = nil
	probe.keyPages[messagesBucket] = 0
	mailbox, err := OpenMailbox(newMailboxProbeVault(t, probe), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.buckets[messagesBucket]) != 0 || mailbox.retainedRecords != 0 ||
		len(probe.buckets[mailboxCleanupBucket]) != 0 {
		t.Fatalf(
			"resumed cleanup = rows %d retained %d cursors %d",
			len(probe.buckets[messagesBucket]),
			mailbox.retainedRecords,
			len(probe.buckets[mailboxCleanupBucket]),
		)
	}
}

func (p *mailboxStorageProbe) View(
	ctx context.Context,
	fn func(vault.EngineTxn) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("view mailbox probe: %w", err)
	}

	return fn(mailboxProbeTxn{probe: p})
}

func (p *mailboxStorageProbe) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (p *mailboxStorageProbe) QuotaBytes() int64                        { return 0 }
func (p *mailboxStorageProbe) Close() error                             { return nil }

func cloneMailboxProbeBuckets(
	source map[vault.Name]map[string][]byte,
) map[vault.Name]map[string][]byte {
	cloned := make(map[vault.Name]map[string][]byte, len(source))
	for name, bucket := range source {
		cloned[name] = make(map[string][]byte, len(bucket))
		for key, value := range bucket {
			cloned[name][key] = append([]byte(nil), value...)
		}
	}

	return cloned
}

type mailboxProbeTxn struct {
	probe    *mailboxStorageProbe
	writable bool
}

func (t mailboxProbeTxn) Bucket(name vault.Name) vault.EngineBucket {
	return mailboxProbeBucket{probe: t.probe, name: name}
}

func (t mailboxProbeTxn) Writable() bool { return t.writable }

type mailboxProbeBucket struct {
	probe *mailboxStorageProbe
	name  vault.Name
}

func (b mailboxProbeBucket) Get(key vault.Key) []byte {
	value, _, _ := b.ReadValue(key)

	return value
}

func (b mailboxProbeBucket) ReadValue(key vault.Key) ([]byte, bool, error) {
	b.probe.gets[b.name]++
	value, found := b.probe.buckets[b.name][string(key)]
	if failure := b.probe.valueReadFailures[b.name][string(key)]; failure != nil {
		return nil, found, failure
	}
	if b.probe.valueReadMissing[b.name][string(key)] {
		return nil, false, nil
	}
	if !found {
		return nil, false, nil
	}

	return append([]byte(nil), value...), true, nil
}

func (b mailboxProbeBucket) Contains(key vault.Key) bool {
	b.probe.contains[b.name]++
	_, found := b.probe.buckets[b.name][string(key)]

	return found
}

func (b mailboxProbeBucket) Put(key vault.Key, value []byte) error {
	if err := b.probe.putFailures[b.name]; err != nil {
		return err
	}
	b.probe.buckets[b.name][string(key)] = append([]byte(nil), value...)
	if b.name == messagesBucket && b.probe.afterMessagePut != nil {
		b.probe.afterMessagePut()
	}

	return nil
}

func (b mailboxProbeBucket) Delete(key vault.Key) error {
	if err := b.probe.deleteFailures[b.name]; err != nil {
		return err
	}
	if b.name == messagesBucket && b.probe.deleteFailure != nil {
		return b.probe.deleteFailure
	}
	delete(b.probe.buckets[b.name], string(key))

	return nil
}

func (b mailboxProbeBucket) Scan(
	prefix vault.Key,
	fn func(vault.Key, []byte) (bool, error),
) error {
	b.probe.scans[b.name]++
	for _, key := range mailboxProbeKeys(b.probe.buckets[b.name]) {
		if !bytes.HasPrefix([]byte(key), prefix) {
			continue
		}
		more, err := fn(
			vault.Key(key),
			append([]byte(nil), b.probe.buckets[b.name][key]...),
		)
		if err != nil || !more {
			return err
		}
	}

	return nil
}

func (b mailboxProbeBucket) ReadKeyPageAfter(
	after vault.Key,
	limit int,
) (vault.BucketKeyPage, error) {
	if b.name == messagesBucket && b.probe.keyPageFailureOn &&
		b.probe.keyPages[b.name] >= b.probe.keyPageFailureAfter {
		return vault.BucketKeyPage{}, b.probe.keyPageFailure
	}
	b.probe.keyPages[b.name]++
	if b.name == messagesBucket && b.probe.keyPageEmpty {
		return vault.BucketKeyPage{}, nil
	}
	keys := mailboxProbeKeys(b.probe.buckets[b.name])
	start := mailboxProbePageStart(keys, after)
	end := min(start+limit, len(keys))
	page := make([]vault.Key, 0, end-start)
	for _, key := range keys[start:end] {
		page = append(page, vault.Key(key))
	}

	return vault.BucketKeyPage{Keys: page, More: end < len(keys)}, nil
}

func (b mailboxProbeBucket) ReadPageAfter(
	after vault.Key,
	limit int,
) (vault.BucketPage, error) {
	b.probe.valuePages[b.name]++
	if b.name == messagesBucket && b.probe.valuePageFailure != nil {
		return vault.BucketPage{}, b.probe.valuePageFailure
	}
	if b.name == messagesBucket && b.probe.valuePageEmpty {
		return vault.BucketPage{}, nil
	}
	keys := mailboxProbeKeys(b.probe.buckets[b.name])
	start := mailboxProbePageStart(keys, after)
	end := min(start+limit, len(keys))
	entries := make([]vault.BucketPageEntry, 0, end-start)
	for _, key := range keys[start:end] {
		value := append([]byte(nil), b.probe.buckets[b.name][key]...)
		b.probe.valuePageBytes[b.name] += len(value)
		entries = append(entries, vault.BucketPageEntry{Key: vault.Key(key), Value: value})
	}

	return vault.BucketPage{Entries: entries, More: end < len(keys)}, nil
}

func (b mailboxProbeBucket) ValueSize(key vault.Key) (int, bool, error) {
	b.probe.valueSizes[b.name]++
	if failure := b.probe.valueSizeFailures[b.name][string(key)]; failure != nil {
		_, found := b.probe.buckets[b.name][string(key)]

		return 0, found, failure
	}
	if b.probe.valueSizeMissing[b.name][string(key)] {
		return 0, false, nil
	}
	if b.name == messagesBucket && b.probe.valueSizeFailure != nil {
		return 0, false, b.probe.valueSizeFailure
	}
	value, found := b.probe.buckets[b.name][string(key)]

	return len(value), found, nil
}

func mailboxProbeKeys(bucket map[string][]byte) []string {
	keys := make([]string, 0, len(bucket))
	for key := range bucket {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	return keys
}

func mailboxProbePageStart(keys []string, after vault.Key) int {
	if after == nil {
		return 0
	}
	start, found := slices.BinarySearch(keys, string(after))
	if found {
		start++
	}

	return start
}

func newMailboxProbeVault(t *testing.T, probe *mailboxStorageProbe) *vault.Vault {
	t.Helper()
	storage, err := vault.New(probe)
	if err != nil {
		t.Fatal(err)
	}

	return storage
}

func populateMailboxProbe(
	t *testing.T,
	probe *mailboxStorageProbe,
	messages []Message,
) {
	t.Helper()
	storage := newMailboxProbeVault(t, probe)
	collection, err := registerMessages(storage)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		for _, message := range messages {
			if err := collection.Put(tx, messageKey(message), message); err != nil {
				return fmt.Errorf("populate mailbox probe: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func openMailboxProbe(t *testing.T) (*Mailbox, *mailboxStorageProbe) {
	t.Helper()
	probe := newMailboxStorageProbe()
	mailbox, err := OpenMailbox(newMailboxProbeVault(t, probe), func() time.Time {
		return mailboxProbeTime
	})
	if err != nil {
		t.Fatal(err)
	}

	return mailbox, probe
}

func mailboxProbeMessages() []Message {
	messages := make([]Message, 0, 3)
	for index := range 3 {
		messages = append(messages, Message{
			ReceivedAt: mailboxProbeTime.Add(time.Duration(index) * time.Second),
			FromHash:   hashFor("sender"),
			ToHash:     hashFor("self"),
			Subject:    fmt.Sprintf("retained-%d", index),
			Body:       "body",
		})
	}

	return messages
}

func TestMailboxStartupEvictsOversizedValueWithoutReadingIt(t *testing.T) {
	probe := newMailboxStorageProbe()
	message := Message{
		ReceivedAt: mailboxProbeTime,
		FromHash:   hashFor("sender"),
		ToHash:     hashFor("self"),
		Subject:    "legacy",
		Body:       strings.Repeat("x", maximumStoredMessageBytes+1),
	}
	populateMailboxProbe(t, probe, []Message{message})
	probe.gets[messagesBucket] = 0
	probe.valuePageBytes[messagesBucket] = 0

	mailbox, err := OpenMailbox(newMailboxProbeVault(t, probe), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.buckets[messagesBucket]) != 0 || mailbox.retainedRecords != 0 ||
		probe.gets[messagesBucket] != 0 || probe.valuePageBytes[messagesBucket] != 0 {
		t.Fatalf(
			"oversized cleanup = rows %d, retained %d, gets %d, paged bytes %d",
			len(probe.buckets[messagesBucket]),
			mailbox.retainedRecords,
			probe.gets[messagesBucket],
			probe.valuePageBytes[messagesBucket],
		)
	}
}

func TestMailboxStartupCleanupCommitsProgressBeforeLaterReadFailure(t *testing.T) {
	probe := newMailboxStorageProbe()
	base := mailboxProbeTime
	messages := make([]Message, 0, maximumMailboxRecords+276)
	for index := range maximumMailboxRecords + 276 {
		messages = append(messages, Message{
			ReceivedAt: base.Add(time.Duration(index) * time.Second),
			FromHash:   hashFor("sender"),
			ToHash:     hashFor("self"),
			Subject:    fmt.Sprintf("legacy-%04d", index),
			Body:       "body",
		})
	}
	populateMailboxProbe(t, probe, messages)
	probe.keyPageFailureOn = true
	probe.keyPageFailureAfter = 1
	probe.keyPageFailure = errors.New("interrupted cleanup")

	if _, err := OpenMailbox(newMailboxProbeVault(t, probe), time.Now); err == nil {
		t.Fatal("interrupted startup cleanup succeeded")
	}
	if got := len(probe.buckets[messagesBucket]); got != maximumMailboxRecords+148 {
		t.Fatalf("rows after interrupted cleanup = %d, want %d", got, maximumMailboxRecords+148)
	}
	probe.keyPageFailureOn = false
	probe.keyPageFailureAfter = 0
	probe.keyPageFailure = nil
	probe.keyPages[messagesBucket] = 0
	mailbox, err := OpenMailbox(newMailboxProbeVault(t, probe), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(probe.buckets[messagesBucket]); got != maximumMailboxRecords ||
		mailbox.retainedRecords != maximumMailboxRecords {
		t.Fatalf("resumed cleanup = rows %d, retained %d", got, mailbox.retainedRecords)
	}
}

func TestMailboxReceiveKeepsHotPathFreeOfBucketScans(t *testing.T) {
	probe := newMailboxStorageProbe()
	mailbox, err := OpenMailbox(newMailboxProbeVault(t, probe), func() time.Time {
		return mailboxProbeTime
	})
	if err != nil {
		t.Fatal(err)
	}
	probe.gets[messagesBucket] = 0
	probe.contains[messagesBucket] = 0
	probe.scans[messagesBucket] = 0
	probe.valueSizes[messagesBucket] = 0
	probe.keyPages[messagesBucket] = 0
	probe.valuePages[messagesBucket] = 0
	const writes = 256
	for index := range writes {
		message := Message{
			ReceivedAt: mailboxProbeTime.Add(time.Duration(index) * time.Second),
			FromHash:   hashFor("sender"),
			ToHash:     hashFor("self"),
			Subject:    fmt.Sprintf("message-%03d", index),
			Body:       "body",
		}
		if err := mailbox.Receive(t.Context(), message); err != nil {
			t.Fatal(err)
		}
	}
	if probe.gets[messagesBucket] != 0 || probe.scans[messagesBucket] != 0 ||
		probe.keyPages[messagesBucket] != 0 || probe.valuePages[messagesBucket] != 0 ||
		probe.valueSizes[messagesBucket] != writes || probe.contains[messagesBucket] != writes {
		t.Fatalf(
			"hot path = gets %d, scans %d, key pages %d, value pages %d, sizes %d, presence %d",
			probe.gets[messagesBucket],
			probe.scans[messagesBucket],
			probe.keyPages[messagesBucket],
			probe.valuePages[messagesBucket],
			probe.valueSizes[messagesBucket],
			probe.contains[messagesBucket],
		)
	}
}

func TestMailboxReceiveMaintainsOverwriteAccounting(t *testing.T) {
	mailbox, probe := openMailboxProbe(t)
	message := Message{
		ReceivedAt: mailboxProbeTime,
		FromHash:   hashFor("sender"),
		ToHash:     hashFor("self"),
		Subject:    "same",
		Body:       "body",
	}
	for range 2 {
		if err := mailbox.Receive(t.Context(), message); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := (messageCodec{}).Encode(message)
	if err != nil {
		t.Fatal(err)
	}
	if mailbox.retainedRecords != 1 || mailbox.retainedBytes != len(raw) ||
		len(probe.buckets[messagesBucket]) != 1 {
		t.Fatalf(
			"overwrite accounting = %d records, %d bytes, %d rows",
			mailbox.retainedRecords,
			mailbox.retainedBytes,
			len(probe.buckets[messagesBucket]),
		)
	}
}

func TestMailboxRejectsInvalidStoredMessages(t *testing.T) {
	mailbox, _ := openMailboxProbe(t)
	if err := mailbox.Receive(t.Context(), Message{
		FromHash: hashFor("sender"),
		Subject:  strings.Repeat("s", acceptedSubjectSize+1),
		Body:     "body",
	}); err == nil {
		t.Fatal("oversized subject accepted")
	}
	if storedMessageAdmitted(make([]byte, maximumStoredMessageBytes+1)) {
		t.Fatal("oversized stored representation accepted")
	}
	if storedMessageAdmitted([]byte("{")) {
		t.Fatal("malformed stored representation accepted")
	}
}

type mailboxReceiveRetentionFailure struct {
	name      string
	configure func(*Mailbox, *mailboxStorageProbe, context.CancelFunc)
	want      error
}

func TestMailboxReceivePropagatesRetentionStorageFailures(t *testing.T) {
	failure := errors.New("retention storage failed")
	tests := []mailboxReceiveRetentionFailure{
		{
			name: "measure replacement",
			configure: func(_ *Mailbox, probe *mailboxStorageProbe, _ context.CancelFunc) {
				probe.valueSizeFailure = failure
			},
			want: failure,
		},
		{
			name: "cancel eviction",
			configure: func(mailbox *Mailbox, probe *mailboxStorageProbe, cancel context.CancelFunc) {
				mailbox.retention.records = 0
				probe.afterMessagePut = cancel
			},
			want: context.Canceled,
		},
		{
			name: "read eviction page",
			configure: func(mailbox *Mailbox, probe *mailboxStorageProbe, _ context.CancelFunc) {
				mailbox.retention.records = 0
				probe.keyPageFailureOn = true
				probe.keyPageFailureAfter = probe.keyPages[messagesBucket]
				probe.keyPageFailure = failure
			},
			want: failure,
		},
		{
			name: "empty eviction page",
			configure: func(mailbox *Mailbox, probe *mailboxStorageProbe, _ context.CancelFunc) {
				mailbox.retention.records = 0
				probe.keyPageEmpty = true
			},
		},
		{
			name: "measure eviction row",
			configure: func(mailbox *Mailbox, probe *mailboxStorageProbe, _ context.CancelFunc) {
				mailbox.retention.records = 0
				probe.afterMessagePut = func() {
					probe.valueSizeFailure = failure
				}
			},
			want: failure,
		},
		{
			name: "delete eviction row",
			configure: func(mailbox *Mailbox, probe *mailboxStorageProbe, _ context.CancelFunc) {
				mailbox.retention.records = 0
				probe.deleteFailure = failure
			},
			want: failure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMailboxReceiveRetentionFailure(t, test)
		})
	}
}

func assertMailboxReceiveRetentionFailure(
	t *testing.T,
	test mailboxReceiveRetentionFailure,
) {
	t.Helper()
	mailbox, probe := openMailboxProbe(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	test.configure(mailbox, probe, cancel)
	err := mailbox.Receive(ctx, Message{
		FromHash: hashFor("sender"), Subject: "subject", Body: "body",
	})
	if err == nil {
		t.Fatal("receive succeeded")
	}
	if test.want != nil && !errors.Is(err, test.want) {
		t.Fatalf("receive error = %v, want %v", err, test.want)
	}
	if mailbox.retainedRecords != 0 || mailbox.retainedBytes != 0 ||
		len(probe.buckets[messagesBucket]) != 0 {
		t.Fatalf(
			"failed receive retained %d records, %d bytes, %d rows",
			mailbox.retainedRecords,
			mailbox.retainedBytes,
			len(probe.buckets[messagesBucket]),
		)
	}
}

func TestMailboxStartupHelpersPropagateStorageFailures(t *testing.T) {
	failure := errors.New("startup storage failed")
	tests := []struct {
		name     string
		exercise func(*testing.T, error)
	}{
		{name: "corrupt length", exercise: mailboxCorruptLengthFailure},
		{name: "empty trim page", exercise: mailboxEmptyTrimPageFailure},
		{name: "trim delete", exercise: mailboxTrimDeleteFailure},
		{name: "scrub cancellation", exercise: mailboxScrubCancellation},
		{name: "trim cancellation", exercise: mailboxTrimCancellation},
		{name: "scrub read", exercise: mailboxScrubReadFailure},
		{name: "scrub delete", exercise: mailboxScrubDeleteFailure},
		{name: "retention read", exercise: mailboxRetentionReadFailure},
		{name: "retention size", exercise: mailboxRetentionSizeFailure},
		{name: "retention delete page", exercise: mailboxRetentionDeletePageFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.exercise(t, failure)
		})
	}
}

func mailboxCorruptLengthFailure(t *testing.T, _ error) {
	mailbox, probe := openMailboxProbe(t)
	probe.buckets[vault.Name("__lengths__")][string(messagesBucket)] = []byte("bad")
	if err := mailbox.trimMessageRecords(t.Context()); err == nil {
		t.Fatal("corrupt length accepted")
	}
}

func mailboxEmptyTrimPageFailure(t *testing.T, _ error) {
	mailbox, probe := openMailboxProbe(t)
	mailbox.retention.records = -1
	probe.keyPageEmpty = true
	if err := mailbox.trimMessageRecords(t.Context()); err == nil {
		t.Fatal("empty trim page accepted")
	}
}

func mailboxTrimDeleteFailure(t *testing.T, failure error) {
	probe := newMailboxStorageProbe()
	populateMailboxProbe(t, probe, mailboxProbeMessages()[:1])
	mailbox, err := OpenMailbox(newMailboxProbeVault(t, probe), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	mailbox.retention.records = 0
	probe.deleteFailure = failure
	if err := mailbox.trimMessageRecords(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("trim error = %v, want %v", err, failure)
	}
}

func mailboxScrubCancellation(t *testing.T, _ error) {
	mailbox, probe := openMailboxProbe(t)
	ctx, cancel := context.WithCancel(t.Context())
	probe.beforeUpdate = cancel
	if err := mailbox.scrubMessages(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("scrub error = %v, want cancellation", err)
	}
}

func mailboxTrimCancellation(t *testing.T, _ error) {
	mailbox, probe := openMailboxProbe(t)
	ctx, cancel := context.WithCancel(t.Context())
	probe.beforeUpdate = cancel
	if err := mailbox.trimMessageRecords(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("trim error = %v, want cancellation", err)
	}
}

func mailboxScrubReadFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	probe.keyPageFailureOn = true
	probe.keyPageFailure = failure
	if err := mailbox.prune(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("prune error = %v, want %v", err, failure)
	}
}

func mailboxScrubDeleteFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	probe.buckets[messagesBucket]["invalid"] = []byte("{")
	probe.deleteFailure = failure
	if err := mailbox.scrubMessages(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("scrub error = %v, want %v", err, failure)
	}
}

func mailboxRetentionReadFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	probe.keyPageFailureOn = true
	probe.keyPageFailureAfter = probe.keyPages[messagesBucket]
	probe.keyPageFailure = failure
	err := mailbox.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, err := mailbox.retainedMessages(t.Context(), tx)

		return err
	})
	if !errors.Is(err, failure) {
		t.Fatalf("retention error = %v, want %v", err, failure)
	}
}

func mailboxRetentionSizeFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	probe.buckets[messagesBucket]["row"] = []byte("stored")
	probe.valueSizeFailure = failure
	err := mailbox.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, err := mailbox.retainedMessages(t.Context(), tx)

		return err
	})
	if !errors.Is(err, failure) {
		t.Fatalf("retention error = %v, want %v", err, failure)
	}
}

func mailboxRetentionDeletePageFailure(t *testing.T, failure error) {
	mailbox, probe := openMailboxProbe(t)
	probe.keyPageFailureOn = true
	probe.keyPageFailureAfter = probe.keyPages[messagesBucket]
	probe.keyPageFailure = failure
	err := mailbox.vault.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := mailbox.deleteMessageBatchBefore(tx, vault.Key("z"))

		return err
	})
	if !errors.Is(err, failure) {
		t.Fatalf("retention delete error = %v, want %v", err, failure)
	}
}

func TestMailboxRetentionHelpersMeasureOversizedRowsWithoutValueReads(t *testing.T) {
	payloads := map[string][]byte{
		"compressible": []byte(strings.Repeat("x", maximumStoredMessageBytes+1)),
		"raw":          []byte(strings.Repeat("0123456789abcdef", maximumStoredMessageBytes/16+1)),
	}
	for name, payload := range payloads {
		t.Run(name+" retained tail", func(t *testing.T) {
			mailbox, probe := openMailboxProbe(t)
			probe.buckets[messagesBucket]["oversized"] = payload
			resetMailboxValueReadProbe(probe)
			if err := mailbox.vault.View(t.Context(), func(tx *vault.Txn) error {
				_, err := mailbox.retainedMessages(t.Context(), tx)

				return err
			}); err != nil {
				t.Fatal(err)
			}
			assertMailboxSizeFirstRead(t, probe)
		})
		t.Run(name+" hot eviction", func(t *testing.T) {
			mailbox, probe := openMailboxProbe(t)
			mailbox.retention = mailboxRetention{records: 0, bytes: maximumMailboxBytes}
			probe.buckets[messagesBucket]["oversized"] = payload
			resetMailboxValueReadProbe(probe)
			retained := retainedMailboxState{records: 1, bytes: len(payload)}
			if err := mailbox.vault.Update(t.Context(), func(tx *vault.Txn) error {
				return mailbox.evictMessagesAboveRetention(t.Context(), tx, &retained)
			}); err != nil {
				t.Fatal(err)
			}
			assertMailboxSizeFirstRead(t, probe)
			if retained.records != 0 || retained.bytes != 0 ||
				len(probe.buckets[messagesBucket]) != 0 {
				t.Fatalf(
					"retained state = %#v with %d rows",
					retained,
					len(probe.buckets[messagesBucket]),
				)
			}
		})
	}
}

func resetMailboxValueReadProbe(probe *mailboxStorageProbe) {
	probe.gets[messagesBucket] = 0
	probe.valueSizes[messagesBucket] = 0
	probe.keyPages[messagesBucket] = 0
	probe.valuePages[messagesBucket] = 0
	probe.valuePageBytes[messagesBucket] = 0
}

func assertMailboxSizeFirstRead(t *testing.T, probe *mailboxStorageProbe) {
	t.Helper()
	if probe.gets[messagesBucket] != 0 || probe.valuePages[messagesBucket] != 0 ||
		probe.valuePageBytes[messagesBucket] != 0 || probe.valueSizes[messagesBucket] == 0 ||
		probe.keyPages[messagesBucket] == 0 {
		t.Fatalf(
			"size-first read = gets %d value pages %d bytes %d sizes %d key pages %d",
			probe.gets[messagesBucket], probe.valuePages[messagesBucket],
			probe.valuePageBytes[messagesBucket], probe.valueSizes[messagesBucket],
			probe.keyPages[messagesBucket],
		)
	}
}

func TestMailboxRetentionInspectionHonorsCancellation(t *testing.T) {
	mailbox, _ := openMailboxProbe(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := mailbox.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, err := mailbox.retainedMessages(ctx, tx)
		if err != nil {
			return fmt.Errorf("inspect retained messages: %w", err)
		}

		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retention error = %v, want cancellation", err)
	}
}

func TestMailboxStartupByteRetentionEvictsOldestRows(t *testing.T) {
	probe := newMailboxStorageProbe()
	messages := mailboxProbeMessages()
	populateMailboxProbe(t, probe, messages)
	mailbox, err := OpenMailbox(newMailboxProbeVault(t, probe), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := (messageCodec{}).Encode(messages[len(messages)-1])
	if err != nil {
		t.Fatal(err)
	}
	mailbox.retention.bytes = len(raw)
	if err := mailbox.prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	retained, err := mailbox.Messages(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(retained) != 1 || retained[0].Subject != messages[len(messages)-1].Subject {
		t.Fatalf("retained messages = %#v", retained)
	}
}

func TestMailboxStartupByteRetentionPropagatesEvictionFailures(t *testing.T) {
	failure := errors.New("eviction failed")
	tests := []struct {
		name      string
		configure func(*mailboxStorageProbe)
	}{
		{
			name: "read",
			configure: func(probe *mailboxStorageProbe) {
				probe.keyPageFailureOn = true
				probe.keyPageFailureAfter = 1
				probe.keyPageFailure = failure
			},
		},
		{
			name: "delete",
			configure: func(probe *mailboxStorageProbe) {
				probe.deleteFailure = failure
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			probe := newMailboxStorageProbe()
			messages := mailboxProbeMessages()
			populateMailboxProbe(t, probe, messages)
			mailbox, err := OpenMailbox(newMailboxProbeVault(t, probe), time.Now)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := (messageCodec{}).Encode(messages[len(messages)-1])
			if err != nil {
				t.Fatal(err)
			}
			mailbox.retention.bytes = len(raw)
			probe.keyPages[messagesBucket] = 0
			test.configure(probe)
			if err := mailbox.prune(t.Context()); !errors.Is(err, failure) {
				t.Fatalf("prune error = %v, want %v", err, failure)
			}
		})
	}
}

func TestRetainedMessageTailHonorsDisabledAndByteLimits(t *testing.T) {
	for _, retention := range []mailboxRetention{
		{records: 0, bytes: 1},
		{records: 1, bytes: 0},
	} {
		tail := newRetainedMessageTail(retention)
		tail.Add(retainedMessage{key: vault.Key("ignored"), bytes: 1})
		if tail.length != 0 {
			t.Fatalf("disabled tail retained %d records", tail.length)
		}
	}
	tail := newRetainedMessageTail(mailboxRetention{records: 2, bytes: 5})
	tail.Add(retainedMessage{key: vault.Key("first"), bytes: 4})
	tail.Add(retainedMessage{key: vault.Key("second"), bytes: 4})
	if tail.length != 1 || tail.bytes != 4 || string(tail.OldestKey()) != "second" {
		t.Fatalf(
			"byte-limited tail = %d records, %d bytes, oldest %q",
			tail.length,
			tail.bytes,
			tail.OldestKey(),
		)
	}
}
