package frontiercheckpoint

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"

	bolt "go.etcd.io/bbolt"
)

const retirementPagesPerTransaction = 256

type retiredHostTransition struct {
	provenance []byte
	prefix     []byte
	host       string
}

type hostPageRemoval struct {
	provenance []byte
	prefix     []byte
	host       string
	generation uint64
	pageURLs   []string
}

func (checkpoint *FrontierCheckpoint) validateHostPages(
	ctx context.Context,
	provenance []byte,
	prefix []byte,
	host string,
	pageURLs []string,
) error {
	return checkpoint.readTransaction(ctx, func(transaction *bolt.Tx) error {
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		return validateHostPagesInTransaction(buckets, prefix, host, pageURLs, record.Pending)
	})
}

func validateHostPagesInTransaction(
	buckets checkpointBuckets,
	prefix []byte,
	host string,
	pageURLs []string,
	pending uint64,
) error {
	found := uint64(0)
	seen := make(map[string]struct{}, len(pageURLs))
	for _, pageURL := range pageURLs {
		if _, duplicate := seen[pageURL]; duplicate {
			continue
		}
		seen[pageURL] = struct{}{}
		row, present, err := findOutstandingPage(buckets, prefix, pageURL)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		if row.page.Host != host {
			return fmt.Errorf("%w: outstanding page identity mismatch", ErrCorruptCheckpoint)
		}
		found++
	}
	if found > pending {
		return fmt.Errorf("%w: dropped pages exceed pending total", ErrCorruptCheckpoint)
	}

	return nil
}

func (checkpoint *FrontierCheckpoint) removeHostPagesInChunks(
	ctx context.Context,
	removal hostPageRemoval,
) error {
	for start := 0; start < len(removal.pageURLs); start += retirementPagesPerTransaction {
		end := min(start+retirementPagesPerTransaction, len(removal.pageURLs))
		chunk := removal
		chunk.pageURLs = removal.pageURLs[start:end]
		current, err := checkpoint.removeHostPagesChunk(
			ctx,
			chunk,
		)
		if err != nil || !current {
			return err
		}
	}

	return nil
}

func (checkpoint *FrontierCheckpoint) removeHostPagesChunk(
	ctx context.Context,
	removal hostPageRemoval,
) (bool, error) {
	current := false
	err := checkpoint.boundedWriteTransaction(ctx, func(transaction *bolt.Tx) error {
		current = false
		record, err := requiredRunRecord(transaction, removal.provenance)
		if err != nil {
			return err
		}
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		hostState, err := readHostRecord(buckets.hosts, removal.prefix, removal.host)
		if err != nil {
			return err
		}
		if hostState.Generation != removal.generation || !hostState.Retired {
			return nil
		}
		current = true
		removed, err := removeHostPages(
			buckets,
			removal.prefix,
			removal.host,
			removal.pageURLs,
		)
		if err != nil {
			return err
		}
		if removed > record.Pending {
			return fmt.Errorf("%w: dropped pages exceed pending total", ErrCorruptCheckpoint)
		}
		record.Pending -= removed
		markCompletion(&record, buckets.pages, removal.prefix)

		return writeRunRecord(transaction, removal.provenance, record)
	})

	return current, err
}

func (checkpoint *FrontierCheckpoint) resumeRetiredHostTransitions(ctx context.Context) error {
	transitions, err := checkpoint.retiredHostTransitions(ctx)
	if err != nil {
		return err
	}
	for _, transition := range transitions {
		for {
			done, err := checkpoint.resumeRetiredHostTransitionChunk(
				ctx,
				transition.provenance,
				transition.prefix,
				transition.host,
			)
			if err != nil || done {
				if err != nil {
					return err
				}
				break
			}
		}
	}

	return nil
}

func (checkpoint *FrontierCheckpoint) retiredHostTransitions(
	ctx context.Context,
) ([]retiredHostTransition, error) {
	transitions := make([]retiredHostTransition, 0)
	err := checkpoint.readTransaction(ctx, func(transaction *bolt.Tx) error {
		var readErr error
		transitions, readErr = readRetiredHostTransitions(transaction)

		return readErr
	})

	return transitions, err
}

func readRetiredHostTransitions(transaction *bolt.Tx) ([]retiredHostTransition, error) {
	runs, err := schemaBucket(transaction, runsBucket)
	if err != nil {
		return nil, err
	}
	hosts, err := schemaBucket(transaction, hostsBucket)
	if err != nil {
		return nil, err
	}
	transitions := make([]retiredHostTransition, 0)
	err = runs.ForEach(func(provenance, encoded []byte) error {
		found, err := retiredHostTransitionsForRun(hosts, provenance, encoded)
		if err != nil {
			return err
		}
		transitions = append(transitions, found...)

		return nil
	})
	if err != nil {
		return nil, wrapDatabaseError("iterate frontier checkpoint runs", err)
	}

	return transitions, nil
}

