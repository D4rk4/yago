package crawlbroker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const automaticDiscoverySettlementIntentFormat byte = 1

type automaticDiscoverySettlementIntent struct {
	Lease      leaseRecord           `json:"lease"`
	Target     leaseControlTarget    `json:"target"`
	Settlement leaseSettlementRecord `json:"settlement"`
}

type automaticDiscoverySettlementIntentCodec struct{}

type automaticDiscoverySettlementResolution struct {
	Intent       automaticDiscoverySettlementIntent
	Settlement   leaseSettlementRecord
	Found        bool
	Acknowledged bool
}

func (automaticDiscoverySettlementIntentCodec) Encode(
	intent automaticDiscoverySettlementIntent,
) ([]byte, error) {
	encoded, _ := json.Marshal(intent)

	return append([]byte{automaticDiscoverySettlementIntentFormat}, encoded...), nil
}

func (automaticDiscoverySettlementIntentCodec) Decode(
	raw []byte,
) (automaticDiscoverySettlementIntent, error) {
	if len(raw) < 2 || raw[0] != automaticDiscoverySettlementIntentFormat {
		return automaticDiscoverySettlementIntent{}, fmt.Errorf(
			"invalid automatic crawl discovery settlement intent",
		)
	}
	var intent automaticDiscoverySettlementIntent
	if err := json.Unmarshal(raw[1:], &intent); err != nil {
		return automaticDiscoverySettlementIntent{}, fmt.Errorf(
			"decode automatic crawl discovery settlement intent: %w",
			err,
		)
	}
	if err := validateAutomaticDiscoverySettlementIntent(intent); err != nil {
		return automaticDiscoverySettlementIntent{}, err
	}

	return intent, nil
}

func validateAutomaticDiscoverySettlementIntent(
	intent automaticDiscoverySettlementIntent,
) error {
	if intent.Lease.DiscoveryKey == "" ||
		intent.Settlement.Outcome != leaseSettlementAcknowledged {
		return fmt.Errorf("invalid automatic crawl discovery settlement intent")
	}
	if (intent.Target.WorkerID == "") != (intent.Target.RunID == "") {
		return fmt.Errorf("invalid automatic crawl discovery settlement target")
	}
	if err := validateLeaseSettlementRecord(intent.Settlement); err != nil {
		return fmt.Errorf("invalid automatic crawl discovery settlement: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) stageAutomaticDiscoveryAcknowledgment(
	ctx context.Context,
	leaseID string,
	workerID string,
	workerSessionID string,
	requireOwner bool,
) error {
	_, err := q.stageAutomaticDiscoveryAcknowledgmentForCompletion(
		ctx,
		leaseID,
		workerID,
		workerSessionID,
		requireOwner,
	)

	return err
}

func (q *DurableOrderQueue) stageAutomaticDiscoveryAcknowledgmentForCompletion(
	ctx context.Context,
	leaseID string,
	workerID string,
	workerSessionID string,
	requireOwner bool,
) (bool, error) {
	staged := false
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		var err error
		staged, err = q.stageAutomaticDiscoveryAcknowledgmentTx(
			tx,
			leaseID,
			workerID,
			workerSessionID,
			requireOwner,
		)

		return err
	}); err != nil {
		return false, fmt.Errorf("stage automatic crawl discovery acknowledgment: %w", err)
	}

	return staged, nil
}

func (q *DurableOrderQueue) stageAutomaticDiscoveryAcknowledgmentTx(
	tx *vault.Txn,
	leaseID string,
	workerID string,
	workerSessionID string,
	requireOwner bool,
) (bool, error) {
	record, found, err := q.leases.Get(tx, vault.Key(leaseID))
	if err != nil {
		return false, fmt.Errorf("read crawl lease: %w", err)
	}
	if !found {
		return q.validateRetainedAutomaticDiscoveryAcknowledgmentTx(tx, leaseID)
	}
	if record.Deferred {
		return false, errLeaseDispositionConflict
	}
	if requireOwner && !liveLeaseOwnedBy(record, workerID, workerSessionID, nowFunc()) {
		return false, errLeaseLost
	}
	target, err := controlTargetFromLease(record)
	if err != nil {
		return false, err
	}
	if record.DiscoveryKey == "" {
		return false, nil
	}

	if err := q.persistAutomaticDiscoverySettlementTx(
		tx,
		leaseID,
		automaticDiscoverySettlementIntent{
			Lease:      record,
			Target:     target,
			Settlement: leaseSettlementRecord{Outcome: leaseSettlementAcknowledged},
		},
	); err != nil {
		return false, err
	}

	return true, nil
}

