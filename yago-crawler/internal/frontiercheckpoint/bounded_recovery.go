package frontiercheckpoint

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	bolt "go.etcd.io/bbolt"
)

const RecoveryPageBatchSize = 256

type recoveryPageRead struct {
	buckets     checkpointBuckets
	prefix      []byte
	after       uint64
	upper       uint64
	limit       int
	dropRetired bool
	record      *runRecord
}

func (checkpoint *FrontierCheckpoint) LoadBounded(
	ctx context.Context,
	provenance []byte,
	limit int,
) (Snapshot, error) {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return Snapshot{}, err
	}
	if limit <= 0 || limit > RecoveryPageBatchSize {
		return Snapshot{}, ErrInvalidPage
	}
	var snapshot Snapshot
	err = checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		snapshot = Snapshot{}
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		if err := validateBoundedRunRecord(transaction, prefix, record); err != nil {
			return err
		}
		batch, err := readRecoveryPageBatch(recoveryPageRead{
			buckets:     buckets,
			prefix:      prefix,
			upper:       record.NextSequence,
			limit:       limit,
			dropRetired: true,
			record:      &record,
		})
		if err != nil {
			return err
		}
		if batch.Complete && uint64(len(batch.Pages)) != record.Pending {
			return fmt.Errorf(
				"%w: pending total does not match outstanding pages",
				ErrCorruptCheckpoint,
			)
		}
		if batch.RetiredPages > 0 {
			markCompletion(&record, buckets.pages, prefix)
			err = writeRunRecord(transaction, provenance, record)
		}
		snapshot = snapshotFromRecord(record)
		snapshot.Visited = nil
		snapshot.HostStates = batch.HostStates
		snapshot.Outstanding = batch.Pages
		snapshot.RecoveryBounded = true
		snapshot.RecoveryCursor = batch.Cursor
		snapshot.RecoveryUpper = record.NextSequence
		snapshot.RecoveryComplete = batch.Complete
		snapshot.SeedLength = record.SeedLength

		return err
	})

	return snapshot, err
}

func (checkpoint *FrontierCheckpoint) LoadRecoveryPageBatch(
	ctx context.Context,
	provenance []byte,
	after uint64,
	upper uint64,
	limit int,
) (RecoveryPageBatch, error) {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return RecoveryPageBatch{}, err
	}
	if limit <= 0 || limit > RecoveryPageBatchSize || after > upper {
		return RecoveryPageBatch{}, ErrInvalidPage
	}
	var batch RecoveryPageBatch
	err = checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		batch = RecoveryPageBatch{}
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if upper > record.NextSequence {
			return fmt.Errorf("%w: recovery boundary exceeds run sequence", ErrCorruptCheckpoint)
		}
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		batch, err = readRecoveryPageBatch(recoveryPageRead{
			buckets:     buckets,
			prefix:      prefix,
			after:       after,
			upper:       upper,
			limit:       limit,
			dropRetired: true,
			record:      &record,
		})
		if err != nil {
			return err
		}
		if batch.RetiredPages == 0 {
			return nil
		}
		markCompletion(&record, buckets.pages, prefix)

		return writeRunRecord(transaction, provenance, record)
	})

	return batch, err
}

func (checkpoint *FrontierCheckpoint) LoadSeedPageBatch(
	ctx context.Context,
	provenance []byte,
	cursor uint64,
	limit int,
) ([]Page, uint64, bool, error) {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return nil, 0, false, err
	}
	if limit <= 0 || limit > SeedAdmissionBatchSize {
		return nil, 0, false, ErrInvalidSeedBatch
	}
	var pages []Page
	var next uint64
	complete := false
	err = checkpoint.readTransaction(ctx, func(transaction *bolt.Tx) error {
		var readErr error
		pages, next, complete, readErr = readSeedPageBatch(
			transaction,
			provenance,
			prefix,
			cursor,
			limit,
		)

		return readErr
	})

	return pages, next, complete, err
}

