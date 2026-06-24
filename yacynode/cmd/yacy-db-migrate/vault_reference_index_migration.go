package main

import (
	"bytes"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

const (
	schemaReferences = "vault-3"

	postingBucket    = "rwi"
	wordsByURLBucket = "urlreferences_words"

	hashLength = 12
)

func buildReferenceIndex(tx *bolt.Tx, lengths *bolt.Bucket) error {
	words, err := tx.CreateBucketIfNotExists([]byte(wordsByURLBucket))
	if err != nil {
		return fmt.Errorf("create words by url bucket: %w", err)
	}

	postings := tx.Bucket([]byte(postingBucket))
	if postings == nil {
		return putLength(lengths, wordsByURLBucket, 0)
	}

	recorded := 0
	if err := postings.ForEach(func(key, _ []byte) error {
		if len(key) != 2*hashLength {
			return nil
		}
		if err := words.Put(wordByURLKey(key), []byte{}); err != nil {
			return fmt.Errorf("record word by url: %w", err)
		}
		recorded++

		return nil
	}); err != nil {
		return fmt.Errorf("scan rwi postings: %w", err)
	}

	return putLength(lengths, wordsByURLBucket, recorded)
}

func wordByURLKey(postingKey []byte) []byte {
	word, url := postingKey[:hashLength], postingKey[hashLength:]

	var key bytes.Buffer
	key.Write(url)
	key.Write(word)

	return key.Bytes()
}
