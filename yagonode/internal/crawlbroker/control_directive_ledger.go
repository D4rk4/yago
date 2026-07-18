package crawlbroker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	controlDirectiveBucket vault.Name = "crawlcontroldirectives"
	controlDirectiveState  vault.Name = "crawlcontrolstate"
)

var controlDirectiveNextKey = vault.Key("next")

type controlDirectiveLedger interface {
	Enqueue(
		ctx context.Context,
		workerID string,
		directive yagocrawlcontract.CrawlControlDirective,
	) (yagocrawlcontract.CrawlControlDirective, error)
	Exchange(
		ctx context.Context,
		workerID string,
		acknowledged []uint64,
	) ([]yagocrawlcontract.CrawlControlDirective, error)
	ReconcileRun(
		ctx context.Context,
		workerID string,
		runID string,
		terminal bool,
	) error
}

func (l *persistentControlDirectiveLedger) ReconcileRun(
	ctx context.Context,
	workerID string,
	runID string,
	terminal bool,
) error {
	if runID == "" {
		return nil
	}
	pending, err := l.runDirectivePending(ctx, runID)
	if err != nil {
		return err
	}
	if !pending {
		return nil
	}
	err = l.storage.Update(ctx, func(tx *vault.Txn) error {
		return l.reconcileRunTx(tx, workerID, runID, terminal)
	})
	if err != nil {
		return fmt.Errorf("reconcile crawl control run: %w", err)
	}

	return nil
}

func (l *persistentControlDirectiveLedger) runDirectivePending(
	ctx context.Context,
	runID string,
) (bool, error) {
	pending := false
	err := l.storage.View(ctx, func(tx *vault.Txn) error {
		return l.directives.Scan(tx, nil, func(
			_ vault.Key,
			record controlDirectiveRecord,
		) (bool, error) {
			pending = record.Directive.RunID == runID

			return !pending, nil
		})
	})
	if err != nil {
		return false, fmt.Errorf("scan crawl control run directives: %w", err)
	}

	return pending, nil
}

func (l *persistentControlDirectiveLedger) reconcileRunTx(
	tx *vault.Txn,
	workerID string,
	runID string,
	terminal bool,
) error {
	keys := make([]vault.Key, 0)
	records := make([]controlDirectiveRecord, 0)
	if err := l.directives.Scan(tx, nil, func(
		key vault.Key,
		record controlDirectiveRecord,
	) (bool, error) {
		if record.Directive.RunID != runID {
			return true, nil
		}
		keys = append(keys, key)
		record.WorkerID = workerID
		records = append(records, record)

		return true, nil
	}); err != nil {
		return fmt.Errorf("scan crawl control run directives: %w", err)
	}
	for index, key := range keys {
		if terminal {
			if _, err := l.directives.Delete(tx, key); err != nil {
				return fmt.Errorf("delete terminal crawl control directive: %w", err)
			}

			continue
		}
		if err := l.directives.Put(tx, key, records[index]); err != nil {
			return fmt.Errorf("move crawl control directive: %w", err)
		}
	}

	return nil
}

type controlDirectiveRecord struct {
	WorkerID  string                                  `json:"worker"`
	Directive yagocrawlcontract.CrawlControlDirective `json:"directive"`
}

type controlDirectiveRecordCodec struct{}

func (controlDirectiveRecordCodec) Encode(record controlDirectiveRecord) ([]byte, error) {
	raw, _ := json.Marshal(record)

	return raw, nil
}

func (controlDirectiveRecordCodec) Decode(raw []byte) (controlDirectiveRecord, error) {
	var record controlDirectiveRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return controlDirectiveRecord{}, fmt.Errorf("decode crawl control directive: %w", err)
	}

	return record, nil
}

type persistentControlDirectiveLedger struct {
	storage    *vault.Vault
	directives *vault.Collection[controlDirectiveRecord]
	sequence   *vault.Collection[uint64]
}

func newPersistentControlDirectiveLedger(
	storage *vault.Vault,
) (*persistentControlDirectiveLedger, error) {
	directives, err := vault.Register(
		storage,
		controlDirectiveBucket,
		controlDirectiveRecordCodec{},
	)
	if err != nil {
		return nil, fmt.Errorf("register crawl control directives: %w", err)
	}
	sequence, err := vault.Register(storage, controlDirectiveState, sequenceCodec{})
	if err != nil {
		return nil, fmt.Errorf("register crawl control state: %w", err)
	}

	return &persistentControlDirectiveLedger{
		storage:    storage,
		directives: directives,
		sequence:   sequence,
	}, nil
}

