package frontiercheckpoint

import (
	"context"
	"errors"
	"fmt"
	"strings"

	bolt "go.etcd.io/bbolt"
)

type checkpointBuckets struct {
	visited       *bolt.Bucket
	pages         *bolt.Bucket
	pagePositions *bolt.Bucket
	hosts         *bolt.Bucket
}

func (checkpoint *FrontierCheckpoint) Admit(
	ctx context.Context,
	provenance []byte,
	pages []Page,
) (int, error) {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return 0, err
	}
	if err := validatePages(pages); err != nil {
		return 0, err
	}
	admitted := 0
	err = checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		admitted = 0
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if record.Completed {
			return ErrRunCompleted
		}
		if record.SeedManifest {
			return ErrInvalidSeedBatch
		}
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		for _, page := range pages {
			added, err := admitPage(buckets, prefix, &record, page)
			if err != nil {
				return err
			}
			if added {
				admitted++
			}
		}
		return writeRunRecord(transaction, provenance, record)
	})
	return admitted, err
}

func validatePages(pages []Page) error {
	for _, page := range pages {
		if strings.TrimSpace(page.URL) == "" || strings.TrimSpace(page.Host) == "" ||
			strings.TrimSpace(page.ObservationID) == "" || page.Depth < 0 {
			return fmt.Errorf(
				"%w: url, host, observation identity, and depth are required",
				ErrInvalidPage,
			)
		}
	}
	return nil
}

func loadCheckpointBuckets(transaction *bolt.Tx) (checkpointBuckets, error) {
	names := [][]byte{visitedBucket, pagesBucket, pagePositionsBucket, hostsBucket}
	loaded := make([]*bolt.Bucket, len(names))
	for index, name := range names {
		bucket, err := schemaBucket(transaction, name)
		if err != nil {
			return checkpointBuckets{}, err
		}
		loaded[index] = bucket
	}
	return checkpointBuckets{
		visited:       loaded[0],
		pages:         loaded[1],
		pagePositions: loaded[2],
		hosts:         loaded[3],
	}, nil
}

func admitPage(
	buckets checkpointBuckets,
	prefix []byte,
	record *runRecord,
	page Page,
) (bool, error) {
	visitedKey := childRowKey(prefix, page.URL)
	if buckets.visited.Get(visitedKey) != nil {
		return false, nil
	}
	sequence, pages, pending, err := advanceRun(record)
	if err != nil {
		return false, err
	}
	encoded, err := encodeRow("page", page)
	if err != nil {
		return false, err
	}
	if err := writeAdmittedPage(buckets, prefix, sequence, page, encoded); err != nil {
		return false, err
	}
	if err := incrementHostPages(buckets.hosts, prefix, page.Host); err != nil {
		return false, err
	}
	record.NextSequence = sequence
	record.Pages = pages
	record.Pending = pending
	record.Completed = false
	return true, nil
}

func advanceRun(record *runRecord) (uint64, uint64, uint64, error) {
	sequence, err := nextValue(record.NextSequence)
	if err != nil {
		return 0, 0, 0, err
	}
	pages, err := nextValue(record.Pages)
	if err != nil {
		return 0, 0, 0, err
	}
	pending, err := nextValue(record.Pending)
	if err != nil {
		return 0, 0, 0, err
	}
	return sequence, pages, pending, nil
}

func writeAdmittedPage(
	buckets checkpointBuckets,
	prefix []byte,
	sequence uint64,
	page Page,
	encoded []byte,
) error {
	return errors.Join(
		putRow(buckets.visited, childRowKey(prefix, page.URL), visitedMarker, "visited page"),
		putRow(buckets.pages, sequenceRowKey(prefix, sequence), encoded, "outstanding page"),
		putRow(
			buckets.pagePositions,
			childRowKey(prefix, page.URL),
			sequenceValue(sequence),
			"page position",
		),
	)
}

func incrementHostPages(bucket *bolt.Bucket, prefix []byte, host string) error {
	record, err := readHostRecord(bucket, prefix, host)
	if err != nil {
		return err
	}
	record.Pages, err = nextValue(record.Pages)
	if err != nil {
		return err
	}
	return writeHostRecord(bucket, prefix, host, record)
}

func decrementHostPages(bucket *bolt.Bucket, prefix []byte, host string) error {
	record, err := readHostRecord(bucket, prefix, host)
	if err != nil {
		return err
	}
	if record.Pages == 0 {
		return fmt.Errorf("%w: redirect host total is empty", ErrCorruptCheckpoint)
	}
	record.Pages--

	return writeHostRecord(bucket, prefix, host, record)
}

func readHostRecord(bucket *bolt.Bucket, prefix []byte, host string) (hostRecord, error) {
	encoded := bucket.Get(childRowKey(prefix, host))
	if encoded == nil {
		return hostRecord{}, nil
	}
	var record hostRecord
	if err := decodeRow("host state", encoded, &record); err != nil {
		return hostRecord{}, err
	}
	return record, nil
}

func writeHostRecord(bucket *bolt.Bucket, prefix []byte, host string, record hostRecord) error {
	encoded, err := encodeRow("host state", record)
	return errors.Join(err, putRow(bucket, childRowKey(prefix, host), encoded, "host state"))
}
