package crawlruns

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	terminalDeliveryBucket       vault.Name = "crawlrunterminaldeliveries"
	terminalDeliveryRecordFormat byte       = 1
)

var errTerminalDeliveryConflict = errors.New(
	"terminal crawl progress delivery conflicts with durable state",
)

type terminalDeliveryRecord struct {
	Identity  []byte                             `json:"identity"`
	Progress  yagocrawlcontract.CrawlRunProgress `json:"progress"`
	Run       Run                                `json:"run"`
	Confirmed bool                               `json:"confirmed,omitempty"`
}

type terminalDeliveryCodec struct{}

func (terminalDeliveryCodec) Encode(record terminalDeliveryRecord) ([]byte, error) {
	encoded, _ := json.Marshal(record)

	return append([]byte{terminalDeliveryRecordFormat}, encoded...), nil
}

func (terminalDeliveryCodec) Decode(raw []byte) (terminalDeliveryRecord, error) {
	if len(raw) < 2 || raw[0] != terminalDeliveryRecordFormat {
		return terminalDeliveryRecord{}, fmt.Errorf("invalid terminal crawl progress delivery")
	}
	var record terminalDeliveryRecord
	if err := json.Unmarshal(raw[1:], &record); err != nil {
		return terminalDeliveryRecord{}, fmt.Errorf(
			"decode terminal crawl progress delivery: %w",
			err,
		)
	}
	if err := validateTerminalDeliveryRecord(record); err != nil {
		return terminalDeliveryRecord{}, err
	}

	return record, nil
}

type terminalDeliveryLedger struct {
	storage    *vault.Vault
	deliveries *vault.Collection[terminalDeliveryRecord]
	records    map[string]terminalDeliveryRecord
	owners     map[string]string
}

func newTerminalDeliveryLedger() *terminalDeliveryLedger {
	return &terminalDeliveryLedger{
		records: make(map[string]terminalDeliveryRecord),
		owners:  make(map[string]string),
	}
}

func Open(ctx context.Context, storage *vault.Vault, capacity int) (*Registry, error) {
	registry := New(capacity)
	deliveries, err := vault.Register(storage, terminalDeliveryBucket, terminalDeliveryCodec{})
	if err != nil {
		return nil, fmt.Errorf("register terminal crawl progress deliveries: %w", err)
	}
	registry.terminal.storage = storage
	registry.terminal.deliveries = deliveries
	if err := registry.loadTerminalDeliveries(ctx); err != nil {
		return nil, err
	}

	return registry, nil
}

func (r *Registry) RecordTerminal(
	ctx context.Context,
	identity []byte,
	progress yagocrawlcontract.CrawlRunProgress,
) error {
	if err := validateTerminalProgress(identity, progress); err != nil {
		return err
	}

	r.mu.Lock()
	identityKey := string(identity)
	if owner, found := r.terminal.owners[identityKey]; found {
		record := r.terminal.records[owner]
		if owner != progress.RunID || record.Progress != progress {
			r.mu.Unlock()

			return errTerminalDeliveryConflict
		}
		r.mu.Unlock()

		return nil
	}
	_, deliveredBefore := r.terminal.records[progress.RunID]
	previousRun, runExisted := r.runs[progress.RunID]
	if deliveredBefore {
		r.mu.Unlock()

		return errTerminalDeliveryConflict
	}
	run := terminalRunFromProgress(progress, previousRun, runExisted, r.now())
	record := terminalDeliveryRecord{
		Identity: append([]byte(nil), identity...),
		Progress: progress,
		Run:      run,
	}
	evicted, err := r.recordTerminalDeliveryLocked(ctx, record)
	if err != nil {
		r.mu.Unlock()

		return err
	}
	r.terminal.records[progress.RunID] = record
	r.terminal.owners[identityKey] = progress.RunID
	for _, removed := range evicted {
		delete(r.terminal.records, removed.Run.RunID)
		delete(r.terminal.owners, string(removed.Identity))
	}
	r.reconcileRunActivityLocked(runExisted, previousRun, run)
	r.runs[progress.RunID] = run
	r.evictLocked()
	newlyTerminal := !runExisted || !isTerminal(previousRun.State)
	active := r.activeCountLocked()
	observers := append([]func(Run, bool, int){}, r.observers...)
	r.mu.Unlock()

	for _, observe := range observers {
		observe(run, newlyTerminal, active)
	}

	return nil
}

func (r *Registry) ConfirmTerminalDelivery(ctx context.Context, identity []byte) error {
	if len(identity) != sha256.Size {
		return fmt.Errorf("invalid terminal crawl progress delivery")
	}

	r.mu.Lock()
	owner, found := r.terminal.owners[string(identity)]
	if !found {
		r.mu.Unlock()

		return nil
	}
	record := r.terminal.records[owner]
	if record.Confirmed {
		r.mu.Unlock()

		return nil
	}
	record.Confirmed = true
	evicted, err := r.confirmTerminalDeliveryLocked(ctx, record)
	if err != nil {
		r.mu.Unlock()

		return err
	}
	r.terminal.records[owner] = record
	for _, removed := range evicted {
		delete(r.terminal.records, removed.Run.RunID)
		delete(r.terminal.owners, string(removed.Identity))
	}
	r.mu.Unlock()

	return nil
}