func (q *DurableOrderQueue) validateRetainedAutomaticDiscoveryAcknowledgmentTx(
	tx *vault.Txn,
	leaseID string,
) (bool, error) {
	intent, staged, err := q.discoverySettlements.Get(tx, vault.Key(leaseID))
	if err != nil {
		return false, fmt.Errorf("read automatic crawl discovery settlement intent: %w", err)
	}
	if staged && intent.Settlement.Terminal {
		return false, errLeaseDispositionConflict
	}

	return staged, nil
}

func (q *DurableOrderQueue) stageAutomaticDiscoveryTerminalSettlement(
	ctx context.Context,
	leaseID string,
	request terminalLeaseRequest,
) error {
	_, err := q.stageAutomaticDiscoveryTerminalSettlementForCompletion(
		ctx,
		leaseID,
		request,
	)

	return err
}

func (q *DurableOrderQueue) stageAutomaticDiscoveryTerminalSettlementForCompletion(
	ctx context.Context,
	leaseID string,
	request terminalLeaseRequest,
) (bool, error) {
	staged := false
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		var err error
		staged, err = q.stageAutomaticDiscoveryTerminalSettlementTx(tx, leaseID, request)

		return err
	}); err != nil {
		return false, fmt.Errorf(
			"stage automatic crawl discovery terminal settlement: %w",
			err,
		)
	}

	return staged, nil
}

func (q *DurableOrderQueue) stageAutomaticDiscoveryTerminalSettlementTx(
	tx *vault.Txn,
	leaseID string,
	request terminalLeaseRequest,
) (bool, error) {
	record, found, err := q.leases.Get(tx, vault.Key(leaseID))
	if err != nil {
		return false, fmt.Errorf("read crawl lease: %w", err)
	}
	staged, intentFound, err := q.discoverySettlements.Get(tx, vault.Key(leaseID))
	if err != nil {
		return false, fmt.Errorf("read automatic crawl discovery settlement intent: %w", err)
	}
	if request.Outcome != leaseSettlementAcknowledged {
		if intentFound {
			return false, errLeaseDispositionConflict
		}

		return false, nil
	}
	record, relevant, err := automaticDiscoverySettlementLeaseForRequest(
		record,
		found,
		staged,
		intentFound,
		request,
	)
	if err != nil || !relevant {
		return false, err
	}
	if automaticDiscoverySettlementIdentityConflicts(record, request.OrderIdentity) {
		return false, errLeaseDispositionConflict
	}
	settlement, err := terminalSettlementWithOrder(
		terminalSettlementRecord(request),
		record.OrderData,
	)
	if err != nil {
		return false, err
	}
	if record.DiscoveryKey == "" {
		return false, nil
	}

	if err := q.persistAutomaticDiscoverySettlementTx(
		tx,
		leaseID,
		automaticDiscoverySettlementIntent{
			Lease: record,
			Target: leaseControlTarget{
				WorkerID: request.WorkerID,
				RunID:    settlement.Progress.RunID,
			},
			Settlement: settlement,
		},
	); err != nil {
		return false, err
	}

	return true, nil
}

func automaticDiscoverySettlementLeaseForRequest(
	record leaseRecord,
	found bool,
	staged automaticDiscoverySettlementIntent,
	intentFound bool,
	request terminalLeaseRequest,
) (leaseRecord, bool, error) {
	if found && !record.Deferred {
		if !liveLeaseOwnedBy(record, request.WorkerID, request.WorkerSessionID, nowFunc()) {
			return leaseRecord{}, false, errLeaseLost
		}

		return record, true, nil
	}
	if !intentFound {
		return leaseRecord{}, false, nil
	}
	if staged.Lease.WorkerID != request.WorkerID ||
		staged.Lease.WorkerSessionID != request.WorkerSessionID {
		return leaseRecord{}, false, errLeaseLost
	}

	return staged.Lease, true, nil
}

func automaticDiscoverySettlementIdentityConflicts(
	record leaseRecord,
	orderIdentity []byte,
) bool {
	identity := sha256.Sum256(record.OrderData)

	return !bytes.Equal(identity[:], orderIdentity)
}

func (q *DurableOrderQueue) persistAutomaticDiscoverySettlementTx(
	tx *vault.Txn,
	leaseID string,
	intent automaticDiscoverySettlementIntent,
) error {
	if intent.Lease.DiscoveryKey == "" {
		return nil
	}
	existing, found, err := q.discoverySettlements.Get(tx, vault.Key(leaseID))
	if err != nil {
		return fmt.Errorf("read automatic crawl discovery settlement intent: %w", err)
	}
	if found {
		if !sameAutomaticDiscoverySettlementIntent(existing, intent) {
			return errLeaseDispositionConflict
		}

		return nil
	}
	if err := q.discoverySettlements.Put(tx, vault.Key(leaseID), intent); err != nil {
		return fmt.Errorf("persist automatic crawl discovery settlement: %w", err)
	}

	return nil
}

