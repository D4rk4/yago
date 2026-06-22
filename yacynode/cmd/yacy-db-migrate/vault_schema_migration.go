package main

import (
	"encoding/binary"
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

const (
	lengthBucket = "__lengths__"
	schemaKey    = "__schema__"
	schemaVault  = "vault-1"

	legacyCountsBucket = "counts"
)

type bucketRename struct {
	from string
	to   string
}

var legacyBucketRenames = []bucketRename{
	{from: "referenced_urls", to: "rwi_refs"},
	{from: "urls", to: "urlmeta"},
}

var countedBuckets = []string{"rwi", "rwi_refs", "urlmeta"}

func migrate(db *bolt.DB) (bool, error) {
	migrated := false

	if err := db.Update(func(tx *bolt.Tx) error {
		lengths, err := tx.CreateBucketIfNotExists([]byte(lengthBucket))
		if err != nil {
			return fmt.Errorf("create length bucket: %w", err)
		}

		if string(lengths.Get([]byte(schemaKey))) == schemaVault {
			return nil
		}

		if err := relocateLegacySchema(tx, lengths); err != nil {
			return err
		}

		migrated = true

		return nil
	}); err != nil {
		return false, fmt.Errorf("upgrade database: %w", err)
	}

	return migrated, nil
}

func relocateLegacySchema(tx *bolt.Tx, lengths *bolt.Bucket) error {
	for _, rename := range legacyBucketRenames {
		if err := renameBucket(tx, rename.from, rename.to); err != nil {
			return err
		}
	}

	if err := rebuildLengths(tx, lengths); err != nil {
		return err
	}

	if tx.Bucket([]byte(legacyCountsBucket)) != nil {
		if err := tx.DeleteBucket([]byte(legacyCountsBucket)); err != nil {
			return fmt.Errorf("delete legacy counts bucket: %w", err)
		}
	}

	if err := lengths.Put([]byte(schemaKey), []byte(schemaVault)); err != nil {
		return fmt.Errorf("mark vault schema: %w", err)
	}

	return nil
}

func rebuildLengths(tx *bolt.Tx, lengths *bolt.Bucket) error {
	for _, name := range countedBuckets {
		keys, err := countKeys(tx, name)
		if err != nil {
			return err
		}
		if err := putLength(lengths, name, keys); err != nil {
			return err
		}
	}

	return nil
}

func countKeys(tx *bolt.Tx, name string) (int, error) {
	bucket := tx.Bucket([]byte(name))
	if bucket == nil {
		return 0, nil
	}

	keys := 0
	if err := bucket.ForEach(func([]byte, []byte) error {
		keys++

		return nil
	}); err != nil {
		return 0, fmt.Errorf("count bucket %s: %w", name, err)
	}

	return keys, nil
}

func renameBucket(tx *bolt.Tx, from, to string) error {
	source := tx.Bucket([]byte(from))
	if source == nil {
		return nil
	}

	target, err := tx.CreateBucketIfNotExists([]byte(to))
	if err != nil {
		return fmt.Errorf("create bucket %s: %w", to, err)
	}

	if err := source.ForEach(func(key, value []byte) error {
		return target.Put(key, value)
	}); err != nil {
		return fmt.Errorf("copy bucket %s to %s: %w", from, to, err)
	}

	if err := tx.DeleteBucket([]byte(from)); err != nil {
		return fmt.Errorf("delete bucket %s: %w", from, err)
	}

	return nil
}

func putLength(lengths *bolt.Bucket, name string, keys int) error {
	if keys < 0 {
		return errors.New("negative key count")
	}

	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], uint64(keys))
	if err := lengths.Put([]byte(name), raw[:]); err != nil {
		return fmt.Errorf("store length %s: %w", name, err)
	}

	return nil
}
