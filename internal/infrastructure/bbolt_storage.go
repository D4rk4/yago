package infrastructure

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
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
	errStorageClosed     = errors.New("storage closed")
)

type BboltStorage struct {
	db         *bolt.DB
	path       string
	quotaBytes int64
}

func OpenBboltStorage(path string, quotaBytes int64) (*BboltStorage, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open storage: %w", err)
	}

	store := &BboltStorage{db: db, path: path, quotaBytes: quotaBytes}
	if err := store.ensureBuckets(); err != nil {
		closeErr := db.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("initialize storage: %w: %w", err, closeErr)
		}

		return nil, fmt.Errorf("initialize storage: %w", err)
	}

	return store, nil
}

func (s *BboltStorage) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	err := s.db.Close()
	s.db = nil
	if err != nil {
		return fmt.Errorf("close storage: %w", err)
	}

	return nil
}

func (s *BboltStorage) AppendRWI(
	ctx context.Context,
	entries []yacymodel.RWIEntry,
) ([]yacymodel.Hash, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}
	if err := s.rejectAtCapacity(); err != nil {
		return nil, err
	}

	var rejected []yacymodel.Hash
	err := s.update(func(tx *bolt.Tx) error {
		rwi := tx.Bucket(bucketRWI)
		refs := tx.Bucket(bucketReferencedURLs)
		counts := tx.Bucket(bucketCounts)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return wrapContextErr(err)
			}

			urlHash, err := entry.URLHash()
			if err != nil || !entry.WordHash.Valid() {
				if err == nil {
					rejected = append(rejected, urlHash)
				}
				continue
			}

			key := rwiKey(entry.WordHash, urlHash)
			existing := rwi.Get(key)
			if existing == nil {
				if err := incrementCount(counts, countRWI); err != nil {
					return err
				}
			}
			if refs.Get([]byte(urlHash)) == nil {
				if err := refs.Put([]byte(urlHash), setMember); err != nil {
					return fmt.Errorf("store referenced url: %w", err)
				}
				if err := incrementCount(counts, countReferencedURLs); err != nil {
					return err
				}
			}
			if err := rwi.Put(key, []byte(entry.String())); err != nil {
				return fmt.Errorf("store rwi: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return rejected, nil
}

func (s *BboltStorage) RWICount(ctx context.Context) (int, error) {
	return s.count(ctx, countRWI)
}

func (s *BboltStorage) ReferencedURLCount(ctx context.Context) (int, error) {
	return s.count(ctx, countReferencedURLs)
}

func (s *BboltStorage) StoreURLs(
	ctx context.Context,
	rows []yacymodel.URIMetadataRow,
) (ports.StoreURLsResult, error) {
	if err := ctx.Err(); err != nil {
		return ports.StoreURLsResult{}, wrapContextErr(err)
	}
	if err := s.rejectAtCapacity(); err != nil {
		return ports.StoreURLsResult{}, err
	}

	var result ports.StoreURLsResult
	err := s.update(func(tx *bolt.Tx) error {
		urls := tx.Bucket(bucketURLs)
		counts := tx.Bucket(bucketCounts)
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return wrapContextErr(err)
			}

			hash, err := row.URLHash()
			if err != nil {
				continue
			}

			key := []byte(hash)
			if urls.Get(key) != nil {
				result.Existing = append(result.Existing, hash)
				continue
			}
			if err := urls.Put(key, []byte(row.String())); err != nil {
				result.Rejected = append(result.Rejected, hash)
				continue
			}
			if err := incrementCount(counts, countURLs); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return ports.StoreURLsResult{}, err
	}

	return result, nil
}

func (s *BboltStorage) MissingURLs(
	ctx context.Context,
	hashes []yacymodel.Hash,
) ([]yacymodel.Hash, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}

	missing := make([]yacymodel.Hash, 0)
	seen := make(map[yacymodel.Hash]struct{}, len(hashes))
	err := s.view(func(tx *bolt.Tx) error {
		urls := tx.Bucket(bucketURLs)
		for _, hash := range hashes {
			if err := ctx.Err(); err != nil {
				return wrapContextErr(err)
			}
			if _, ok := seen[hash]; ok {
				continue
			}
			seen[hash] = struct{}{}
			if urls.Get([]byte(hash)) == nil {
				missing = append(missing, hash)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return missing, nil
}

func (s *BboltStorage) RowsByHash(
	ctx context.Context,
	hashes []yacymodel.Hash,
) ([]yacymodel.URIMetadataRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}

	rows := make([]yacymodel.URIMetadataRow, 0, len(hashes))
	err := s.view(func(tx *bolt.Tx) error {
		urls := tx.Bucket(bucketURLs)
		for _, hash := range hashes {
			if err := ctx.Err(); err != nil {
				return wrapContextErr(err)
			}

			raw := urls.Get([]byte(hash))
			if raw == nil {
				continue
			}
			row, err := yacymodel.ParseURIMetadataRow(string(raw))
			if err != nil {
				return fmt.Errorf("parse url metadata: %w", err)
			}
			rows = append(rows, row)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return rows, nil
}

func (s *BboltStorage) URLCount(ctx context.Context) (int, error) {
	return s.count(ctx, countURLs)
}

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

func (s *BboltStorage) count(ctx context.Context, key []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, wrapContextErr(err)
	}

	var n int
	err := s.view(func(tx *bolt.Tx) error {
		value := tx.Bucket(bucketCounts).Get(key)
		count, err := decodeCount(value)
		if err != nil {
			return err
		}
		n = count

		return nil
	})
	if err != nil {
		return 0, err
	}

	return n, nil
}

func (s *BboltStorage) rejectAtCapacity() error {
	if s.quotaBytes <= 0 {
		return nil
	}

	info, err := os.Stat(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("stat storage: %w", err)
	}
	if info.Size() >= s.quotaBytes {
		return ports.ErrAtCapacity
	}

	return nil
}

func (s *BboltStorage) view(fn func(*bolt.Tx) error) error {
	if s == nil || s.db == nil {
		return wrapStorageError("read storage", errStorageClosed)
	}

	err := s.db.View(fn)
	if err != nil {
		return wrapStorageError("read storage", err)
	}

	return nil
}

func (s *BboltStorage) update(fn func(*bolt.Tx) error) error {
	if s == nil || s.db == nil {
		return wrapStorageError("write storage", errStorageClosed)
	}

	err := s.db.Update(fn)
	if err != nil {
		return wrapStorageError("write storage", err)
	}

	return nil
}

func rwiKey(wordHash yacymodel.Hash, urlHash yacymodel.Hash) []byte {
	key := make([]byte, 0, yacymodel.HashLength*2)
	key = append(key, wordHash.String()...)
	key = append(key, urlHash.String()...)

	return key
}

func incrementCount(bucket *bolt.Bucket, key []byte) error {
	n, err := decodeCount(bucket.Get(key))
	if err != nil {
		return err
	}

	return putCount(bucket, key, n+1)
}

func decodeCount(raw []byte) (int, error) {
	if raw == nil {
		return 0, nil
	}
	if len(raw) != 8 {
		return 0, fmt.Errorf("bad count length: %d", len(raw))
	}

	n := binary.BigEndian.Uint64(raw)
	if n > uint64(int(^uint(0)>>1)) {
		return 0, errors.New("count overflow")
	}

	return int(n), nil
}

func putCount(bucket *bolt.Bucket, key []byte, n int) error {
	value, err := countUint64(n)
	if err != nil {
		return err
	}

	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], value)
	if err := bucket.Put(key, raw[:]); err != nil {
		return fmt.Errorf("store count: %w", err)
	}

	return nil
}

func countUint64(n int) (uint64, error) {
	if n < 0 {
		return 0, errors.New("negative count")
	}

	value, err := strconv.ParseUint(strconv.Itoa(n), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("encode count: %w", err)
	}

	return value, nil
}

func wrapContextErr(err error) error {
	return fmt.Errorf("context: %w", err)
}

func wrapStorageError(operation string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if storageAtCapacityError(err) {
		return fmt.Errorf("%s: %w", operation, ports.ErrAtCapacity)
	}

	return fmt.Errorf("%s: %w", operation, ports.ErrStoreFailure)
}

func storageAtCapacityError(err error) bool {
	if errors.Is(err, syscall.ENOSPC) ||
		errors.Is(err, syscall.EDQUOT) ||
		errors.Is(err, syscall.EFBIG) {
		return true
	}

	message := strings.ToLower(err.Error())

	return strings.Contains(message, "no space left on device") ||
		strings.Contains(message, "disk quota exceeded") ||
		strings.Contains(message, "file too large") ||
		strings.Contains(message, "not enough space")
}

var (
	_ ports.RWIStore = (*BboltStorage)(nil)
	_ ports.URLStore = (*BboltStorage)(nil)
)
