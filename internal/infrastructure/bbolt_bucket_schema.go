package infrastructure

import (
	"fmt"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketRWI            = []byte("rwi")
	bucketReferencedURLs = []byte("referenced_urls")
	bucketURLs           = []byte("urls")
	bucketCounts         = []byte("counts")
	countRWI             = []byte("rwi")
	countReferencedURLs  = []byte("referenced_urls")
	countURLs            = []byte("urls")
	setMember            = []byte{1}
)

func (s *BboltStorage) ensureBuckets() error {
	return s.update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bucketRWI, bucketReferencedURLs, bucketURLs, bucketCounts} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return fmt.Errorf("create bucket %s: %w", name, err)
			}
		}

		counts := tx.Bucket(bucketCounts)
		for _, key := range [][]byte{countRWI, countReferencedURLs, countURLs} {
			if counts.Get(key) == nil {
				if err := putCount(counts, key, 0); err != nil {
					return err
				}
			}
		}

		return nil
	})
}
