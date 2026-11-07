package rwibackfill

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type checkpointTestEngine struct {
	buckets      map[vault.Name]map[string][]byte
	updateErr    error
	viewErr      error
	provisionErr error
}

func (e *checkpointTestEngine) Update(
	ctx context.Context,
	visit func(vault.EngineTxn) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("checkpoint update context: %w", err)
	}
	if e.updateErr != nil {
		err := e.updateErr
		e.updateErr = nil
		return err
	}
	return visit(checkpointTestTxn{engine: e, writable: true})
}

func (e *checkpointTestEngine) View(ctx context.Context, visit func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("checkpoint view context: %w", err)
	}
	if e.viewErr != nil {
		return e.viewErr
	}
	return visit(checkpointTestTxn{engine: e})
}

func (e *checkpointTestEngine) Provision(name vault.Name) error {
	if e.provisionErr != nil {
		return e.provisionErr
	}
	if e.buckets == nil {
		e.buckets = make(map[vault.Name]map[string][]byte)
	}
	if e.buckets[name] == nil {
		e.buckets[name] = make(map[string][]byte)
	}
	return nil
}

func (*checkpointTestEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (*checkpointTestEngine) QuotaBytes() int64                        { return 0 }
func (*checkpointTestEngine) Close() error                             { return nil }

type checkpointTestTxn struct {
	engine   *checkpointTestEngine
	writable bool
}

func (tx checkpointTestTxn) Bucket(name vault.Name) vault.EngineBucket {
	if tx.engine.buckets[name] == nil {
		tx.engine.buckets[name] = make(map[string][]byte)
	}
	return checkpointTestBucket{values: tx.engine.buckets[name]}
}

func (tx checkpointTestTxn) Writable() bool { return tx.writable }

type checkpointTestBucket struct {
	values map[string][]byte
}

func (b checkpointTestBucket) Get(key vault.Key) []byte {
	return b.values[string(key)]
}

func (b checkpointTestBucket) Put(key vault.Key, value []byte) error {
	b.values[string(key)] = append([]byte(nil), value...)
	return nil
}

func (b checkpointTestBucket) Delete(key vault.Key) error {
	delete(b.values, string(key))
	return nil
}

func (b checkpointTestBucket) Scan(
	prefix vault.Key,
	visit func(vault.Key, []byte) (bool, error),
) error {
	for key, value := range b.values {
		if len(key) < len(prefix) || key[:len(prefix)] != string(prefix) {
			continue
		}
		keepGoing, err := visit(vault.Key(key), value)
		if err != nil || !keepGoing {
			return err
		}
	}
	return nil
}

func validCheckpoint() checkpoint {
	return checkpoint{
		Format:             checkpointFormat,
		State:              stateRunning,
		UpdatedAtUnixMilli: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC).UnixMilli(),
	}
}