func readSeedPageBatch(
	transaction *bolt.Tx,
	provenance []byte,
	prefix []byte,
	cursor uint64,
	limit int,
) ([]Page, uint64, bool, error) {
	record, err := requiredRunRecord(transaction, provenance)
	if err != nil {
		return nil, 0, false, err
	}
	if !record.Seeding || !record.SeedManifest || cursor != record.SeedCursor ||
		cursor > record.SeedLength {
		return nil, 0, false, ErrInvalidSeedBatch
	}
	manifest, err := schemaBucket(transaction, seedManifestBucket)
	if err != nil {
		return nil, 0, false, err
	}
	pages := make([]Page, 0, limit)
	next := cursor
	for next < record.SeedLength && len(pages) < limit {
		next++
		page, err := readSeedManifestPage(manifest, prefix, next)
		if err != nil {
			return nil, 0, false, err
		}
		pages = append(pages, page)
	}
	complete := next == record.SeedLength
	if err := validateSeedPageBatchEnd(manifest, prefix, record.SeedLength, complete); err != nil {
		return nil, 0, false, err
	}

	return pages, next, complete, nil
}

func validateSeedPageBatchEnd(
	manifest *bolt.Bucket,
	prefix []byte,
	seedLength uint64,
	complete bool,
) error {
	if !complete {
		return nil
	}
	key, _ := manifest.Cursor().Seek(sequenceRowKey(prefix, seedLength+1))
	if key != nil && bytes.HasPrefix(key, prefix) {
		return fmt.Errorf("%w: seed manifest has excess rows", ErrCorruptCheckpoint)
	}

	return nil
}

func (checkpoint *FrontierCheckpoint) AdmissionBatchState(
	ctx context.Context,
	provenance []byte,
	pages []Page,
) (AdmissionBatchState, error) {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return AdmissionBatchState{}, err
	}
	if len(pages) == 0 || len(pages) > RecoveryPageBatchSize {
		return AdmissionBatchState{}, ErrInvalidPage
	}
	if err := validatePages(pages); err != nil {
		return AdmissionBatchState{}, err
	}
	state := AdmissionBatchState{
		Visited:    make([]bool, len(pages)),
		HostStates: make(map[string]HostState),
	}
	err = checkpoint.readTransaction(ctx, func(transaction *bolt.Tx) error {
		if _, err := requiredRunRecord(transaction, provenance); err != nil {
			return err
		}
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		for index, page := range pages {
			marker := buckets.visited.Get(childRowKey(prefix, page.URL))
			if marker != nil && !bytes.Equal(marker, visitedMarker) {
				return fmt.Errorf("%w: invalid visited row", ErrCorruptCheckpoint)
			}
			state.Visited[index] = marker != nil
			if _, loaded := state.HostStates[page.Host]; loaded {
				continue
			}
			host, err := readHostState(buckets.hosts, prefix, page.Host)
			if err != nil {
				return err
			}
			state.HostStates[page.Host] = host
		}

		return nil
	})

	return state, err
}

func (checkpoint *FrontierCheckpoint) CancelRecoveryPages(
	ctx context.Context,
	provenance []byte,
	after uint64,
	upper uint64,
) (uint64, error) {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return 0, err
	}
	if after > upper {
		return 0, ErrInvalidPage
	}
	removed := uint64(0)
	cursor := after
	for cursor < upper {
		chunkRemoved, next, complete, err := checkpoint.cancelRecoveryPageBatch(
			ctx,
			provenance,
			prefix,
			cursor,
			upper,
		)
		if err != nil {
			return 0, err
		}
		removed += chunkRemoved
		cursor = next
		if complete {
			break
		}
	}

	return removed, nil
}

