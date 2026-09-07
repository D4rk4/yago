package frontiercheckpoint

import (
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func setHostState(
	bucket *bolt.Bucket,
	prefix []byte,
	host string,
	progress HostProgress,
) (bool, error) {
	record, err := readHostRecord(bucket, prefix, host)
	if err != nil {
		return false, err
	}
	if progress.Generation < record.Generation {
		return false, nil
	}
	if progress.Generation == record.Generation && progress.Generation != 0 {
		if progress.Failures != record.Failures || progress.Retired != record.Retired {
			return false, fmt.Errorf(
				"%w: conflicting host outcome generation",
				ErrCorruptCheckpoint,
			)
		}
		return true, nil
	}
	if progress.Retired && (!record.Retired || progress.Generation != record.Generation) {
		record.RetirementCursor = 0
		record.RetirementScanned = false
	}
	if !progress.Retired {
		record.RetirementCursor = 0
		record.RetirementScanned = false
	}
	record.Failures = progress.Failures
	record.Retired = progress.Retired
	record.Generation = progress.Generation
	return true, writeHostRecord(bucket, prefix, host, record)
}

func removeHostPages(
	buckets checkpointBuckets,
	prefix []byte,
	host string,
	pageURLs []string,
) (uint64, error) {
	var removed uint64
	for _, pageURL := range pageURLs {
		pageRemoved, err := removeOutstandingPage(buckets, prefix, pageURL, host)
		if err != nil {
			return 0, err
		}
		if pageRemoved {
			removed++
		}
	}
	return removed, nil
}
