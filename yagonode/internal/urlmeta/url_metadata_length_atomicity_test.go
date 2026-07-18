package urlmeta

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type urlMetadataLengthEngine struct {
	buckets     map[vault.Name]map[string][]byte
	additions   map[vault.Name]int
	removals    map[vault.Name]int
	lengthError error
}

func newURLMetadataLengthEngine() *urlMetadataLengthEngine {
	return &urlMetadataLengthEngine{
		buckets:   map[vault.Name]map[string][]byte{},
		additions: map[vault.Name]int{},
		removals:  map[vault.Name]int{},
	}
}

func (e *urlMetadataLengthEngine) Update(
	ctx context.Context,
	fn func(vault.EngineTxn) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("update URL metadata length engine: %w", err)
	}
	tx := urlMetadataLengthTxn{
		engine:    e,
		buckets:   cloneURLMetadataLengthBuckets(e.buckets),
		additions: cloneURLMetadataLengths(e.additions),
		removals:  cloneURLMetadataLengths(e.removals),
		writable:  true,
	}
	if err := fn(tx); err != nil {
		return err
	}
	e.buckets = tx.buckets
	e.additions = tx.additions
	e.removals = tx.removals

	return nil
}

func (e *urlMetadataLengthEngine) View(
	ctx context.Context,
	fn func(vault.EngineTxn) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("view URL metadata length engine: %w", err)
	}

	return fn(urlMetadataLengthTxn{
		engine:    e,
		buckets:   e.buckets,
		additions: e.additions,
		removals:  e.removals,
	})
}

func (e *urlMetadataLengthEngine) Provision(name vault.Name) error {
	if e.buckets[name] == nil {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (*urlMetadataLengthEngine) UsedBytes(context.Context) (int64, error) {
	return 0, nil
}

func (*urlMetadataLengthEngine) QuotaBytes() int64 {
	return 0
}

func (*urlMetadataLengthEngine) Close() error {
	return nil
}

type urlMetadataLengthTxn struct {
	engine    *urlMetadataLengthEngine
	buckets   map[vault.Name]map[string][]byte
	additions map[vault.Name]int
	removals  map[vault.Name]int
	writable  bool
}

func (t urlMetadataLengthTxn) Bucket(name vault.Name) vault.EngineBucket {
	return urlMetadataLengthBucket{records: t.buckets[name]}
}

func (t urlMetadataLengthTxn) Writable() bool {
	return t.writable
}

func (t urlMetadataLengthTxn) RecordCollectionAddition(
	collection vault.Name,
	_ vault.Key,
) error {
	if t.engine.lengthError != nil {
		return t.engine.lengthError
	}
	t.additions[collection]++

	return nil
}

func (t urlMetadataLengthTxn) RecordCollectionRemoval(
	collection vault.Name,
	_ vault.Key,
) error {
	t.removals[collection]++

	return nil
}

func (t urlMetadataLengthTxn) CollectionLengthChanges(
	collection vault.Name,
) (int, int, error) {
	return t.additions[collection], t.removals[collection], nil
}

type urlMetadataLengthBucket struct {
	records map[string][]byte
}

func (b urlMetadataLengthBucket) Get(key vault.Key) []byte {
	return append([]byte(nil), b.records[string(key)]...)
}

func (b urlMetadataLengthBucket) Put(key vault.Key, value []byte) error {
	b.records[string(key)] = append([]byte(nil), value...)

	return nil
}

func (b urlMetadataLengthBucket) Delete(key vault.Key) error {
	delete(b.records, string(key))

	return nil
}

func (b urlMetadataLengthBucket) Scan(
	prefix vault.Key,
	fn func(vault.Key, []byte) (bool, error),
) error {
	keys := make([]string, 0, len(b.records))
	for key := range b.records {
		if bytes.HasPrefix([]byte(key), prefix) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		again, err := fn(vault.Key(key), append([]byte(nil), b.records[key]...))
		if err != nil {
			return err
		}
		if !again {
			return nil
		}
	}

	return nil
}

func cloneURLMetadataLengthBuckets(
	source map[vault.Name]map[string][]byte,
) map[vault.Name]map[string][]byte {
	cloned := make(map[vault.Name]map[string][]byte, len(source))
	for name, bucket := range source {
		cloned[name] = make(map[string][]byte, len(bucket))
		for key, value := range bucket {
			cloned[name][key] = append([]byte(nil), value...)
		}
	}

	return cloned
}

func cloneURLMetadataLengths(source map[vault.Name]int) map[vault.Name]int {
	cloned := make(map[vault.Name]int, len(source))
	for name, length := range source {
		cloned[name] = length
	}

	return cloned
}

func TestURLMetadataLengthFailureRollsBackRowAndRetry(t *testing.T) {
	engine := newURLMetadataLengthEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	directory, _, receiver, err := Open(storage)
	if err != nil {
		t.Fatalf("open URL metadata: %v", err)
	}
	row := urlRow(t, "atomic-length")
	hash := rowHash(t, row)
	want := errors.New("length journal failed")
	engine.lengthError = want

	if _, err := receiver.Receive(
		t.Context(),
		[]yagomodel.URIMetadataRow{row},
	); !errors.Is(err, want) {
		t.Fatalf("failed receive error = %v, want %v", err, want)
	}
	assertURLMetadataLengthState(t, directory, hash, 0)

	engine.lengthError = nil
	receipt, err := receiver.Receive(t.Context(), []yagomodel.URIMetadataRow{row})
	if err != nil {
		t.Fatalf("retry receive: %v", err)
	}
	if receipt.Busy || receipt.Double != 0 || len(receipt.ErrorURL) != 0 {
		t.Fatalf("retry receipt = %+v", receipt)
	}
	assertURLMetadataLengthState(t, directory, hash, 1)
}

func assertURLMetadataLengthState(
	t *testing.T,
	directory URLDirectory,
	hash yagomodel.Hash,
	want int,
) {
	t.Helper()
	rows, err := directory.RowsByHash(t.Context(), []yagomodel.Hash{hash})
	if err != nil {
		t.Fatalf("read URL metadata: %v", err)
	}
	if len(rows) != want {
		t.Fatalf("stored URL rows = %d, want %d", len(rows), want)
	}
	length, err := directory.Count(t.Context())
	if err != nil {
		t.Fatalf("count URL metadata: %v", err)
	}
	if length != want {
		t.Fatalf("URL metadata length = %d, want %d", length, want)
	}
}
