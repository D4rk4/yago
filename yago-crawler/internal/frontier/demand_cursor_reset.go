package frontier

import (
	"context"
	"encoding/binary"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func (frontier *Frontier) ResetSearchDemandCursor(ctx context.Context) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	err := frontier.database.Update(func(transaction *bolt.Tx) error {
		return transaction.Bucket([]byte(metadataBucketName)).Put(
			[]byte(demandCursorKey), binary.BigEndian.AppendUint64(nil, 0),
		)
	})
	if err != nil {
		return fmt.Errorf("reset search demand cursor: %w", err)
	}
	return nil
}
