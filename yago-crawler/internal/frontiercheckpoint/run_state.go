package frontiercheckpoint

import (
	"bytes"
	"context"

	bolt "go.etcd.io/bbolt"
)

func (checkpoint *FrontierCheckpoint) Inspect(
	ctx context.Context,
	provenance []byte,
	orderIdentity []byte,
) (RunState, error) {
	if _, err := provenancePrefix(provenance); err != nil {
		return RunState{}, err
	}
	if len(orderIdentity) == 0 {
		return RunState{}, ErrInvalidIdentity
	}
	state := RunState{Status: RunMissing}
	err := checkpoint.readTransaction(ctx, func(transaction *bolt.Tx) error {
		record, found, err := readRunRecord(transaction, provenance)
		if err != nil || !found {
			return err
		}
		if !bytes.Equal(record.OrderIdentity, orderIdentity) {
			return ErrProvenanceCollision
		}
		if record.Deleting {
			return ErrRunDeleting
		}
		state = RunState{
			Status:       RunActive,
			Pages:        record.Pages,
			Pending:      record.Pending,
			Failed:       record.Failed,
			Seeding:      record.Seeding,
			SeedManifest: record.SeedManifest,
			Tally:        record.Tally,
			Control: RunControl{
				Paused:         record.Paused,
				Cancelled:      record.Cancelled,
				PagesPerMinute: clonePagesPerMinute(record.PagesPerMinute),
			},
		}
		if record.Completed {
			state.Status = RunCompleted
		}

		return nil
	})

	return state, err
}
