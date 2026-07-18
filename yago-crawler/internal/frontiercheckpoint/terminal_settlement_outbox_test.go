package frontiercheckpoint

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func completedTerminalSettlement(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	leaseID string,
) crawlsettlement.Settlement {
	t.Helper()
	identity := bytes.Repeat([]byte{1}, sha256.Size)
	provenance := []byte("run-" + leaseID)
	beginTestRun(t, checkpoint, provenance, identity)
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish terminal run: %v", err)
	}

	return crawlsettlement.Settlement{
		LeaseID:         leaseID,
		OrderIdentity:   identity,
		Provenance:      provenance,
		WorkerID:        "worker",
		WorkerSessionID: "session",
		Outcome:         crawlsettlement.Delete,
		State:           yagocrawlcontract.CrawlRunFinished,
		Tally:           yagocrawlcontract.CrawlRunTally{Fetched: 1},
	}
}

func TestTerminalSettlementOutboxPersistsEveryPhaseAndDeletesAfterConfirmation(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	settlement := completedTerminalSettlement(t, checkpoint, "lease-phases")
	if err := checkpoint.Stage(testContext, settlement); err != nil {
		t.Fatalf("stage terminal settlement: %v", err)
	}
	if err := checkpoint.Stage(testContext, settlement); err != nil {
		t.Fatalf("repeat terminal settlement stage: %v", err)
	}
	current, found, err := checkpoint.Current(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
	)
	if err != nil || !found || current.Phase != crawlsettlement.AwaitingAcknowledgment {
		t.Fatalf("awaiting terminal settlement = %+v, found=%v err=%v", current, found, err)
	}
	token := bytes.Repeat([]byte{2}, sha256.Size)
	if err := checkpoint.RecordAcknowledgment(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
		token,
	); err != nil {
		t.Fatalf("record terminal acknowledgment: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close acknowledged outbox: %v", err)
	}

	checkpoint = openTestCheckpoint(t, path)
	current, found, err = checkpoint.Current(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
	)
	if err != nil || !found || current.Phase != crawlsettlement.AcknowledgedDeleting ||
		!bytes.Equal(current.ConfirmationToken, token) {
		t.Fatalf("reopened terminal settlement = %+v, found=%v err=%v", current, found, err)
	}
	if err := checkpoint.PrepareConfirmation(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
	); err != nil {
		t.Fatalf("prepare terminal confirmation: %v", err)
	}
	status, err := checkpoint.Status(testContext, settlement.Provenance, settlement.OrderIdentity)
	if err != nil || status != RunMissing {
		t.Fatalf("terminal run after resumable delete = %v, %v", status, err)
	}
	current, found, err = checkpoint.Current(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
	)
	if err != nil || !found || current.Phase != crawlsettlement.Confirming {
		t.Fatalf("confirming terminal settlement = %+v, found=%v err=%v", current, found, err)
	}
	if err := checkpoint.Complete(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
	); err != nil {
		t.Fatalf("complete terminal settlement: %v", err)
	}
	if err := checkpoint.Complete(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
	); err != nil {
		t.Fatalf("repeat terminal settlement completion: %v", err)
	}
	awaiting, err := checkpoint.Awaiting(testContext)
	if err != nil || len(awaiting) != 0 {
		t.Fatalf("terminal outbox after confirmation = %+v, %v", awaiting, err)
	}
}

func TestTerminalSettlementDeletionResumesBeforeConfirmation(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	settlement := completedTerminalSettlementWithPages(
		t,
		checkpoint,
		"lease-delete-crash",
		deletionRowsPerTransaction+17,
	)
	if err := checkpoint.Stage(testContext, settlement); err != nil {
		t.Fatalf("stage deletion settlement: %v", err)
	}
	token := bytes.Repeat([]byte{3}, sha256.Size)
	if err := checkpoint.RecordAcknowledgment(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
		token,
	); err != nil {
		t.Fatalf("record deletion acknowledgment: %v", err)
	}
	prefix, err := provenancePrefix(settlement.Provenance)
	if err != nil {
		t.Fatalf("deletion provenance prefix: %v", err)
	}
	found, err := checkpoint.markRunDeleting(testContext, settlement.Provenance)
	if err != nil || !found {
		t.Fatalf("mark terminal run deleting = %v, %v", found, err)
	}
	done, err := checkpoint.deleteMarkedRunChunk(testContext, settlement.Provenance, prefix)
	if err != nil || done {
		t.Fatalf("partial terminal deletion = %v, %v", done, err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close during terminal deletion: %v", err)
	}

	checkpoint = openTestCheckpoint(t, path)
	if err := checkpoint.PrepareConfirmation(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
	); err != nil {
		t.Fatalf("resume terminal deletion: %v", err)
	}
	current, found, err := checkpoint.Current(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
	)
	if err != nil || !found || current.Phase != crawlsettlement.Confirming {
		t.Fatalf("resumed terminal phase = %+v, found=%v err=%v", current, found, err)
	}
}

func completedTerminalSettlementWithPages(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	leaseID string,
	pages int,
) crawlsettlement.Settlement {
	t.Helper()
	identity := bytes.Repeat([]byte{4}, sha256.Size)
	provenance := []byte("run-" + leaseID)
	beginTestRun(t, checkpoint, provenance, identity)
	admission := make([]Page, 0, pages)
	for index := 0; index < pages; index++ {
		admission = append(admission, testPage(
			fmt.Sprintf("https://example.test/%d", index),
			"example.test",
			fmt.Sprintf("observation-%d", index),
			0,
		))
	}
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		admission,
	); err != nil ||
		admitted != pages {
		t.Fatalf("admit terminal pages = %d, %v, want %d", admitted, err, pages)
	}
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish terminal seeding: %v", err)
	}
	var fetched uint64
	for _, page := range admission {
		if err := checkpoint.CompletePage(
			testContext,
			provenance,
			page.URL,
			testPageCompletion(),
		); err != nil {
			t.Fatalf("complete terminal page %q: %v", page.URL, err)
		}
		fetched++
	}

	return crawlsettlement.Settlement{
		LeaseID:         leaseID,
		OrderIdentity:   identity,
		Provenance:      provenance,
		WorkerID:        "worker",
		WorkerSessionID: "session",
		Outcome:         crawlsettlement.Delete,
		State:           yagocrawlcontract.CrawlRunFinished,
		Tally:           yagocrawlcontract.CrawlRunTally{Fetched: fetched},
	}
}

