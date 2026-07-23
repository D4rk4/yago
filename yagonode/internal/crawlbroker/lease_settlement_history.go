package crawlbroker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	leaseSettlementBucket       vault.Name = "crawlordersettlements"
	leaseSettlementOrderBucket  vault.Name = "crawlordersettlementorder"
	leaseSettlementExpiryBucket vault.Name = "crawlordersettlementexpiry"
	leaseSettlementRecordFormat byte       = 2
	leaseSettlementRetention               = 24 * time.Hour
)

var (
	leaseSettlementNextKey          = vault.Key("leaseSettlementNext")
	leaseSettlementMigrationNextKey = vault.Key("leaseSettlementMigrationNext")
	errLeaseDispositionConflict     = errors.New("crawl lease disposition is no longer available")
)

type leaseSettlementOutcome uint8

const (
	leaseSettlementAcknowledged leaseSettlementOutcome = iota + 1
	leaseSettlementRequeued
)

type leaseSettlementRecord struct {
	Outcome             leaseSettlementOutcome             `json:"outcome"`
	Sequence            uint64                             `json:"sequence"`
	SettledAtUnixNano   int64                              `json:"settled_at_unix_nano,omitempty"`
	OrderIdentity       []byte                             `json:"order_identity,omitempty"`
	WorkerSessionID     string                             `json:"worker_session_id,omitempty"`
	Progress            yagocrawlcontract.CrawlRunProgress `json:"progress,omitempty"`
	Terminal            bool                               `json:"terminal,omitempty"`
	ProgressDelivered   bool                               `json:"progress_delivered,omitempty"`
	FinalizedAtUnixNano int64                              `json:"finalized_at_unix_nano,omitempty"`
}

type leaseSettlementRecordCodec struct{}

type leaseSettlementIdentityCodec struct{}

func (leaseSettlementIdentityCodec) Encode(leaseID []byte) ([]byte, error) {
	return append([]byte{1}, leaseID...), nil
}

func (leaseSettlementIdentityCodec) Decode(raw []byte) ([]byte, error) {
	if len(raw) < 2 || raw[0] != 1 {
		return nil, fmt.Errorf("invalid lease identity")
	}

	return append([]byte(nil), raw[1:]...), nil
}

func (leaseSettlementRecordCodec) Encode(record leaseSettlementRecord) ([]byte, error) {
	encoded, _ := json.Marshal(record)

	return append([]byte{leaseSettlementRecordFormat}, encoded...), nil
}

func (leaseSettlementRecordCodec) Decode(raw []byte) (leaseSettlementRecord, error) {
	if len(raw) == 9 {
		record := legacyLeaseSettlementRecord(raw)
		if err := validateLeaseSettlementRecord(record); err != nil {
			return leaseSettlementRecord{}, err
		}

		return record, nil
	}
	if len(raw) < 2 || raw[0] != leaseSettlementRecordFormat {
		return leaseSettlementRecord{}, fmt.Errorf("invalid lease settlement record")
	}
	var record leaseSettlementRecord
	if err := json.Unmarshal(raw[1:], &record); err != nil {
		return leaseSettlementRecord{}, fmt.Errorf("decode lease settlement record: %w", err)
	}
	if err := validateLeaseSettlementRecord(record); err != nil {
		return leaseSettlementRecord{}, err
	}

	return record, nil
}

func legacyLeaseSettlementRecord(raw []byte) leaseSettlementRecord {
	return leaseSettlementRecord{
		Outcome: leaseSettlementOutcome(raw[0]),
		Sequence: uint64(raw[1])<<56 |
			uint64(raw[2])<<48 |
			uint64(raw[3])<<40 |
			uint64(raw[4])<<32 |
			uint64(raw[5])<<24 |
			uint64(raw[6])<<16 |
			uint64(raw[7])<<8 |
			uint64(raw[8]),
	}
}

func validateLeaseSettlementRecord(record leaseSettlementRecord) error {
	if record.Outcome != leaseSettlementAcknowledged && record.Outcome != leaseSettlementRequeued {
		return fmt.Errorf("invalid lease settlement outcome")
	}
	if !record.Terminal {
		if len(record.OrderIdentity) != 0 || record.WorkerSessionID != "" ||
			record.Progress != (yagocrawlcontract.CrawlRunProgress{}) ||
			record.ProgressDelivered || record.FinalizedAtUnixNano != 0 {
			return fmt.Errorf("invalid legacy lease settlement definition")
		}

		return nil
	}
	if err := validateTerminalLeaseDefinition(record.OrderIdentity, record.Progress); err != nil {
		return err
	}
	if record.FinalizedAtUnixNano < 0 ||
		record.FinalizedAtUnixNano != 0 && !record.ProgressDelivered {
		return fmt.Errorf("invalid terminal lease settlement finalization")
	}

	return nil
}

