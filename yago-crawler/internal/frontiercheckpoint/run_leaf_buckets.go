package frontiercheckpoint

import bolt "go.etcd.io/bbolt"

func runLeafBuckets(transaction *bolt.Tx) ([]*bolt.Bucket, error) {
	buckets, err := loadCheckpointBuckets(transaction)
	if err != nil {
		return nil, err
	}
	manifest, err := schemaBucket(transaction, seedManifestBucket)
	if err != nil {
		return nil, err
	}

	return []*bolt.Bucket{
		buckets.visited,
		buckets.pages,
		buckets.pagePositions,
		buckets.hosts,
		manifest,
	}, nil
}