func TestTerminalSettlementOutboxRebindsOnlyPreTokenWorkerSession(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	settlement := completedTerminalSettlement(t, checkpoint, "lease-conflict")
	if err := checkpoint.Stage(testContext, settlement); err != nil {
		t.Fatalf("stage conflict settlement: %v", err)
	}
	changed := crawlsettlement.Clone(settlement)
	changed.WorkerSessionID = "other-session"
	if err := checkpoint.Stage(testContext, changed); err != nil {
		t.Fatalf("rebind staged worker session: %v", err)
	}
	current, found, err := checkpoint.Current(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
	)
	if err != nil || !found || current.WorkerSessionID != changed.WorkerSessionID {
		t.Fatalf("rebound staged settlement = %+v, found=%v err=%v", current, found, err)
	}
	conflicting := crawlsettlement.Clone(changed)
	conflicting.Outcome = crawlsettlement.Requeue
	if err := checkpoint.Stage(testContext, conflicting); !errors.Is(
		err,
		crawlsettlement.ErrDefinitionConflict,
	) {
		t.Fatalf("conflicting terminal definition = %v", err)
	}
	if err := checkpoint.RecordAcknowledgment(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
		[]byte("short"),
	); !errors.Is(err, crawlsettlement.ErrDefinitionConflict) {
		t.Fatalf("short terminal token = %v", err)
	}
	if err := checkpoint.PrepareConfirmation(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
	); !errors.Is(err, crawlsettlement.ErrDefinitionConflict) {
		t.Fatalf("premature terminal confirmation = %v", err)
	}
	rebound, found, err := checkpoint.RebindWorkerSession(
		testContext,
		current,
		"third-session",
	)
	if err != nil || !found || rebound.WorkerSessionID != "third-session" {
		t.Fatalf("direct worker session rebind = %+v, found=%v err=%v", rebound, found, err)
	}
	token := bytes.Repeat([]byte{7}, sha256.Size)
	if err := checkpoint.RecordAcknowledgment(
		testContext,
		rebound.LeaseID,
		rebound.OrderIdentity,
		token,
	); err != nil {
		t.Fatalf("record rebound acknowledgment: %v", err)
	}
	if _, _, err := checkpoint.RebindWorkerSession(
		testContext,
		rebound,
		"post-token-session",
	); !errors.Is(err, crawlsettlement.ErrDefinitionConflict) {
		t.Fatalf("post-token worker session rebind = %v", err)
	}
	postToken := crawlsettlement.Clone(rebound)
	postToken.WorkerSessionID = "post-token-session"
	if err := checkpoint.Stage(testContext, postToken); !errors.Is(
		err,
		crawlsettlement.ErrDefinitionConflict,
	) {
		t.Fatalf("post-token terminal restage = %v", err)
	}
}

func TestTerminalSettlementAwaitingIsBoundedAndOrdered(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	total := terminalSettlementReconciliationBatchSize + 6
	stageTerminalSettlementBatch(t, checkpoint, total)
	awaiting, err := checkpoint.Awaiting(testContext)
	if err != nil {
		t.Fatalf("read terminal reconciliation batch: %v", err)
	}
	requireInitialTerminalSettlementBatch(t, awaiting)
	inserted := rotateTerminalSettlementBatch(t, checkpoint)
	awaiting, err = checkpoint.Awaiting(testContext)
	if err != nil || len(awaiting) != terminalSettlementReconciliationBatchSize {
		t.Fatalf("rotated terminal reconciliation batch = %d, %v", len(awaiting), err)
	}
	requireRotatedTerminalSettlementBatch(t, awaiting)
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close rotated terminal outbox: %v", err)
	}
	requireRestartedTerminalSettlements(t, path, total, inserted.LeaseID)
}