func (r *Registry) loadTerminalDeliveries(ctx context.Context) error {
	loaded := make([]terminalDeliveryRecord, 0, r.capacity)
	if err := r.terminal.storage.View(ctx, func(transaction *vault.Txn) error {
		return r.terminal.deliveries.Scan(transaction, nil, func(
			key vault.Key,
			record terminalDeliveryRecord,
		) (bool, error) {
			if string(key) != record.Run.RunID {
				return false, fmt.Errorf("terminal crawl progress delivery key conflicts with run")
			}
			loaded = append(loaded, record)

			return true, nil
		})
	}); err != nil {
		return fmt.Errorf("load terminal crawl progress deliveries: %w", err)
	}
	sortTerminalDeliveriesOldestFirst(loaded)
	pruned := terminalDeliveriesToPrune(loaded, r.capacity)
	if len(pruned) > 0 {
		if err := r.terminal.storage.Update(ctx, func(transaction *vault.Txn) error {
			for _, record := range pruned {
				if _, err := r.terminal.deliveries.Delete(
					transaction,
					vault.Key(record.Run.RunID),
				); err != nil {
					return fmt.Errorf("delete terminal crawl progress delivery: %w", err)
				}
			}

			return nil
		}); err != nil {
			return fmt.Errorf("prune terminal crawl progress deliveries: %w", err)
		}
	}
	prunedRuns := make(map[string]struct{}, len(pruned))
	for _, record := range pruned {
		prunedRuns[record.Run.RunID] = struct{}{}
	}
	for _, record := range loaded {
		if _, removed := prunedRuns[record.Run.RunID]; removed {
			continue
		}
		identityKey := string(record.Identity)
		if _, exists := r.terminal.owners[identityKey]; exists {
			return errTerminalDeliveryConflict
		}
		r.terminal.records[record.Run.RunID] = record
		r.terminal.owners[identityKey] = record.Run.RunID
		r.runs[record.Run.RunID] = record.Run
	}
	r.evictLocked()

	return nil
}

func (r *Registry) recordTerminalDeliveryLocked(
	ctx context.Context,
	record terminalDeliveryRecord,
) ([]terminalDeliveryRecord, error) {
	if r.terminal.storage == nil {
		return nil, nil
	}
	evicted := make([]terminalDeliveryRecord, 0, 1)
	err := r.terminal.storage.Update(ctx, func(transaction *vault.Txn) error {
		evicted = evicted[:0]
		stored, found, err := r.terminal.deliveries.Get(transaction, vault.Key(record.Run.RunID))
		if err != nil {
			return fmt.Errorf("read terminal crawl progress delivery: %w", err)
		}
		if found {
			if !bytes.Equal(stored.Identity, record.Identity) ||
				stored.Progress != record.Progress {
				return errTerminalDeliveryConflict
			}

			return nil
		}
		if err := r.terminal.deliveries.Put(
			transaction,
			vault.Key(record.Run.RunID),
			record,
		); err != nil {
			return fmt.Errorf("store terminal crawl progress delivery: %w", err)
		}
		return r.pruneConfirmedTerminalDeliveries(transaction, &evicted)
	})
	if err != nil {
		return nil, fmt.Errorf("persist terminal crawl progress delivery: %w", err)
	}

	return evicted, nil
}

func (r *Registry) confirmTerminalDeliveryLocked(
	ctx context.Context,
	record terminalDeliveryRecord,
) ([]terminalDeliveryRecord, error) {
	if r.terminal.storage == nil {
		return nil, nil
	}
	evicted := make([]terminalDeliveryRecord, 0, 1)
	err := r.terminal.storage.Update(ctx, func(transaction *vault.Txn) error {
		evicted = evicted[:0]
		stored, found, err := r.terminal.deliveries.Get(transaction, vault.Key(record.Run.RunID))
		if err != nil {
			return fmt.Errorf("read terminal crawl progress delivery: %w", err)
		}
		if !found {
			return errTerminalDeliveryConflict
		}
		if !bytes.Equal(stored.Identity, record.Identity) || stored.Progress != record.Progress {
			return errTerminalDeliveryConflict
		}
		stored.Confirmed = true
		if err := r.terminal.deliveries.Put(
			transaction,
			vault.Key(record.Run.RunID),
			stored,
		); err != nil {
			return fmt.Errorf("store confirmed terminal crawl progress delivery: %w", err)
		}

		return r.pruneConfirmedTerminalDeliveries(transaction, &evicted)
	})
	if err != nil {
		return nil, fmt.Errorf("confirm terminal crawl progress delivery: %w", err)
	}

	return evicted, nil
}