func retiredHostTransitionsForRun(
	hosts *bolt.Bucket,
	provenance []byte,
	encoded []byte,
) ([]retiredHostTransition, error) {
	var record runRecord
	if err := decodeRow("run", encoded, &record); err != nil {
		return nil, err
	}
	if record.Completed || record.Cancelled || record.Deleting {
		return nil, nil
	}
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid run provenance", ErrCorruptCheckpoint)
	}
	transitions := make([]retiredHostTransition, 0)
	cursor := hosts.Cursor()
	for key, value := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, value = cursor.Next() {
		if len(key) == len(prefix) {
			return nil, fmt.Errorf("%w: empty host row", ErrCorruptCheckpoint)
		}
		var hostState hostRecord
		if err := decodeRow("host state", value, &hostState); err != nil {
			return nil, err
		}
		if hostState.Retired && !hostState.RetirementScanned {
			transitions = append(transitions, retiredHostTransition{
				provenance: bytes.Clone(provenance),
				prefix:     bytes.Clone(prefix),
				host:       string(key[len(prefix):]),
			})
		}
	}

	return transitions, nil
}

func (checkpoint *FrontierCheckpoint) resumeRetiredHostTransitionChunk(
	ctx context.Context,
	provenance []byte,
	prefix []byte,
	host string,
) (bool, error) {
	done := false
	err := checkpoint.boundedWriteTransaction(ctx, func(transaction *bolt.Tx) error {
		var transitionErr error
		done, transitionErr = resumeRetiredHostTransition(
			transaction,
			provenance,
			prefix,
			host,
		)

		return transitionErr
	})

	return done, err
}

func resumeRetiredHostTransition(
	transaction *bolt.Tx,
	provenance []byte,
	prefix []byte,
	host string,
) (bool, error) {
	record, found, err := readRunRecord(transaction, provenance)
	if err != nil {
		return false, err
	}
	if !found || record.Completed || record.Cancelled || record.Deleting {
		return true, nil
	}
	buckets, err := loadCheckpointBuckets(transaction)
	if err != nil {
		return false, err
	}
	hostState, err := readHostRecord(buckets.hosts, prefix, host)
	if err != nil {
		return false, err
	}
	if !hostState.Retired || hostState.RetirementScanned {
		return true, nil
	}
	if hostState.RetirementCursor > record.NextSequence {
		return false, fmt.Errorf(
			"%w: host retirement cursor exceeds run sequence",
			ErrCorruptCheckpoint,
		)
	}
	pageURLs, cursor, atEnd, err := retiredHostPageChunk(
		buckets.pages,
		prefix,
		host,
		hostState.RetirementCursor,
	)
	if err != nil {
		return false, err
	}
	removed, err := removeHostPages(buckets, prefix, host, pageURLs)
	if err != nil {
		return false, err
	}
	if removed > record.Pending {
		return false, fmt.Errorf(
			"%w: retired host pages exceed pending total",
			ErrCorruptCheckpoint,
		)
	}
	record.Pending -= removed
	hostState.RetirementCursor = cursor
	hostState.RetirementScanned = atEnd
	if err := writeHostRecord(buckets.hosts, prefix, host, hostState); err != nil {
		return false, err
	}
	markCompletion(&record, buckets.pages, prefix)

	return atEnd, writeRunRecord(transaction, provenance, record)
}

func retiredHostPageChunk(
	pages *bolt.Bucket,
	prefix []byte,
	host string,
	after uint64,
) ([]string, uint64, bool, error) {
	if after == math.MaxUint64 {
		return nil, after, true, nil
	}
	start := sequenceRowKey(prefix, after+1)
	cursor := pages.Cursor()
	key, encoded := cursor.Seek(start)
	pageURLs := make([]string, 0, retirementPagesPerTransaction)
	last := after
	scanned := 0
	for key != nil && bytes.HasPrefix(key, prefix) && scanned < retirementPagesPerTransaction {
		if len(key) != len(prefix)+8 {
			return nil, 0, false, fmt.Errorf(
				"%w: invalid outstanding page key",
				ErrCorruptCheckpoint,
			)
		}
		var page Page
		if err := decodeRow("page", encoded, &page); err != nil {
			return nil, 0, false, err
		}
		if err := validatePages([]Page{page}); err != nil {
			return nil, 0, false, fmt.Errorf("%w: persisted page is invalid", ErrCorruptCheckpoint)
		}
		last = binary.BigEndian.Uint64(key[len(prefix):])
		if page.Host == host {
			pageURLs = append(pageURLs, page.URL)
		}
		scanned++
		key, encoded = cursor.Next()
	}

	return pageURLs, last, key == nil || !bytes.HasPrefix(key, prefix), nil
}