func stageTerminalSettlementBatch(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	total int,
) {
	t.Helper()
	for index := total - 1; index >= 0; index-- {
		leaseID := fmt.Sprintf("lease-batch-%03d", index)
		settlement := completedTerminalSettlement(t, checkpoint, leaseID)
		if err := checkpoint.Stage(testContext, settlement); err != nil {
			t.Fatalf("stage terminal batch row %d: %v", index, err)
		}
	}
}

func requireInitialTerminalSettlementBatch(
	t *testing.T,
	awaiting []crawlsettlement.Settlement,
) {
	t.Helper()
	if len(awaiting) != terminalSettlementReconciliationBatchSize {
		t.Fatalf("terminal reconciliation batch = %d", len(awaiting))
	}
	for index, settlement := range awaiting {
		want := fmt.Sprintf("lease-batch-%03d", index)
		if settlement.LeaseID != want {
			t.Fatalf(
				"terminal reconciliation row %d = %q, want %q",
				index,
				settlement.LeaseID,
				want,
			)
		}
	}
}

func rotateTerminalSettlementBatch(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
) crawlsettlement.Settlement {
	t.Helper()
	if err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
		outbox := transaction.Bucket(terminalOutboxBucket)
		return errors.Join(
			deleteRow(outbox, []byte("lease-batch-063"), "terminal settlement"),
			deleteRow(outbox, []byte("lease-batch-064"), "terminal settlement"),
		)
	}); err != nil {
		t.Fatalf("delete rows around terminal reconciliation cursor: %v", err)
	}
	inserted := completedTerminalSettlement(t, checkpoint, "lease-batch-063a")
	if err := checkpoint.Stage(testContext, inserted); err != nil {
		t.Fatalf("insert row after terminal reconciliation cursor: %v", err)
	}

	return inserted
}

func requireRotatedTerminalSettlementBatch(
	t *testing.T,
	awaiting []crawlsettlement.Settlement,
) {
	t.Helper()
	for index, settlement := range awaiting {
		want := rotatedTerminalSettlementLeaseID(index)
		if settlement.LeaseID != want {
			t.Fatalf(
				"rotated terminal reconciliation row %d = %q, want %q",
				index,
				settlement.LeaseID,
				want,
			)
		}
	}
}

func rotatedTerminalSettlementLeaseID(index int) string {
	if index == 0 {
		return "lease-batch-063a"
	}
	if index <= 5 {
		return fmt.Sprintf("lease-batch-%03d", index+64)
	}

	return fmt.Sprintf("lease-batch-%03d", index-6)
}

func requireRestartedTerminalSettlements(
	t *testing.T,
	path string,
	total int,
	insertedLeaseID string,
) {
	t.Helper()
	checkpoint := openTestCheckpoint(t, path)
	seen := make(map[string]struct{})
	for range 2 {
		batch, err := checkpoint.Awaiting(testContext)
		if err != nil {
			t.Fatalf("read terminal reconciliation batch after restart: %v", err)
		}
		for _, settlement := range batch {
			seen[settlement.LeaseID] = struct{}{}
		}
	}
	if len(seen) != total-1 {
		t.Fatalf("terminal rows visible after restart = %d, want %d", len(seen), total-1)
	}
	requireRestartedTerminalSettlementRows(t, seen, total)
	if _, found := seen[insertedLeaseID]; !found {
		t.Fatalf("inserted terminal row %q became invisible after restart", insertedLeaseID)
	}
}

func requireRestartedTerminalSettlementRows(
	t *testing.T,
	seen map[string]struct{},
	total int,
) {
	t.Helper()
	for index := range total {
		leaseID := fmt.Sprintf("lease-batch-%03d", index)
		_, found := seen[leaseID]
		if index == 63 || index == 64 {
			if found {
				t.Fatalf("deleted terminal row %q remained visible", leaseID)
			}
		} else if !found {
			t.Fatalf("terminal row %q became invisible after restart", leaseID)
		}
	}
}

func TestOpenMigratesSeedManifestSchemaToTerminalOutbox(t *testing.T) {
	path := testCheckpointPath(t)
	writeRawCheckpoint(t, path, func(transaction *bolt.Tx) error {
		version := make([]byte, 4)
		binary.BigEndian.PutUint32(version, seedManifestSchemaVersion)
		putSchemaVersion(t, transaction, version)
		for _, name := range append(
			append([][]byte(nil), initialSchemaBuckets[1:]...),
			hostPacesBucket,
			hostPaceOrderBucket,
			seedManifestBucket,
		) {
			if _, err := transaction.CreateBucket(name); err != nil {
				return fmt.Errorf("create seed manifest schema bucket: %w", err)
			}
		}

		return nil
	})
	checkpoint := openTestCheckpoint(t, path)
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		if transaction.Bucket(terminalOutboxBucket) == nil {
			t.Fatal("migrated terminal outbox bucket is missing")
		}

		return nil
	}); err != nil {
		t.Fatalf("inspect migrated terminal outbox: %v", err)
	}
}
