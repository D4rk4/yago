package contentcluster

import (
	"context"
	"fmt"
	"slices"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type postingProjection struct {
	collection *vault.Keyspace[postingRecord]
	key        vault.Key
	exact      bool
}

func (i *Index) prepareRecordPostings(
	tx *vault.Txn,
	ctx context.Context,
	record fingerprintRecord,
) error {
	projections := i.recordPostings(record)
	for _, projection := range projections {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("prepare content posting: %w", err)
		}
		posting, err := i.visiblePosting(tx, projection)
		if err != nil {
			return err
		}
		if _, found := slices.BinarySearch(posting.URLs, record.URL); !found &&
			len(posting.URLs) < i.limits.MaximumBucketMembers {
			posting.URLs = insertSorted(posting.URLs, record.URL)
		}
		if err := projection.collection.Put(tx, projection.key, posting); err != nil {
			return fmt.Errorf("store prepared content posting: %w", err)
		}
	}

	return nil
}

func (i *Index) finalizeRecordPostings(
	tx *vault.Txn,
	ctx context.Context,
	record fingerprintRecord,
) error {
	for _, projection := range i.recordPostings(record) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("finalize content posting: %w", err)
		}
		posting, err := i.visiblePosting(tx, projection)
		if err != nil {
			return err
		}
		posting.URLs = insertSorted(posting.URLs, record.URL)
		if len(posting.URLs) > i.limits.MaximumBucketMembers {
			posting.URLs = posting.URLs[:i.limits.MaximumBucketMembers]
		}
		if err := projection.collection.Put(tx, projection.key, posting); err != nil {
			return fmt.Errorf("store finalized content posting: %w", err)
		}
	}

	return nil
}

func (i *Index) removeRecordPostings(
	tx *vault.Txn,
	ctx context.Context,
	record fingerprintRecord,
) error {
	for _, projection := range i.recordPostings(record) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("remove content posting: %w", err)
		}
		posting, err := i.visiblePosting(tx, projection)
		if err != nil {
			return err
		}
		if len(posting.URLs) == 0 {
			if _, err := projection.collection.Delete(tx, projection.key); err != nil {
				return fmt.Errorf("delete empty content posting: %w", err)
			}
			continue
		}
		if err := projection.collection.Put(tx, projection.key, posting); err != nil {
			return fmt.Errorf("store cleaned content posting: %w", err)
		}
	}

	return nil
}

func (i *Index) visiblePosting(
	tx *vault.Txn,
	projection postingProjection,
) (postingRecord, error) {
	posting, found, err := projection.collection.Get(tx, projection.key)
	if err != nil || !found {
		if err != nil {
			return postingRecord{}, fmt.Errorf("read content posting projection: %w", err)
		}

		return postingRecord{}, nil
	}
	visible := make([]string, 0, len(posting.URLs))
	for _, url := range posting.URLs {
		record, found, err := i.projectedFingerprint(tx, url)
		if err != nil {
			return postingRecord{}, err
		}
		if found && postingMatches(record, projection) {
			visible = insertSorted(visible, url)
		}
	}
	if len(visible) > i.limits.MaximumBucketMembers {
		visible = visible[:i.limits.MaximumBucketMembers]
	}

	return postingRecord{URLs: visible}, nil
}

func (i *Index) recordPostings(record fingerprintRecord) []postingProjection {
	postings := make([]postingProjection, 0, bandCount+1)
	postings = append(postings, postingProjection{
		collection: i.exactBuckets,
		key:        vault.Key(record.ContentHash),
		exact:      true,
	})
	if len(record.Shingles) == 0 {
		return postings
	}
	for band, value := range fingerprintBands(record.Fingerprint) {
		postings = append(postings, postingProjection{
			collection: i.bandBuckets,
			key:        bandKey(uint8(band), value),
		})
	}

	return postings
}

func postingMatches(record fingerprintRecord, projection postingProjection) bool {
	if projection.exact {
		return record.ContentHash == string(projection.key)
	}
	if len(record.Shingles) == 0 || len(projection.key) != 2 {
		return false
	}
	bands := fingerprintBands(record.Fingerprint)
	band := int(projection.key[0])

	return band < len(bands) && bands[band] == projection.key[1]
}
