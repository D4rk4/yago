package frontiercheckpoint

import (
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const SeedAdmissionBatchSize = 256

func (checkpoint *FrontierCheckpoint) BeginSeedManifest(
	ctx context.Context,
	provenance []byte,
	orderIdentity []byte,
	priority yagocrawlcontract.CrawlOrderPriority,
	pages []Page,
) error {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return err
	}
	if len(orderIdentity) == 0 {
		return ErrInvalidIdentity
	}
	if err := validatePages(pages); err != nil {
		return err
	}
	encodedPages, err := encodeSeedManifest(pages)
	if err != nil {
		return err
	}
	manifestLength := seedManifestPageTotal(encodedPages)

	return checkpoint.publishSeedManifest(ctx, seedManifestPublication{
		provenance:       provenance,
		prefix:           prefix,
		orderIdentity:    orderIdentity,
		priority:         priority,
		encodedPages:     encodedPages,
		manifestIdentity: identifySeedManifest(encodedPages),
		manifestLength:   manifestLength,
	})
}

func seedManifestPageTotal(encodedPages [][]byte) uint64 {
	return uint64(len(encodedPages))
}

func encodeSeedManifest(pages []Page) ([][]byte, error) {
	encodedPages := make([][]byte, 0, len(pages))
	for _, page := range pages {
		encoded, err := encodeRow("seed manifest page", page)
		if err != nil {
			return nil, err
		}
		encodedPages = append(encodedPages, encoded)
	}
	return encodedPages, nil
}

func (checkpoint *FrontierCheckpoint) AdmitSeedBatch(
	ctx context.Context,
	provenance []byte,
	batch SeedBatch,
) (SeedBatchResult, error) {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return SeedBatchResult{}, err
	}
	if len(batch.Decisions) == 0 || len(batch.Decisions) > SeedAdmissionBatchSize {
		return SeedBatchResult{}, ErrInvalidSeedBatch
	}
	pages := make([]Page, 0, len(batch.Decisions))
	for _, decision := range batch.Decisions {
		pages = append(pages, decision.Page)
	}
	if err := validatePages(pages); err != nil {
		return SeedBatchResult{}, err
	}
	var result SeedBatchResult
	err = checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		var admissionErr error
		result, admissionErr = admitSeedBatch(transaction, provenance, prefix, batch)

		return admissionErr
	})
	return result, err
}

func admitSeedBatch(
	transaction *bolt.Tx,
	provenance []byte,
	prefix []byte,
	batch SeedBatch,
) (SeedBatchResult, error) {
	record, err := requiredRunRecord(transaction, provenance)
	if err != nil {
		return SeedBatchResult{}, err
	}
	if record.Completed {
		return SeedBatchResult{}, ErrRunCompleted
	}
	if !record.Seeding || !record.SeedManifest {
		return SeedBatchResult{}, ErrSeedManifestMissing
	}
	if !validSeedBatchCursor(record, batch) {
		return SeedBatchResult{}, ErrInvalidSeedBatch
	}
	manifest, err := schemaBucket(transaction, seedManifestBucket)
	if err != nil {
		return SeedBatchResult{}, err
	}
	buckets, err := loadCheckpointBuckets(transaction)
	if err != nil {
		return SeedBatchResult{}, err
	}
	result, nextCursor, err := applySeedDecisions(manifest, buckets, prefix, &record, batch)
	if err != nil {
		return SeedBatchResult{}, err
	}
	record.SeedCursor = nextCursor
	record.Tally, err = addRunTally(
		record.Tally,
		yagocrawlcontract.CrawlRunTally{Duplicates: result.Duplicates},
	)
	if err != nil {
		return SeedBatchResult{}, err
	}

	return result, writeRunRecord(transaction, provenance, record)
}

func validSeedBatchCursor(record runRecord, batch SeedBatch) bool {
	if record.SeedCursor > record.SeedLength || batch.Cursor != record.SeedCursor {
		return false
	}
	remaining := record.SeedLength - record.SeedCursor
	for range batch.Decisions {
		if remaining == 0 {
			return false
		}
		remaining--
	}

	return true
}

func applySeedDecisions(
	manifest *bolt.Bucket,
	buckets checkpointBuckets,
	prefix []byte,
	record *runRecord,
	batch SeedBatch,
) (SeedBatchResult, uint64, error) {
	result := SeedBatchResult{}
	position := record.SeedCursor
	for _, decision := range batch.Decisions {
		position++
		if err := validateSeedDecisionPage(manifest, prefix, position, decision); err != nil {
			return SeedBatchResult{}, 0, err
		}
		if err := applySeedDecision(buckets, prefix, record, decision, &result); err != nil {
			return SeedBatchResult{}, 0, err
		}
	}

	return result, position, nil
}

func validateSeedDecisionPage(
	manifest *bolt.Bucket,
	prefix []byte,
	position uint64,
	decision SeedDecision,
) error {
	manifestPage, err := readSeedManifestPage(manifest, prefix, position)
	if err != nil {
		return err
	}
	if !samePage(manifestPage, decision.Page) {
		return fmt.Errorf("%w: seed manifest page changed", ErrCorruptCheckpoint)
	}

	return nil
}

func applySeedDecision(
	buckets checkpointBuckets,
	prefix []byte,
	record *runRecord,
	decision SeedDecision,
	result *SeedBatchResult,
) error {
	seen := buckets.visited.Get(childRowKey(prefix, decision.Page.URL)) != nil
	if !decision.Admit {
		if seen {
			result.Duplicates++
		}

		return nil
	}
	if seen {
		return fmt.Errorf("%w: admitted seed was already visited", ErrCorruptCheckpoint)
	}
	if _, err := admitPage(buckets, prefix, record, decision.Page); err != nil {
		return err
	}
	result.Admitted++

	return nil
}

func readSeedManifestPage(
	bucket *bolt.Bucket,
	prefix []byte,
	position uint64,
) (Page, error) {
	encoded := bucket.Get(sequenceRowKey(prefix, position))
	if encoded == nil {
		return Page{}, fmt.Errorf("%w: seed manifest page is missing", ErrCorruptCheckpoint)
	}
	var page Page
	if err := decodeRow("seed manifest page", encoded, &page); err != nil {
		return Page{}, err
	}
	if err := validatePages([]Page{page}); err != nil {
		return Page{}, fmt.Errorf(
			"%w: persisted seed manifest page is invalid",
			ErrCorruptCheckpoint,
		)
	}
	return page, nil
}

func samePage(left, right Page) bool {
	return left.URL == right.URL &&
		left.Host == right.Host &&
		left.Depth == right.Depth &&
		left.ProfileHandle == right.ProfileHandle &&
		left.ObservationID == right.ObservationID &&
		left.ObservedAt.Equal(right.ObservedAt) &&
		left.SourceModifiedAt.Equal(right.SourceModifiedAt) &&
		left.RedirectURL == right.RedirectURL &&
		left.RedirectHost == right.RedirectHost &&
		left.RedirectHostBump == right.RedirectHostBump &&
		left.Index == right.Index
}