func sameAutomaticDiscoverySettlementIntent(
	left automaticDiscoverySettlementIntent,
	right automaticDiscoverySettlementIntent,
) bool {
	if !sameAutomaticDiscoverySettlementLease(left.Lease, right.Lease) ||
		left.Target != right.Target ||
		left.Settlement.Terminal != right.Settlement.Terminal {
		return false
	}
	if left.Settlement.Terminal {
		return sameTerminalLeaseSettlement(left.Settlement, right.Settlement)
	}

	return left.Settlement.Outcome == right.Settlement.Outcome
}

func (q *DurableOrderQueue) completeAutomaticDiscoverySettlement(
	ctx context.Context,
	leaseID string,
) (automaticDiscoverySettlementResolution, error) {
	var intent automaticDiscoverySettlementIntent
	var settlement leaseSettlementRecord
	found := false
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		var err error
		intent, settlement, found, err = q.completeAutomaticDiscoverySettlementTx(tx, leaseID)

		return err
	}); err != nil {
		return automaticDiscoverySettlementResolution{}, fmt.Errorf(
			"complete automatic crawl discovery settlement: %w",
			err,
		)
	}
	if !found {
		return automaticDiscoverySettlementResolution{}, nil
	}
	resolution := automaticDiscoverySettlementResolution{
		Intent:       intent,
		Settlement:   settlement,
		Found:        true,
		Acknowledged: true,
	}
	if err := q.releaseAutomaticDiscoverySettlementIntent(ctx, leaseID); err != nil {
		return resolution, err
	}

	return resolution, nil
}

func (q *DurableOrderQueue) completeAutomaticDiscoverySettlementTx(
	tx *vault.Txn,
	leaseID string,
) (automaticDiscoverySettlementIntent, leaseSettlementRecord, bool, error) {
	intent, found, err := q.discoverySettlements.Get(tx, vault.Key(leaseID))
	if err != nil {
		return automaticDiscoverySettlementIntent{}, leaseSettlementRecord{}, false, fmt.Errorf(
			"read automatic crawl discovery settlement: %w",
			err,
		)
	}
	if !found {
		return automaticDiscoverySettlementIntent{}, leaseSettlementRecord{}, false, nil
	}
	if err := q.removeRetainedAutomaticDiscoveryLeaseTx(tx, leaseID, intent); err != nil {
		return automaticDiscoverySettlementIntent{}, leaseSettlementRecord{}, false, err
	}
	if err := q.releaseLeasedAutomaticDiscoveryTx(tx, leaseID, intent.Lease); err != nil {
		return automaticDiscoverySettlementIntent{}, leaseSettlementRecord{}, false, err
	}
	settlement, err := q.ensureAutomaticDiscoveryLeaseSettlementTx(
		tx,
		leaseID,
		intent.Settlement,
	)
	if err != nil {
		return automaticDiscoverySettlementIntent{}, leaseSettlementRecord{}, false, err
	}
	if err := q.releaseActiveAutomaticDiscoveryTx(tx, intent.Lease); err != nil {
		return automaticDiscoverySettlementIntent{}, leaseSettlementRecord{}, false, err
	}

	return intent, settlement, true, nil
}

func (q *DurableOrderQueue) removeRetainedAutomaticDiscoveryLeaseTx(
	tx *vault.Txn,
	leaseID string,
	intent automaticDiscoverySettlementIntent,
) error {
	record, leased, err := q.leases.Get(tx, vault.Key(leaseID))
	if err != nil {
		return fmt.Errorf("read settled automatic crawl discovery lease: %w", err)
	}
	if leased && !sameAutomaticDiscoverySettlementLease(record, intent.Lease) {
		return errLeaseDispositionConflict
	}
	if intent.Target.WorkerID != "" && intent.Target.RunID != "" {
		if err := q.leaseControlTargets.Put(
			tx,
			vault.Key(leaseID),
			intent.Target,
		); err != nil {
			return fmt.Errorf("restore crawl lease control target: %w", err)
		}
	}
	if !leased {
		return nil
	}
	if _, err := q.leases.Delete(tx, vault.Key(leaseID)); err != nil {
		return fmt.Errorf("delete settled automatic crawl discovery lease: %w", err)
	}

	return nil
}

func sameAutomaticDiscoverySettlementLease(left leaseRecord, right leaseRecord) bool {
	return bytes.Equal(left.OrderData, right.OrderData) &&
		left.Priority == right.Priority &&
		left.WorkerID == right.WorkerID &&
		left.WorkerSessionID == right.WorkerSessionID &&
		left.Deferred == right.Deferred &&
		left.DiscoveryKey == right.DiscoveryKey &&
		left.DiscoverySequence == right.DiscoverySequence
}