func (l *persistentControlDirectiveLedger) Enqueue(
	ctx context.Context,
	workerID string,
	directive yagocrawlcontract.CrawlControlDirective,
) (yagocrawlcontract.CrawlControlDirective, error) {
	var assigned yagocrawlcontract.CrawlControlDirective
	err := l.storage.Update(ctx, func(tx *vault.Txn) error {
		assigned = directive
		next, _, err := l.sequence.Get(tx, controlDirectiveNextKey)
		if err != nil {
			return fmt.Errorf("read crawl control sequence: %w", err)
		}
		assigned.DirectiveID = next + 1
		record := controlDirectiveRecord{WorkerID: workerID, Directive: assigned}
		if err := l.directives.Put(tx, orderKey(assigned.DirectiveID), record); err != nil {
			return fmt.Errorf("store crawl control directive: %w", err)
		}
		if err := l.sequence.Put(tx, controlDirectiveNextKey, assigned.DirectiveID); err != nil {
			return fmt.Errorf("advance crawl control sequence: %w", err)
		}

		return nil
	})
	if err != nil {
		return yagocrawlcontract.CrawlControlDirective{}, fmt.Errorf(
			"enqueue crawl control directive: %w",
			err,
		)
	}

	return assigned, nil
}

func (l *persistentControlDirectiveLedger) Exchange(
	ctx context.Context,
	workerID string,
	acknowledged []uint64,
) ([]yagocrawlcontract.CrawlControlDirective, error) {
	if len(acknowledged) == 0 {
		return l.workerDirectives(ctx, workerID)
	}
	directives := make([]yagocrawlcontract.CrawlControlDirective, 0)
	err := l.storage.Update(ctx, func(tx *vault.Txn) error {
		if err := l.acknowledgeWorkerDirectivesTx(tx, workerID, acknowledged); err != nil {
			return err
		}
		var err error
		directives, err = l.workerDirectivesTx(tx, workerID)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("acknowledge crawl control directives: %w", err)
	}

	return directives, nil
}

type memoryControlDirectiveLedger struct {
	next       uint64
	directives map[uint64]controlDirectiveRecord
}

func newMemoryControlDirectiveLedger() *memoryControlDirectiveLedger {
	return &memoryControlDirectiveLedger{directives: make(map[uint64]controlDirectiveRecord)}
}

func (l *memoryControlDirectiveLedger) Enqueue(
	_ context.Context,
	workerID string,
	directive yagocrawlcontract.CrawlControlDirective,
) (yagocrawlcontract.CrawlControlDirective, error) {
	l.next++
	directive.DirectiveID = l.next
	l.directives[directive.DirectiveID] = controlDirectiveRecord{
		WorkerID:  workerID,
		Directive: directive,
	}

	return directive, nil
}

func (l *memoryControlDirectiveLedger) Exchange(
	_ context.Context,
	workerID string,
	acknowledged []uint64,
) ([]yagocrawlcontract.CrawlControlDirective, error) {
	for _, directiveID := range acknowledged {
		if record, found := l.directives[directiveID]; found && record.WorkerID == workerID {
			delete(l.directives, directiveID)
		}
	}
	directives := make([]yagocrawlcontract.CrawlControlDirective, 0)
	for directiveID := uint64(1); directiveID <= l.next; directiveID++ {
		record, found := l.directives[directiveID]
		if found && record.WorkerID == workerID {
			directives = append(directives, record.Directive)
			if len(directives) == yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments {
				break
			}
		}
	}

	return directives, nil
}

func (l *memoryControlDirectiveLedger) ReconcileRun(
	_ context.Context,
	workerID string,
	runID string,
	terminal bool,
) error {
	if runID == "" {
		return nil
	}
	for directiveID, record := range l.directives {
		if record.Directive.RunID != runID {
			continue
		}
		if terminal {
			delete(l.directives, directiveID)

			continue
		}
		record.WorkerID = workerID
		l.directives[directiveID] = record
	}

	return nil
}
