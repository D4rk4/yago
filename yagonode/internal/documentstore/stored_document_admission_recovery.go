package documentstore

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func loadStoredDocumentAdmissionHighWater(
	v *vault.Vault,
	admissions *vault.Keyspace[uint64],
) (uint64, uint64, error) {
	persisted := uint64(0)
	physical := uint64(0)
	err := v.View(context.Background(), func(tx *vault.Txn) error {
		stored, found, err := admissions.Get(tx, documentAdmissionHighWaterKey)
		if err != nil {
			return fmt.Errorf("read document admission high water: %w", err)
		}
		if found {
			persisted = stored
		}
		lastKey, err := tx.ReadBucketLastKey(orderedDocumentBucketName)
		if err != nil {
			return fmt.Errorf("read ordered document high key: %w", err)
		}
		if lastKey != nil {
			physical, _, err = decodeOrderedDocumentKey(lastKey)
			if err != nil {
				return fmt.Errorf("decode ordered document high key: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("open document admissions: %w", err)
	}

	return persisted, physical, nil
}

func persistRecoveredStoredDocumentAdmissionHighWater(
	v *vault.Vault,
	admissions *vault.Keyspace[uint64],
	highWater uint64,
) (uint64, error) {
	err := v.Update(context.Background(), func(tx *vault.Txn) error {
		durable, found, err := admissions.Get(tx, documentAdmissionHighWaterKey)
		if err != nil {
			return fmt.Errorf("reread document admission high water: %w", err)
		}
		if found && durable >= highWater {
			highWater = durable

			return nil
		}

		return admissions.Put(tx, documentAdmissionHighWaterKey, highWater)
	})
	if err != nil {
		return 0, fmt.Errorf("persist recovered document admission: %w", err)
	}

	return highWater, nil
}
