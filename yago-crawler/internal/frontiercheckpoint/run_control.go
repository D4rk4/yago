package frontiercheckpoint

import (
	"context"

	bolt "go.etcd.io/bbolt"
)

func (checkpoint *FrontierCheckpoint) UpdateControl(
	ctx context.Context,
	provenance []byte,
	update ControlUpdate,
) error {
	if _, err := provenancePrefix(provenance); err != nil {
		return err
	}
	if update.Paused == nil && !update.Cancelled && update.PagesPerMinute == nil {
		return ErrInvalidControl
	}

	return checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if record.Completed {
			return ErrRunCompleted
		}
		if update.Paused != nil {
			record.Paused = *update.Paused
		}
		record.Cancelled = record.Cancelled || update.Cancelled
		if update.PagesPerMinute != nil {
			record.PagesPerMinute = clonePagesPerMinute(update.PagesPerMinute)
		}

		return writeRunRecord(transaction, provenance, record)
	})
}

func clonePagesPerMinute(value *uint32) *uint32 {
	if value == nil {
		return nil
	}
	cloned := *value

	return &cloned
}
