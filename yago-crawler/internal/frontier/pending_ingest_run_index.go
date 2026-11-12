package frontier

import (
	"encoding/binary"
	"errors"

	bolt "go.etcd.io/bbolt"
)

const pendingIngestRunCountKey = "count"

func addPendingIngestRunEntry(
	runs *bolt.Bucket,
	runID string,
	pageID string,
) error {
	run, err := runs.CreateBucketIfNotExists([]byte(runID))
	if err != nil {
		return err
	}
	if run.Get([]byte(pageID)) != nil {
		return nil
	}
	count, err := pendingIngestRunCount(run)
	if err != nil {
		return err
	}
	if err := run.Put([]byte(pageID), []byte{1}); err != nil {
		return err
	}
	return putPendingIngestRunCount(run, count+1)
}

func removePendingIngestRunEntry(
	runs *bolt.Bucket,
	runID string,
	pageID string,
) error {
	run := runs.Bucket([]byte(runID))
	if run == nil || run.Get([]byte(pageID)) == nil {
		return errors.New("pending crawler ingest run index is missing its page")
	}
	count, err := pendingIngestRunCount(run)
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("pending crawler ingest run count underflow")
	}
	if err := run.Delete([]byte(pageID)); err != nil {
		return err
	}
	if count == 1 {
		return runs.DeleteBucket([]byte(runID))
	}
	return putPendingIngestRunCount(run, count-1)
}

func pendingIngestRunCount(run *bolt.Bucket) (uint64, error) {
	raw := run.Get([]byte(pendingIngestRunCountKey))
	if raw == nil {
		return 0, nil
	}
	if len(raw) != 8 {
		return 0, errors.New("invalid pending crawler ingest run count")
	}
	return binary.BigEndian.Uint64(raw), nil
}

func putPendingIngestRunCount(run *bolt.Bucket, count uint64) error {
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, count)
	return run.Put([]byte(pendingIngestRunCountKey), encoded)
}