func (r *Registry) pruneConfirmedTerminalDeliveries(
	transaction *vault.Txn,
	evicted *[]terminalDeliveryRecord,
) error {
	length, err := r.terminal.deliveries.Len(transaction)
	if err != nil {
		return fmt.Errorf("count terminal crawl progress deliveries: %w", err)
	}
	for length > r.capacity {
		oldest, found, err := oldestConfirmedTerminalDelivery(transaction, r.terminal.deliveries)
		if err != nil {
			return fmt.Errorf("find oldest confirmed terminal crawl progress delivery: %w", err)
		}
		if !found {
			return nil
		}
		if _, err := r.terminal.deliveries.Delete(
			transaction,
			vault.Key(oldest.Run.RunID),
		); err != nil {
			return fmt.Errorf("delete confirmed terminal crawl progress delivery: %w", err)
		}
		*evicted = append(*evicted, oldest)
		length--
	}

	return nil
}

func oldestConfirmedTerminalDelivery(
	transaction *vault.Txn,
	deliveries *vault.Collection[terminalDeliveryRecord],
) (terminalDeliveryRecord, bool, error) {
	var oldest terminalDeliveryRecord
	found := false
	err := deliveries.Scan(transaction, nil, func(
		_ vault.Key,
		record terminalDeliveryRecord,
	) (bool, error) {
		if !record.Confirmed {
			return true, nil
		}
		if !found || terminalDeliveryLess(record, oldest) {
			oldest = record
			found = true
		}

		return true, nil
	})
	if err != nil {
		return terminalDeliveryRecord{}, false, fmt.Errorf(
			"scan confirmed terminal crawl progress deliveries: %w",
			err,
		)
	}

	return oldest, found, nil
}

func terminalDeliveriesToPrune(
	records []terminalDeliveryRecord,
	capacity int,
) []terminalDeliveryRecord {
	remove := max(len(records)-capacity, 0)
	pruned := make([]terminalDeliveryRecord, 0, remove)
	for _, record := range records {
		if remove == 0 {
			break
		}
		if !record.Confirmed {
			continue
		}
		pruned = append(pruned, record)
		remove--
	}

	return pruned
}

func sortTerminalDeliveriesOldestFirst(records []terminalDeliveryRecord) {
	sort.Slice(records, func(left, right int) bool {
		return terminalDeliveryLess(records[left], records[right])
	})
}

func terminalDeliveryLess(left, right terminalDeliveryRecord) bool {
	if left.Run.Updated.Equal(right.Run.Updated) {
		return left.Run.RunID < right.Run.RunID
	}

	return left.Run.Updated.Before(right.Run.Updated)
}

func terminalRunFromProgress(
	progress yagocrawlcontract.CrawlRunProgress,
	previous Run,
	existed bool,
	now time.Time,
) Run {
	run := previous
	if !existed {
		run.RunID = progress.RunID
		run.FirstSeen = now
	}
	run.WorkerID = progress.WorkerID
	run.ProfileHandle = progress.ProfileHandle
	run.ProfileName = progress.ProfileName
	run.State = progress.State
	run.Tally = progress.Tally
	run.RecentOutcomes = run.RecentOutcomes.Merge(progress.RecentOutcomes)
	if progress.RateKnown {
		run.PagesPerMinute = progress.PagesPerMinute
		run.RateKnown = true
	}
	if progress.LimitsKnown {
		run.MaxPagesPerHost = progress.MaxPagesPerHost
		run.MaxPagesPerRun = progress.MaxPagesPerRun
		run.LimitsKnown = true
	}
	run.Updated = now

	return run
}

func validateTerminalProgress(
	identity []byte,
	progress yagocrawlcontract.CrawlRunProgress,
) error {
	if len(identity) != sha256.Size || progress.RunID == "" || !isTerminal(progress.State) ||
		progress.Tally.Pending != 0 || !progress.RecentOutcomes.Valid() ||
		progress.LimitsKnown && !yagocrawlcontract.ValidCrawlRunLimits(
			progress.MaxPagesPerHost,
			progress.MaxPagesPerRun,
		) {
		return fmt.Errorf("invalid terminal crawl progress delivery")
	}

	return nil
}

func validateTerminalDeliveryRecord(record terminalDeliveryRecord) error {
	if err := validateTerminalProgress(record.Identity, record.Progress); err != nil {
		return err
	}
	if record.Run.RunID != record.Progress.RunID ||
		record.Run.WorkerID != record.Progress.WorkerID ||
		record.Run.ProfileHandle != record.Progress.ProfileHandle ||
		record.Run.ProfileName != record.Progress.ProfileName ||
		record.Run.State != record.Progress.State ||
		record.Run.Tally != record.Progress.Tally ||
		!record.Run.RecentOutcomes.Valid() ||
		record.Run.FirstSeen.IsZero() ||
		record.Run.Updated.IsZero() ||
		record.Progress.RateKnown && (!record.Run.RateKnown ||
			record.Run.PagesPerMinute != record.Progress.PagesPerMinute) ||
		record.Progress.LimitsKnown && (!record.Run.LimitsKnown ||
			record.Run.MaxPagesPerHost != record.Progress.MaxPagesPerHost ||
			record.Run.MaxPagesPerRun != record.Progress.MaxPagesPerRun) {
		return fmt.Errorf("invalid terminal crawl progress delivery")
	}

	return nil
}
