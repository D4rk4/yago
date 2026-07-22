package shardvault

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestMailNewsFullCapSweepResumesAfterCommitInterruption(t *testing.T) {
	fixtures := []mailNewsSweepFixture{
		{name: "mailbox", bucket: mailNewsMailboxBucket, records: 1024, page: 512},
		{name: "news", bucket: mailNewsQueueBucket, records: 4096, page: 1024},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			mailNewsAssertSweepResume(t, fixture)
		})
	}
}

func TestMailNewsMailboxAdmissionRecoversBothCommitOrders(t *testing.T) {
	for _, order := range []string{"previous-first", "accepted-first"} {
		t.Run(order, func(t *testing.T) {
			mailNewsAssertMailboxAdmissionRecovery(t, order)
		})
	}
}

func TestMailNewsRecordAdmissionRecoversLaterShardCommit(t *testing.T) {
	directory := t.TempDir()
	engine, storage := mailNewsOpenStorage(t, directory)
	fixture := mailNewsRegisterRecordFixture(t, storage)
	intent := mailNewsFindRecordAdmissionIntent(t, engine)
	mailNewsPersistRecordAdmission(t, storage, fixture, intent)
	routes := mailNewsRecordAdmissionRoutes(engine, intent)
	failureShard := routes[1]
	realCommit := commitTx
	t.Cleanup(func() { commitTx = realCommit })
	failure := errors.New("injected record admission commit failure")
	commitTx = func(tx *bolt.Tx) error {
		if tx.DB() == engine.shards[failureShard] {
			return failure
		}

		return realCommit(tx)
	}
	if err := mailNewsApplyRecordAdmission(storage, fixture, intent); !errors.Is(err, failure) {
		t.Fatalf("apply record admission error = %v, want %v", err, failure)
	}
	present := mailNewsRecordAdmissionPresence(t, storage, fixture, intent)
	if present == 0 || present == 4 {
		t.Fatalf("partial record admission retained %d of 4 values", present)
	}
	commitTx = realCommit
	mailNewsCloseStorage(t, storage)

	mailNewsAssertRecordAdmissionRecovered(t, directory, intent)
}

func TestMailNewsRotationRecoveryConvergesAfterRepeatedShardFailures(t *testing.T) {
	directory := t.TempDir()
	engine, storage := mailNewsOpenStorage(t, directory)
	fixture := mailNewsRegisterRecordFixture(t, storage)
	intent := mailNewsFindRotationIntent(t, engine)
	mailNewsSeedRotation(t, storage, fixture, intent)
	failureShard := max(
		engine.route(mailNewsQueueBucket, vault.Key(intent.Source)),
		engine.route(mailNewsQueueBucket, vault.Key(intent.Target)),
	)
	realCommit := commitTx
	t.Cleanup(func() { commitTx = realCommit })
	failure := errors.New("injected rotation commit failure")
	commitTx = func(tx *bolt.Tx) error {
		if tx.DB() == engine.shards[failureShard] {
			return failure
		}

		return realCommit(tx)
	}
	if err := mailNewsApplyRotation(storage, fixture, intent); !errors.Is(err, failure) {
		t.Fatalf("apply rotation error = %v, want %v", err, failure)
	}
	if err := mailNewsRecoverRotation(storage, fixture); !errors.Is(err, failure) {
		t.Fatalf("repeat rotation recovery error = %v, want %v", err, failure)
	}
	commitTx = realCommit
	mailNewsCloseStorage(t, storage)

	mailNewsAssertRotationRecovered(t, directory, intent)
}