func (checkpoint *FrontierCheckpoint) cancelRecoveryPageBatch(
	ctx context.Context,
	provenance []byte,
	prefix []byte,
	after uint64,
	upper uint64,
) (uint64, uint64, bool, error) {
	removed := uint64(0)
	next := after
	complete := false
	err := checkpoint.boundedWriteTransaction(ctx, func(transaction *bolt.Tx) error {
		removed = 0
		next = after
		complete = false
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if !record.Cancelled || upper > record.NextSequence {
			return fmt.Errorf("%w: recovery cancellation state is invalid", ErrCorruptCheckpoint)
		}
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		pageURLs, cursor, atEnd, err := recoveryPageURLs(
			buckets.pages,
			prefix,
			after,
			upper,
			RecoveryPageBatchSize,
		)
		if err != nil {
			return err
		}
		removed, err = removeHostPages(buckets, prefix, "", pageURLs)
		if removed > record.Pending {
			return fmt.Errorf(
				"%w: cancelled recovery pages exceed pending total",
				ErrCorruptCheckpoint,
			)
		}
		record.Pending -= removed
		next = cursor
		complete = atEnd
		markCompletion(&record, buckets.pages, prefix)

		return errors.Join(err, writeRunRecord(transaction, provenance, record))
	})

	return removed, next, complete, err
}

func validateBoundedRunRecord(
	transaction *bolt.Tx,
	prefix []byte,
	record runRecord,
) error {
	if record.Pages < record.Pending {
		return fmt.Errorf("%w: page total is smaller than pending total", ErrCorruptCheckpoint)
	}
	if err := validateBoundedSeedRecord(transaction, prefix, record); err != nil {
		return err
	}
	expectedCompletion := !record.Seeding && record.Pending == 0
	if record.Completed != expectedCompletion {
		return fmt.Errorf("%w: completion marker is inconsistent", ErrCorruptCheckpoint)
	}

	return nil
}

func validateBoundedSeedRecord(
	transaction *bolt.Tx,
	prefix []byte,
	record runRecord,
) error {
	if record.SeedCursor > record.SeedLength ||
		(!record.SeedManifest && (record.SeedCursor != 0 || record.SeedLength != 0)) {
		return fmt.Errorf("%w: seed manifest state is inconsistent", ErrCorruptCheckpoint)
	}
	if !record.Seeding && record.SeedManifest {
		return fmt.Errorf("%w: completed seeding retains its manifest", ErrCorruptCheckpoint)
	}
	if !record.SeedManifest {
		return nil
	}
	manifest, err := schemaBucket(transaction, seedManifestBucket)
	if err != nil {
		return err
	}
	if record.SeedLength == 0 {
		return validateEmptySeedManifest(manifest, prefix)
	}
	if _, err := readSeedManifestPage(manifest, prefix, 1); err != nil {
		return err
	}
	_, err = readSeedManifestPage(manifest, prefix, record.SeedLength)

	return err
}

func validateEmptySeedManifest(manifest *bolt.Bucket, prefix []byte) error {
	key, _ := manifest.Cursor().Seek(prefix)
	if key != nil && bytes.HasPrefix(key, prefix) {
		return fmt.Errorf("%w: empty seed manifest has rows", ErrCorruptCheckpoint)
	}

	return nil
}

func readRecoveryPageBatch(read recoveryPageRead) (RecoveryPageBatch, error) {
	batch := RecoveryPageBatch{
		HostStates: make(map[string]HostState),
		Cursor:     read.after,
	}
	if read.after == read.upper {
		batch.Complete = true

		return batch, nil
	}
	if read.after == math.MaxUint64 {
		return RecoveryPageBatch{}, fmt.Errorf("%w: recovery cursor overflow", ErrCorruptCheckpoint)
	}
	retiredURLs, nextKey, err := scanRecoveryPageBatch(read, &batch)
	if err != nil {
		return RecoveryPageBatch{}, err
	}
	finishRecoveryPageBatch(&batch, nextKey, read.prefix, read.upper)
	if err := dropRetiredRecoveryPages(
		read.buckets,
		read.prefix,
		retiredURLs,
		read.record,
		&batch,
	); err != nil {
		return RecoveryPageBatch{}, err
	}

	return batch, nil
}

