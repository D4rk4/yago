package crawlruns

import (
	"bytes"
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type terminalDeliveryDecision func(
	durable terminalDeliveryRecord,
	found bool,
) (*terminalDeliveryRecord, error)

func (r *Registry) reviseTerminalDeliveryLocked(
	ctx context.Context,
	runID string,
	operation string,
	decide terminalDeliveryDecision,
) ([]terminalDeliveryRecord, error) {
	if r.terminal.storage == nil {
		return nil, nil
	}
	var evicted []terminalDeliveryRecord
	err := r.terminal.storage.Update(ctx, func(transaction *vault.Txn) error {
		evicted = evicted[:0]
		durable, found, err := r.terminal.deliveries.Get(transaction, vault.Key(runID))
		if err != nil {
			return fmt.Errorf("read terminal crawl progress delivery: %w", err)
		}
		revised, err := decide(durable, found)
		if err != nil || revised == nil {
			return err
		}
		if err := r.terminal.deliveries.Put(transaction, vault.Key(runID), *revised); err != nil {
			return fmt.Errorf("store terminal crawl progress delivery: %w", err)
		}

		return r.pruneConfirmedTerminalDeliveries(transaction, &evicted)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}

	return evicted, nil
}

func (record terminalDeliveryRecord) sameDelivery(durable terminalDeliveryRecord) bool {
	return bytes.Equal(durable.Identity, record.Identity) && durable.Progress.Equal(record.Progress)
}