func TestMailNewsCursorFloorsConvergeAcrossReopen(t *testing.T) {
	directory := t.TempDir()
	engine, storage := mailNewsOpenStorage(t, directory)
	fixture := mailNewsRegisterRecordFixture(t, storage)
	expectations := mailNewsFindCursorExpectations(t, engine)
	mailNewsSeedCursorRows(t, storage, fixture, expectations)
	realCommit := commitTx
	t.Cleanup(func() { commitTx = realCommit })
	failure := errors.New("injected cursor floor commit failure")
	commitTx = func(tx *bolt.Tx) error {
		if tx.DB() == engine.shards[expectations[1].shard] {
			return failure
		}

		return realCommit(tx)
	}
	if err := mailNewsReconcileCursorFloors(storage, fixture); !errors.Is(err, failure) {
		t.Fatalf("reconcile cursor floors error = %v, want %v", err, failure)
	}
	mailNewsAssertCursorValue(t, storage, fixture, expectations[0], expectations[0].sequence)
	mailNewsAssertCursorValue(t, storage, fixture, expectations[1], 1)
	commitTx = realCommit
	mailNewsCloseStorage(t, storage)

	mailNewsAssertCursorFloorsRecovered(t, directory, expectations)
}

func mailNewsAssertSweepResume(t *testing.T, sweep mailNewsSweepFixture) {
	t.Helper()
	directory := t.TempDir()
	_, storage := mailNewsOpenStorage(t, directory)
	fixture := mailNewsRegisterSweepFixture(t, storage, sweep.bucket)
	mailNewsSeedSweepRows(t, storage, fixture, sweep.records)
	mailNewsCloseStorage(t, storage)

	_, failedStorage := mailNewsOpenStorage(t, directory)
	failedFixture := mailNewsRegisterSweepFixture(t, failedStorage, sweep.bucket)
	realCommit := commitTx
	t.Cleanup(func() { commitTx = realCommit })
	commits := 0
	failure := errors.New("injected cleanup cursor commit failure")
	commitTx = func(tx *bolt.Tx) error {
		commits++
		if commits == 2 {
			return failure
		}

		return realCommit(tx)
	}
	firstAfter, err := mailNewsSweepRows(
		failedStorage,
		failedFixture,
		sweep.page,
	)
	if !errors.Is(
		err,
		failure,
	) {
		t.Fatalf("interrupted sweep error = %v, want %v", err, failure)
	}
	if firstAfter != nil {
		t.Fatalf("fresh sweep started after %q", firstAfter)
	}
	cursor, found := mailNewsReadValue(
		t,
		failedStorage,
		failedFixture.cursor,
		vault.Key("scrub"),
	)
	if !found || cursor.After == "" {
		t.Fatal("cleanup cursor was not durable before interruption")
	}
	durableAfter := vault.Key(cursor.After)
	expectedAfter := vault.Key(fmt.Sprintf("row-%08d", sweep.page-1))
	if !bytes.Equal(durableAfter, expectedAfter) {
		t.Fatalf("durable cursor = %q, want %q", durableAfter, expectedAfter)
	}
	commitTx = realCommit
	mailNewsCloseStorage(t, failedStorage)

	mailNewsAssertSweepRecovered(t, directory, sweep, durableAfter, realCommit)
}

func mailNewsAssertSweepRecovered(
	t *testing.T,
	directory string,
	sweep mailNewsSweepFixture,
	durableAfter vault.Key,
	realCommit func(*bolt.Tx) error,
) {
	t.Helper()
	engine, storage := mailNewsOpenStorage(t, directory)
	t.Cleanup(func() { _ = storage.Close() })
	fixture := mailNewsRegisterSweepFixture(t, storage, sweep.bucket)
	commits := 0
	commitTx = func(tx *bolt.Tx) error {
		commits++

		return realCommit(tx)
	}
	firstAfter, err := mailNewsSweepRows(storage, fixture, sweep.page)
	if err != nil {
		t.Fatal(err)
	}
	commitTx = realCommit
	if !bytes.Equal(firstAfter, durableAfter) {
		t.Fatalf(
			"reopened %s sweep started after %q, want %q",
			sweep.name,
			firstAfter,
			durableAfter,
		)
	}
	if got := mailNewsBucketRows(t, engine, sweep.bucket); got != sweep.records {
		t.Fatalf("recovered %s rows = %d, want %d", sweep.name, got, sweep.records)
	}
	if _, found := mailNewsReadValue(t, storage, fixture.cursor, vault.Key("scrub")); found {
		t.Fatal("completed cleanup retained its cursor")
	}
	expectedCommits := (sweep.records - sweep.page + sweep.page - 1) / sweep.page
	if commits != expectedCommits {
		t.Fatalf(
			"resumed %s cleanup used %d commits, want %d",
			sweep.name,
			commits,
			expectedCommits,
		)
	}
}