func TestCheckpointCodecRoundTripAndRejectsInvalidInput(t *testing.T) {
	codec := checkpointCodec{}
	encoded, err := codec.Encode(validCheckpoint())
	if err != nil {
		t.Fatalf("Encode valid checkpoint: %v", err)
	}
	decoded, err := codec.Decode(encoded)
	if err != nil || decoded != validCheckpoint() {
		t.Fatalf("Decode checkpoint = %+v, error %v", decoded, err)
	}

	invalid := validCheckpoint()
	invalid.Format = "unknown"
	if _, err := codec.Encode(invalid); err == nil {
		t.Fatal("Encode accepted unsupported checkpoint format")
	}
	for name, value := range map[string][]byte{
		"empty":          nil,
		"oversized":      []byte(strings.Repeat("x", (128<<10)+1)),
		"malformed":      []byte("{"),
		"unknown field":  []byte(`{"format":"yago-rwi-backfill-v1","state":"running","updatedAtUnixMilli":1,"unknown":true}`),
		"invalid state":  []byte(`{"format":"yago-rwi-backfill-v1","state":"unknown","updatedAtUnixMilli":1}`),
		"trailing value": append(append([]byte(nil), encoded...), []byte(` {}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := codec.Decode(value); err == nil {
				t.Fatal("Decode accepted invalid checkpoint bytes")
			}
		})
	}
}

func TestCheckpointValidationRejectsOutOfRangeAndInconsistentFields(t *testing.T) {
	cases := map[string]func(*checkpoint){
		"state":               func(value *checkpoint) { value.State = "unknown" },
		"negative scanned":    func(value *checkpoint) { value.ScannedDocuments = -1 },
		"negative indexed":    func(value *checkpoint) { value.IndexedDocuments = -1 },
		"negative skipped":    func(value *checkpoint) { value.SkippedDocuments = -1 },
		"negative postings":   func(value *checkpoint) { value.PostingRows = -1 },
		"negative bytes":      func(value *checkpoint) { value.EstimatedBytes = -1 },
		"negative limit":      func(value *checkpoint) { value.MaximumDocumentsRun = -1 },
		"zero time":           func(value *checkpoint) { value.UpdatedAtUnixMilli = 0 },
		"long cursor":         func(value *checkpoint) { value.Cursor = strings.Repeat("x", (96<<10)+1) },
		"long error":          func(value *checkpoint) { value.LastError = strings.Repeat("x", 513) },
		"inconsistent totals": func(value *checkpoint) { value.IndexedDocuments = 1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			value := validCheckpoint()
			mutate(&value)
			if err := validateCheckpoint(value); err == nil {
				t.Fatal("validateCheckpoint accepted invalid value")
			}
		})
	}
}

func TestCheckpointRepositoryReadsWritesAndReportsStorageErrors(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	repository, err := openCheckpointRepository(storage)
	if err != nil {
		t.Fatalf("openCheckpointRepository: %v", err)
	}
	if _, err := openCheckpointRepository(storage); err == nil {
		t.Fatal("duplicate checkpoint repository registration succeeded")
	}
	value, found, err := repository.load(t.Context())
	if err != nil || found || value != (checkpoint{}) {
		t.Fatalf("empty load = %+v, found %v, error %v", value, found, err)
	}
	if err := repository.save(t.Context(), validCheckpoint()); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	value, found, err = repository.load(t.Context())
	if err != nil || !found || value != validCheckpoint() {
		t.Fatalf("saved load = %+v, found %v, error %v", value, found, err)
	}
	if err := repository.save(t.Context(), checkpoint{}); err == nil {
		t.Fatal("save accepted invalid checkpoint")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := repository.load(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled load error = %v", err)
	}
	if err := repository.save(canceled, validCheckpoint()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled save error = %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("storage.Close: %v", err)
	}
}

func TestCheckpointRepositoryReportsCorruptAndEngineFailures(t *testing.T) {
	engine := &checkpointTestEngine{}
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	defer func() { _ = storage.Close() }()
	repository, err := openCheckpointRepository(storage)
	if err != nil {
		t.Fatalf("openCheckpointRepository: %v", err)
	}
	engine.buckets[vault.Name("rwi_backfill_checkpoints")][string(checkpointKey)] = []byte(
		"not-json",
	)
	if _, _, err := repository.load(t.Context()); err == nil {
		t.Fatal("load accepted corrupt checkpoint bytes")
	}
	delete(engine.buckets[vault.Name("rwi_backfill_checkpoints")], string(checkpointKey))
	engine.viewErr = errors.New("scripted view failure")
	if _, _, err := repository.load(t.Context()); err == nil {
		t.Fatal("load ignored engine view failure")
	}
	engine.viewErr = nil
	engine.updateErr = errors.New("scripted update failure")
	if err := repository.save(t.Context(), validCheckpoint()); err == nil {
		t.Fatal("save ignored engine update failure")
	}
	if _, err := vault.New(
		&checkpointTestEngine{provisionErr: errors.New("provision")},
	); err == nil {
		t.Fatal("vault.New ignored length-bucket provision failure")
	}
}