func scanRecoveryPageBatch(
	read recoveryPageRead,
	batch *RecoveryPageBatch,
) ([]string, []byte, error) {
	retiredURLs := make([]string, 0, read.limit)
	cursor := read.buckets.pages.Cursor()
	key, encoded := cursor.Seek(sequenceRowKey(read.prefix, read.after+1))
	scanned := 0
	for key != nil && bytes.HasPrefix(key, read.prefix) && scanned < read.limit {
		sequence, page, host, err := decodeRecoveryPageEntry(
			read.buckets,
			read.prefix,
			key,
			encoded,
			batch,
		)
		if err != nil {
			return nil, nil, err
		}
		if sequence > read.upper {
			break
		}
		batch.Cursor = sequence
		scanned++
		key, encoded = cursor.Next()
		if read.dropRetired && host.Retired {
			retiredURLs = append(retiredURLs, page.URL)

			continue
		}
		batch.Pages = append(batch.Pages, page)
	}

	return retiredURLs, bytes.Clone(key), nil
}

func decodeRecoveryPageEntry(
	buckets checkpointBuckets,
	prefix []byte,
	key []byte,
	encoded []byte,
	batch *RecoveryPageBatch,
) (uint64, Page, HostState, error) {
	if len(key) != len(prefix)+8 {
		return 0, Page{}, HostState{}, fmt.Errorf(
			"%w: invalid outstanding page key",
			ErrCorruptCheckpoint,
		)
	}
	sequence := binary.BigEndian.Uint64(key[len(prefix):])
	var page Page
	if err := decodeRow("page", encoded, &page); err != nil {
		return 0, Page{}, HostState{}, err
	}
	if err := validateRecoveryPage(buckets, prefix, sequence, page); err != nil {
		return 0, Page{}, HostState{}, err
	}
	host, err := loadRecoveryHostState(buckets.hosts, prefix, page.Host, batch.HostStates)
	if err != nil {
		return 0, Page{}, HostState{}, err
	}
	if page.RedirectHost != "" {
		if _, err := loadRecoveryHostState(
			buckets.hosts,
			prefix,
			page.RedirectHost,
			batch.HostStates,
		); err != nil {
			return 0, Page{}, HostState{}, err
		}
	}

	return sequence, page, host, nil
}

func finishRecoveryPageBatch(
	batch *RecoveryPageBatch,
	nextKey []byte,
	prefix []byte,
	upper uint64,
) {
	batch.Complete = nextKey == nil || !bytes.HasPrefix(nextKey, prefix)
	if !batch.Complete && len(nextKey) == len(prefix)+8 {
		batch.Complete = binary.BigEndian.Uint64(nextKey[len(prefix):]) > upper
	}
	if batch.Complete {
		batch.Cursor = upper
	}
}

func dropRetiredRecoveryPages(
	buckets checkpointBuckets,
	prefix []byte,
	retiredURLs []string,
	record *runRecord,
	batch *RecoveryPageBatch,
) error {
	if len(retiredURLs) == 0 {
		return nil
	}
	removed, err := removeHostPages(buckets, prefix, "", retiredURLs)
	if err != nil {
		return err
	}
	if removed > record.Pending || !retiredPageTotalMatches(removed, retiredURLs) {
		return fmt.Errorf(
			"%w: retired recovery page total is invalid",
			ErrCorruptCheckpoint,
		)
	}
	record.Pending -= removed
	batch.RetiredPages = removed

	return nil
}

func retiredPageTotalMatches(removed uint64, retiredURLs []string) bool {
	for range retiredURLs {
		if removed == 0 {
			return false
		}
		removed--
	}

	return removed == 0
}