func (q *DurableOrderQueue) ensureAutomaticDiscoveryLeaseSettlementTx(
	tx *vault.Txn,
	leaseID string,
	want leaseSettlementRecord,
) (leaseSettlementRecord, error) {
	record, found, err := q.leaseSettlements.Get(tx, vault.Key(leaseID))
	if err != nil {
		return leaseSettlementRecord{}, fmt.Errorf("read crawl lease settlement: %w", err)
	}
	if !found {
		return q.storeLeaseSettlement(tx, leaseID, want)
	}
	if automaticDiscoveryLeaseSettlementConflicts(record, want) {
		return leaseSettlementRecord{}, errLeaseDispositionConflict
	}
	record, err = q.relocateAutomaticDiscoverySettlementSequenceTx(tx, leaseID, record)
	if err != nil {
		return leaseSettlementRecord{}, err
	}
	if err := q.restoreAutomaticDiscoverySettlementSequencesTx(tx, record.Sequence); err != nil {
		return leaseSettlementRecord{}, err
	}
	if err := q.restoreAutomaticDiscoverySettlementOrderTx(tx, leaseID, record); err != nil {
		return leaseSettlementRecord{}, err
	}
	if err := q.restoreAutomaticDiscoverySettlementExpiryTx(tx, leaseID, record); err != nil {
		return leaseSettlementRecord{}, err
	}

	return record, nil
}

func automaticDiscoveryLeaseSettlementConflicts(
	record leaseSettlementRecord,
	want leaseSettlementRecord,
) bool {
	return record.Terminal != want.Terminal ||
		record.Terminal && !sameTerminalLeaseSettlement(record, want) ||
		!record.Terminal && record.Outcome != want.Outcome
}

func (q *DurableOrderQueue) restoreAutomaticDiscoverySettlementOrderTx(
	tx *vault.Txn,
	leaseID string,
	record leaseSettlementRecord,
) error {
	if err := q.leaseSettlementOrder.Put(
		tx,
		orderKey(record.Sequence),
		[]byte(leaseID),
	); err != nil {
		return fmt.Errorf("restore crawl lease settlement index: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) restoreAutomaticDiscoverySettlementExpiryTx(
	tx *vault.Txn,
	leaseID string,
	record leaseSettlementRecord,
) error {
	if record.Terminal {
		return nil
	}
	if err := q.leaseSettlementExpiry.Put(
		tx,
		leaseSettlementExpiryKey(record),
		[]byte(leaseID),
	); err != nil {
		return fmt.Errorf("restore crawl lease settlement expiry: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) restoreAutomaticDiscoverySettlementSequencesTx(
	tx *vault.Txn,
	settlementSequence uint64,
) error {
	next, _, err := q.seq.Get(tx, leaseSettlementNextKey)
	if err != nil {
		return fmt.Errorf("read crawl lease settlement sequence: %w", err)
	}
	if next <= settlementSequence {
		if err := q.seq.Put(tx, leaseSettlementNextKey, settlementSequence+1); err != nil {
			return fmt.Errorf(
				"restore crawl lease settlement sequence: %w",
				err,
			)
		}
	}
	migrationNext, _, err := q.seq.Get(tx, leaseSettlementMigrationNextKey)
	if err != nil {
		return fmt.Errorf("read crawl lease settlement migration: %w", err)
	}
	if migrationNext <= settlementSequence {
		if err := q.seq.Put(
			tx,
			leaseSettlementMigrationNextKey,
			settlementSequence+1,
		); err != nil {
			return fmt.Errorf(
				"restore crawl lease settlement migration: %w",
				err,
			)
		}
	}

	return nil
}

func (q *DurableOrderQueue) releaseAutomaticDiscoverySettlementIntent(
	ctx context.Context,
	leaseID string,
) error {
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := q.discoverySettlements.Delete(tx, vault.Key(leaseID)); err != nil {
			return fmt.Errorf("release automatic crawl discovery settlement: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("release automatic crawl discovery settlement intent: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) reconcileAutomaticDiscoverySettlements(ctx context.Context) error {
	leaseIDs := make([]string, 0, 1)
	if err := q.vault.View(ctx, func(tx *vault.Txn) error {
		return q.discoverySettlements.Scan(tx, nil, func(
			key vault.Key,
			_ automaticDiscoverySettlementIntent,
		) (bool, error) {
			leaseIDs = append(leaseIDs, string(key))

			return true, nil
		})
	}); err != nil {
		return fmt.Errorf("read automatic crawl discovery settlements: %w", err)
	}
	for _, leaseID := range leaseIDs {
		if _, err := q.completeAutomaticDiscoverySettlement(ctx, leaseID); err != nil {
			return err
		}
	}

	return nil
}
