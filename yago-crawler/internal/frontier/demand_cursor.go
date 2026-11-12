package frontier

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

	bolt "go.etcd.io/bbolt"
)

const demandCursorKey = "search-demand-cursor"

func (frontier *Frontier) SearchDemandCursor(ctx context.Context) (string, error) {
	if err := checkContext(ctx); err != nil {
		return "", err
	}
	var cursor uint64
	err := frontier.database.View(func(transaction *bolt.Tx) error {
		raw := transaction.Bucket([]byte(metadataBucketName)).Get([]byte(demandCursorKey))
		if raw == nil {
			return nil
		}
		if len(raw) != 8 {
			return errors.New("invalid search demand cursor")
		}
		cursor = binary.BigEndian.Uint64(raw)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("read search demand cursor: %w", err)
	}
	return strconv.FormatUint(cursor, 10), nil
}

func (frontier *Frontier) SaveSearchDemandCursor(ctx context.Context, cursor string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	sequence, err := strconv.ParseUint(cursor, 10, 64)
	if err != nil || strconv.FormatUint(sequence, 10) != cursor {
		return errors.New("invalid search demand cursor")
	}
	err = frontier.database.Update(func(transaction *bolt.Tx) error {
		metadata := transaction.Bucket([]byte(metadataBucketName))
		raw := metadata.Get([]byte(demandCursorKey))
		if raw != nil {
			if len(raw) != 8 {
				return errors.New("invalid search demand cursor")
			}
			if sequence < binary.BigEndian.Uint64(raw) {
				return errors.New("search demand cursor cannot move backwards")
			}
		}
		encoded := make([]byte, 8)
		binary.BigEndian.PutUint64(encoded, sequence)
		return metadata.Put([]byte(demandCursorKey), encoded)
	})
	if err != nil {
		return fmt.Errorf("save search demand cursor: %w", err)
	}
	return nil
}