func validateRecoveryPage(
	buckets checkpointBuckets,
	prefix []byte,
	sequence uint64,
	page Page,
) error {
	if err := validatePages([]Page{page}); err != nil {
		return fmt.Errorf("%w: persisted page is invalid", ErrCorruptCheckpoint)
	}
	position := buckets.pagePositions.Get(childRowKey(prefix, page.URL))
	if len(position) != 8 || binary.BigEndian.Uint64(position) != sequence {
		return fmt.Errorf("%w: page position mismatch", ErrCorruptCheckpoint)
	}
	if !bytes.Equal(buckets.visited.Get(childRowKey(prefix, page.URL)), visitedMarker) {
		return fmt.Errorf("%w: outstanding page was not visited", ErrCorruptCheckpoint)
	}
	if page.RedirectURL == "" {
		if page.RedirectHost != "" || page.RedirectHostBump {
			return fmt.Errorf("%w: redirect ownership has no target", ErrCorruptCheckpoint)
		}

		return nil
	}
	if page.RedirectHost == "" || page.RedirectURL == page.URL ||
		page.RedirectHostBump != (page.RedirectHost != page.Host) {
		return fmt.Errorf("%w: redirect ownership is invalid", ErrCorruptCheckpoint)
	}
	if !bytes.Equal(buckets.visited.Get(childRowKey(prefix, page.RedirectURL)), visitedMarker) {
		return fmt.Errorf("%w: redirect target was not reserved", ErrCorruptCheckpoint)
	}
	if buckets.pagePositions.Get(childRowKey(prefix, page.RedirectURL)) != nil {
		return fmt.Errorf("%w: redirect target is also outstanding", ErrCorruptCheckpoint)
	}

	return nil
}

func loadRecoveryHostState(
	bucket *bolt.Bucket,
	prefix []byte,
	host string,
	states map[string]HostState,
) (HostState, error) {
	if state, loaded := states[host]; loaded {
		return state, nil
	}
	state, err := readHostState(bucket, prefix, host)
	if err != nil {
		return HostState{}, err
	}
	states[host] = state

	return state, nil
}

func readHostState(bucket *bolt.Bucket, prefix []byte, host string) (HostState, error) {
	record, err := readHostRecord(bucket, prefix, host)
	if err != nil {
		return HostState{}, err
	}

	return HostState{
		Pages:      record.Pages,
		Failures:   record.Failures,
		Retired:    record.Retired,
		Generation: record.Generation,
	}, nil
}

func recoveryPageURLs(
	bucket *bolt.Bucket,
	prefix []byte,
	after uint64,
	upper uint64,
	limit int,
) ([]string, uint64, bool, error) {
	if after == upper {
		return nil, upper, true, nil
	}
	if after == math.MaxUint64 {
		return nil, 0, false, fmt.Errorf("%w: recovery cursor overflow", ErrCorruptCheckpoint)
	}
	cursor := bucket.Cursor()
	key, encoded := cursor.Seek(sequenceRowKey(prefix, after+1))
	pageURLs := make([]string, 0, limit)
	last := after
	for key != nil && bytes.HasPrefix(key, prefix) && len(pageURLs) < limit {
		if len(key) != len(prefix)+8 {
			return nil, 0, false, fmt.Errorf(
				"%w: invalid outstanding page key",
				ErrCorruptCheckpoint,
			)
		}
		sequence := binary.BigEndian.Uint64(key[len(prefix):])
		if sequence > upper {
			break
		}
		var page Page
		if err := decodeRow("page", encoded, &page); err != nil {
			return nil, 0, false, err
		}
		if err := validatePages([]Page{page}); err != nil {
			return nil, 0, false, fmt.Errorf("%w: persisted page is invalid", ErrCorruptCheckpoint)
		}
		pageURLs = append(pageURLs, page.URL)
		last = sequence
		key, encoded = cursor.Next()
	}
	complete := key == nil || !bytes.HasPrefix(key, prefix)
	if !complete && len(key) == len(prefix)+8 &&
		binary.BigEndian.Uint64(key[len(prefix):]) > upper {
		complete = true
	}
	if complete {
		last = upper
	}

	return pageURLs, last, complete, nil
}