func (q *DurableOrderQueue) requireLeaseSettlement(
	tx *vault.Txn,
	leaseID string,
	want leaseSettlementOutcome,
) error {
	record, found, err := q.leaseSettlements.Get(tx, vault.Key(leaseID))
	if err != nil {
		return fmt.Errorf("read crawl lease settlement: %w", err)
	}
	if !found || record.Outcome != want {
		return errLeaseDispositionConflict
	}
	if record.Terminal {
		return nil
	}
	now := nowFunc()
	if record.SettledAtUnixNano == 0 {
		record.SettledAtUnixNano = now.UnixNano()
		if err := q.leaseSettlements.Put(tx, vault.Key(leaseID), record); err != nil {
			return fmt.Errorf("migrate crawl lease settlement time: %w", err)
		}
		if err := q.leaseSettlementExpiry.Put(
			tx,
			leaseSettlementExpiryKey(record),
			[]byte(leaseID),
		); err != nil {
			return fmt.Errorf("migrate crawl lease settlement expiry: %w", err)
		}
	}
	if !time.Unix(0, record.SettledAtUnixNano).Add(leaseSettlementRetention).After(now) {
		return errLeaseDispositionConflict
	}

	return nil
}

func (q *DurableOrderQueue) requireTerminalLeaseSettlement(
	tx *vault.Txn,
	leaseID string,
	want leaseSettlementRecord,
) (leaseSettlementRecord, error) {
	record, found, err := q.leaseSettlements.Get(tx, vault.Key(leaseID))
	if err != nil {
		return leaseSettlementRecord{}, fmt.Errorf("read crawl lease settlement: %w", err)
	}
	if !found || !sameTerminalLeaseSettlement(record, want) {
		return leaseSettlementRecord{}, errLeaseDispositionConflict
	}

	return record, nil
}

func sameTerminalLeaseSettlement(left, right leaseSettlementRecord) bool {
	return left.Terminal && right.Terminal && left.Outcome == right.Outcome &&
		bytes.Equal(left.OrderIdentity, right.OrderIdentity) &&
		left.Progress.WorkerID == right.Progress.WorkerID &&
		left.WorkerSessionID == right.WorkerSessionID &&
		left.Progress.State == right.Progress.State && left.Progress.Tally == right.Progress.Tally &&
		left.Progress.RecentOutcomes == right.Progress.RecentOutcomes &&
		left.Progress.PagesPerMinute == right.Progress.PagesPerMinute &&
		left.Progress.RateKnown == right.Progress.RateKnown
}

func (q *DurableOrderQueue) recordLeaseSettlement(
	tx *vault.Txn,
	leaseID string,
	outcome leaseSettlementOutcome,
) error {
	_, err := q.storeLeaseSettlement(tx, leaseID, leaseSettlementRecord{Outcome: outcome})

	return err
}

func (q *DurableOrderQueue) recordTerminalLeaseSettlement(
	tx *vault.Txn,
	leaseID string,
	record leaseSettlementRecord,
) (leaseSettlementRecord, error) {
	return q.storeLeaseSettlement(tx, leaseID, record)
}

func (q *DurableOrderQueue) storeLeaseSettlement(
	tx *vault.Txn,
	leaseID string,
	record leaseSettlementRecord,
) (leaseSettlementRecord, error) {
	existing, found, err := q.leaseSettlements.Get(tx, vault.Key(leaseID))
	if err != nil {
		return leaseSettlementRecord{}, fmt.Errorf("read crawl lease settlement: %w", err)
	}
	if found {
		if record.Terminal {
			if !sameTerminalLeaseSettlement(existing, record) {
				return leaseSettlementRecord{}, errLeaseDispositionConflict
			}
		} else if existing.Outcome != record.Outcome {
			return leaseSettlementRecord{}, errLeaseDispositionConflict
		}

		return existing, nil
	}
	sequence, err := q.reserveLeaseSettlementSequenceTx(tx)
	if err != nil {
		return leaseSettlementRecord{}, err
	}
	record.Sequence = sequence
	record.SettledAtUnixNano = nowFunc().UnixNano()
	if err := q.leaseSettlements.Put(tx, vault.Key(leaseID), record); err != nil {
		return leaseSettlementRecord{}, fmt.Errorf("store crawl lease settlement: %w", err)
	}
	if err := q.leaseSettlementOrder.Put(tx, orderKey(sequence), []byte(leaseID)); err != nil {
		return leaseSettlementRecord{}, fmt.Errorf("store crawl lease settlement index: %w", err)
	}
	if !record.Terminal {
		if err := q.leaseSettlementExpiry.Put(
			tx,
			leaseSettlementExpiryKey(record),
			[]byte(leaseID),
		); err != nil {
			return leaseSettlementRecord{}, fmt.Errorf(
				"store crawl lease settlement expiry: %w",
				err,
			)
		}
	}
	return record, nil
}
