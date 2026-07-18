package frontiercheckpoint

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func (checkpoint *FrontierCheckpoint) Load(
	ctx context.Context,
	provenance []byte,
) (Snapshot, error) {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	err = checkpoint.readTransaction(ctx, func(transaction *bolt.Tx) error {
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		snapshot = snapshotFromRecord(record)
		if err := loadVisitedPages(buckets.visited, prefix, snapshot.Visited); err != nil {
			return err
		}
		if err := loadOutstandingPages(buckets, prefix, &snapshot); err != nil {
			return err
		}
		if err := loadHostStates(buckets.hosts, prefix, snapshot.HostStates); err != nil {
			return err
		}
		if err := loadSeedManifest(transaction, prefix, record, &snapshot); err != nil {
			return err
		}
		return validateSnapshot(snapshot)
	})
	return snapshot, err
}

func snapshotFromRecord(record runRecord) Snapshot {
	return Snapshot{
		Visited:       make(map[string]struct{}),
		Counters:      Counters{Pages: record.Pages, Pending: record.Pending},
		HostStates:    make(map[string]HostState),
		OrderIdentity: append([]byte(nil), record.OrderIdentity...),
		Priority:      record.Priority,
		Failed:        record.Failed,
		Seeding:       record.Seeding,
		Completed:     record.Completed,
		Tally:         record.Tally,
		SeedManifest:  record.SeedManifest,
		SeedCursor:    record.SeedCursor,
		SeedLength:    record.SeedLength,
		Control: RunControl{
			Paused:         record.Paused,
			Cancelled:      record.Cancelled,
			PagesPerMinute: clonePagesPerMinute(record.PagesPerMinute),
		},
	}
}

func loadVisitedPages(bucket *bolt.Bucket, prefix []byte, visited map[string]struct{}) error {
	cursor := bucket.Cursor()
	for key, value := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, value = cursor.Next() {
		if len(key) == len(prefix) || !bytes.Equal(value, visitedMarker) {
			return fmt.Errorf("%w: invalid visited row", ErrCorruptCheckpoint)
		}
		visited[string(key[len(prefix):])] = struct{}{}
	}
	return nil
}

func loadOutstandingPages(
	buckets checkpointBuckets,
	prefix []byte,
	snapshot *Snapshot,
) error {
	cursor := buckets.pages.Cursor()
	redirectOwners := make(map[string]string)
	for key, encoded := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, encoded = cursor.Next() {
		if len(key) != len(prefix)+8 {
			return fmt.Errorf("%w: invalid outstanding page key", ErrCorruptCheckpoint)
		}
		var page Page
		if err := decodeRow("page", encoded, &page); err != nil {
			return err
		}
		if err := validatePages([]Page{page}); err != nil {
			return fmt.Errorf("%w: persisted page is invalid", ErrCorruptCheckpoint)
		}
		sequence := binary.BigEndian.Uint64(key[len(prefix):])
		if err := validatePageIndexes(
			buckets,
			prefix,
			sequence,
			page,
			snapshot.Visited,
		); err != nil {
			return err
		}
		if err := validateRedirectAssociation(
			buckets,
			prefix,
			page,
			snapshot.Visited,
			redirectOwners,
		); err != nil {
			return err
		}
		snapshot.Outstanding = append(snapshot.Outstanding, page)
	}
	return nil
}

func validateRedirectAssociation(
	buckets checkpointBuckets,
	prefix []byte,
	page Page,
	visited map[string]struct{},
	owners map[string]string,
) error {
	if page.RedirectURL == "" {
		if page.RedirectHost != "" || page.RedirectHostBump {
			return fmt.Errorf("%w: redirect ownership has no target", ErrCorruptCheckpoint)
		}

		return nil
	}
	if page.RedirectHost == "" ||
		page.RedirectHostBump != (page.RedirectHost != page.Host) {
		return fmt.Errorf("%w: redirect ownership is invalid", ErrCorruptCheckpoint)
	}
	if page.RedirectURL == page.URL {
		return fmt.Errorf("%w: redirect target equals its source", ErrCorruptCheckpoint)
	}
	if _, found := visited[page.RedirectURL]; !found {
		return fmt.Errorf("%w: redirect target was not reserved", ErrCorruptCheckpoint)
	}
	if owner, found := owners[page.RedirectURL]; found && owner != page.URL {
		return fmt.Errorf("%w: redirect target has multiple sources", ErrCorruptCheckpoint)
	}
	if buckets.pagePositions.Get(childRowKey(prefix, page.RedirectURL)) != nil {
		return fmt.Errorf("%w: redirect target is also outstanding", ErrCorruptCheckpoint)
	}
	owners[page.RedirectURL] = page.URL
	return nil
}