func mailNewsAssertMailboxAdmissionRecovery(t *testing.T, order string) {
	t.Helper()
	directory := t.TempDir()
	engine, storage := mailNewsOpenStorage(t, directory)
	fixture := mailNewsRegisterMailboxFixture(t, storage)
	intent := mailNewsSeedMailbox(t, engine, storage, fixture, order)
	mailNewsPersistMailboxAdmission(t, storage, fixture, intent)
	failureShard := engine.route(mailNewsMailboxBucket, vault.Key(intent.Accepted))
	if order == "accepted-first" {
		failureShard = engine.route(mailNewsMailboxBucket, vault.Key(intent.Previous))
	}
	realCommit := commitTx
	t.Cleanup(func() { commitTx = realCommit })
	failure := errors.New("injected mailbox admission commit failure")
	commitTx = func(tx *bolt.Tx) error {
		if tx.DB() == engine.shards[failureShard] {
			return failure
		}

		return realCommit(tx)
	}
	if err := mailNewsApplyMailboxAdmission(storage, fixture, intent); !errors.Is(err, failure) {
		t.Fatalf("apply mailbox admission error = %v, want %v", err, failure)
	}
	mailNewsAssertPartialMailboxState(t, storage, fixture, intent, order)
	commitTx = realCommit
	mailNewsCloseStorage(t, storage)

	mailNewsAssertMailboxRecovered(t, directory, intent)
}

func mailNewsAssertPartialMailboxState(
	t *testing.T,
	storage *vault.Vault,
	fixture mailNewsMailboxFixture,
	intent mailNewsMailboxAdmissionIntent,
	order string,
) {
	t.Helper()
	_, previousFound := mailNewsReadValue(t, storage, fixture.rows, vault.Key(intent.Previous))
	_, acceptedFound := mailNewsReadValue(t, storage, fixture.rows, vault.Key(intent.Accepted))
	if order == "previous-first" && (previousFound || acceptedFound) {
		t.Fatalf(
			"previous-first partial state = previous:%t accepted:%t",
			previousFound,
			acceptedFound,
		)
	}
	if order == "accepted-first" && (!previousFound || !acceptedFound) {
		t.Fatalf(
			"accepted-first partial state = previous:%t accepted:%t",
			previousFound,
			acceptedFound,
		)
	}
}

func mailNewsAssertMailboxRecovered(
	t *testing.T,
	directory string,
	intent mailNewsMailboxAdmissionIntent,
) {
	t.Helper()
	engine, storage := mailNewsOpenStorage(t, directory)
	t.Cleanup(func() { _ = storage.Close() })
	fixture := mailNewsRegisterMailboxFixture(t, storage)
	if err := mailNewsRecoverMailboxAdmission(storage, fixture); err != nil {
		t.Fatal(err)
	}
	if got := mailNewsBucketRows(t, engine, mailNewsMailboxBucket); got != 1024 {
		t.Fatalf("recovered mailbox rows = %d, want 1024", got)
	}
	if _, found := mailNewsReadValue(t, storage, fixture.rows, vault.Key(intent.Previous)); found {
		t.Fatal("recovered mailbox retained evicted row")
	}
	accepted, found := mailNewsReadValue(t, storage, fixture.rows, vault.Key(intent.Accepted))
	if !found || accepted != intent.Row {
		t.Fatalf("recovered mailbox row = %#v/%t", accepted, found)
	}
	if _, found := mailNewsReadValue(t, storage, fixture.recovery, vault.Key("admission")); found {
		t.Fatal("recovered mailbox retained admission intent")
	}
	if err := mailNewsRecoverMailboxAdmission(storage, fixture); err != nil {
		t.Fatal(fmt.Errorf("repeat mailbox recovery: %w", err))
	}
}
