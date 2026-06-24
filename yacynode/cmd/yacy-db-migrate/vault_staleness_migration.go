package main

import (
	"bytes"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const (
	schemaStaleness = "vault-2"

	urlMetadataBucket        = "urlmeta"
	stalenessOrderBucket     = "urlmeta_staleness_order"
	stalenessFreshnessBucket = "urlmeta_staleness_freshness"

	stalenessOrderSeparator = 0x00
)

func buildStalenessOrder(tx *bolt.Tx, lengths *bolt.Bucket) error {
	order, err := tx.CreateBucketIfNotExists([]byte(stalenessOrderBucket))
	if err != nil {
		return fmt.Errorf("create staleness order bucket: %w", err)
	}
	freshnessOf, err := tx.CreateBucketIfNotExists([]byte(stalenessFreshnessBucket))
	if err != nil {
		return fmt.Errorf("create staleness freshness bucket: %w", err)
	}

	urls := tx.Bucket([]byte(urlMetadataBucket))
	if urls == nil {
		return putLength(lengths, stalenessOrderBucket, 0)
	}

	recorded := 0
	var corrupt [][]byte
	if err := urls.ForEach(func(hash, encoded []byte) error {
		row, decodeErr := yacymodel.DecodeURIMetadata(encoded)
		if decodeErr == nil {
			freshness := row.Freshness()
			if err := order.Put(stalenessOrderKey(freshness, hash), []byte{}); err != nil {
				return fmt.Errorf("record staleness order: %w", err)
			}
			if err := freshnessOf.Put(hash, []byte(freshness)); err != nil {
				return fmt.Errorf("record staleness freshness: %w", err)
			}
			recorded++

			return nil
		}
		corrupt = append(corrupt, append([]byte(nil), hash...))

		return nil
	}); err != nil {
		return fmt.Errorf("scan url metadata: %w", err)
	}

	for _, hash := range corrupt {
		if err := urls.Delete(hash); err != nil {
			return fmt.Errorf("drop corrupt url metadata: %w", err)
		}
	}

	if err := putLength(lengths, urlMetadataBucket, recorded); err != nil {
		return err
	}
	if err := putLength(lengths, stalenessOrderBucket, recorded); err != nil {
		return err
	}

	return putLength(lengths, stalenessFreshnessBucket, recorded)
}

func stalenessOrderKey(freshness string, hash []byte) []byte {
	var key bytes.Buffer
	key.WriteString(freshness)
	key.WriteByte(stalenessOrderSeparator)
	key.Write(hash)

	return key.Bytes()
}