func validatePageIndexes(
	buckets checkpointBuckets,
	prefix []byte,
	sequence uint64,
	page Page,
	visited map[string]struct{},
) error {
	position := buckets.pagePositions.Get(childRowKey(prefix, page.URL))
	if len(position) != 8 || binary.BigEndian.Uint64(position) != sequence {
		return fmt.Errorf("%w: page position mismatch", ErrCorruptCheckpoint)
	}
	if _, found := visited[page.URL]; !found {
		return fmt.Errorf("%w: outstanding page was not visited", ErrCorruptCheckpoint)
	}
	return nil
}

func loadHostStates(bucket *bolt.Bucket, prefix []byte, states map[string]HostState) error {
	cursor := bucket.Cursor()
	for key, encoded := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, encoded = cursor.Next() {
		if len(key) == len(prefix) {
			return fmt.Errorf("%w: empty host row", ErrCorruptCheckpoint)
		}
		var record hostRecord
		if err := decodeRow("host state", encoded, &record); err != nil {
			return err
		}
		states[string(key[len(prefix):])] = HostState{
			Pages:      record.Pages,
			Failures:   record.Failures,
			Retired:    record.Retired,
			Generation: record.Generation,
		}
	}
	return nil
}

func validateSnapshot(snapshot Snapshot) error {
	if snapshot.Counters.Pending != uint64(len(snapshot.Outstanding)) {
		return fmt.Errorf(
			"%w: pending total does not match outstanding pages",
			ErrCorruptCheckpoint,
		)
	}
	if snapshot.Counters.Pages < snapshot.Counters.Pending {
		return fmt.Errorf("%w: page total is smaller than pending total", ErrCorruptCheckpoint)
	}
	if snapshot.SeedCursor > snapshot.SeedLength ||
		snapshot.SeedLength != uint64(len(snapshot.SeedPages)) ||
		(!snapshot.SeedManifest && (snapshot.SeedCursor != 0 || len(snapshot.SeedPages) != 0)) {
		return fmt.Errorf("%w: seed manifest state is inconsistent", ErrCorruptCheckpoint)
	}
	if !snapshot.Seeding && snapshot.SeedManifest {
		return fmt.Errorf("%w: completed seeding retains its manifest", ErrCorruptCheckpoint)
	}
	expectedCompletion := !snapshot.Seeding && snapshot.Counters.Pending == 0
	if snapshot.Completed != expectedCompletion {
		return fmt.Errorf("%w: completion marker is inconsistent", ErrCorruptCheckpoint)
	}
	return nil
}

func loadSeedManifest(
	transaction *bolt.Tx,
	prefix []byte,
	record runRecord,
	snapshot *Snapshot,
) error {
	bucket, err := schemaBucket(transaction, seedManifestBucket)
	if err != nil {
		return err
	}
	cursor := bucket.Cursor()
	key, _ := cursor.Seek(prefix)
	if !record.SeedManifest {
		if key != nil && bytes.HasPrefix(key, prefix) {
			return fmt.Errorf("%w: seed manifest rows lack a marker", ErrCorruptCheckpoint)
		}
		if record.SeedLength != 0 || record.SeedCursor != 0 {
			return fmt.Errorf("%w: seed manifest metadata lacks a marker", ErrCorruptCheckpoint)
		}
		return nil
	}
	if record.SeedCursor > record.SeedLength {
		return fmt.Errorf("%w: seed manifest cursor exceeds its length", ErrCorruptCheckpoint)
	}
	if record.SeedLength > uint64(^uint(0)>>1) {
		return fmt.Errorf("%w: seed manifest exceeds platform capacity", ErrCorruptCheckpoint)
	}
	snapshot.SeedPages = make([]Page, 0, int(record.SeedLength))
	for position := uint64(1); position <= record.SeedLength; position++ {
		expectedKey := sequenceRowKey(prefix, position)
		if key == nil || !bytes.Equal(key, expectedKey) {
			return fmt.Errorf("%w: seed manifest order is incomplete", ErrCorruptCheckpoint)
		}
		page, err := readSeedManifestPage(bucket, prefix, position)
		if err != nil {
			return err
		}
		snapshot.SeedPages = append(snapshot.SeedPages, page)
		key, _ = cursor.Next()
	}
	if key != nil && bytes.HasPrefix(key, prefix) {
		return fmt.Errorf("%w: seed manifest has excess rows", ErrCorruptCheckpoint)
	}
	return nil
}
