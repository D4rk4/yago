package crawlresults

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type observationFaultEngine struct {
	buckets      map[vault.Name]map[string][]byte
	provisionErr error
	viewErr      error
	updateErr    error
	putErrBucket vault.Name
	betweenViews func()
}

func newObservationFaultEngine() *observationFaultEngine {
	return &observationFaultEngine{buckets: make(map[vault.Name]map[string][]byte)}
}

func (e *observationFaultEngine) Provision(name vault.Name) error {
	if e.provisionErr != nil {
		return e.provisionErr
	}
	if e.buckets[name] == nil {
		e.buckets[name] = make(map[string][]byte)
	}

	return nil
}

func (e *observationFaultEngine) Update(
	_ context.Context,
	fn func(vault.EngineTxn) error,
) error {
	if e.updateErr != nil {
		return e.updateErr
	}

	return fn(observationFaultTxn{engine: e, writable: true})
}

func (e *observationFaultEngine) View(
	_ context.Context,
	fn func(vault.EngineTxn) error,
) error {
	if e.viewErr != nil {
		return e.viewErr
	}
	if err := fn(observationFaultTxn{engine: e}); err != nil {
		return err
	}
	if e.betweenViews != nil {
		between := e.betweenViews
		e.betweenViews = nil
		between()

		return fn(observationFaultTxn{engine: e})
	}

	return nil
}

func (e *observationFaultEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }

func (e *observationFaultEngine) QuotaBytes() int64 { return 0 }

func (e *observationFaultEngine) Close() error { return nil }

type observationFaultTxn struct {
	engine   *observationFaultEngine
	writable bool
}

func (t observationFaultTxn) Bucket(name vault.Name) vault.EngineBucket {
	return observationFaultBucket{engine: t.engine, name: name}
}

func (t observationFaultTxn) Writable() bool { return t.writable }

type observationFaultBucket struct {
	engine *observationFaultEngine
	name   vault.Name
}

func (b observationFaultBucket) Get(key vault.Key) []byte {
	return b.engine.buckets[b.name][string(key)]
}

func (b observationFaultBucket) Put(key vault.Key, value []byte) error {
	if b.engine.putErrBucket == b.name {
		return errors.New("put failed")
	}
	b.engine.buckets[b.name][string(key)] = append([]byte(nil), value...)

	return nil
}

func (b observationFaultBucket) Delete(key vault.Key) error {
	delete(b.engine.buckets[b.name], string(key))

	return nil
}

func (b observationFaultBucket) Scan(
	prefix vault.Key,
	fn func(vault.Key, []byte) (bool, error),
) error {
	for key, value := range b.engine.buckets[b.name] {
		if len(prefix) > len(key) || string(prefix) != key[:len(prefix)] {
			continue
		}
		keep, err := fn(vault.Key(key), value)
		if err != nil || !keep {
			return err
		}
	}

	return nil
}

func openObservationFaultHistory(
	t *testing.T,
) (*URLObservationHistory, *observationFaultEngine) {
	t.Helper()
	engine := newObservationFaultEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	history, err := OpenURLObservationHistory(storage)
	if err != nil {
		t.Fatal(err)
	}

	return history, engine
}

func historyBatch(id string, at time.Time) yagocrawlcontract.IngestBatch {
	return yagocrawlcontract.IngestBatch{
		SourceURL:     "https://example.org/page",
		ObservationID: id,
		ObservedAt:    at,
	}
}

func TestURLObservationCodecErrors(t *testing.T) {
	if _, err := (urlObservationCodec{}).Encode(urlObservationRecord{
		ObservedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err == nil {
		t.Fatal("invalid observation time encoded")
	}
	if _, err := (urlObservationCodec{}).Decode([]byte("{")); err == nil {
		t.Fatal("invalid observation record decoded")
	}
}

func TestOpenURLObservationHistoryReturnsProvisionError(t *testing.T) {
	engine := newObservationFaultEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	engine.provisionErr = errors.New("provision failed")
	if _, err := OpenURLObservationHistory(storage); err == nil {
		t.Fatal("provision error was ignored")
	}
}

func TestURLObservationHistoryBeginErrors(t *testing.T) {
	t.Run("identity", func(t *testing.T) {
		storage, err := memvault.Open(0)
		if err != nil {
			t.Fatal(err)
		}
		history, err := OpenURLObservationHistory(storage)
		if err != nil {
			t.Fatal(err)
		}
		batch := historyBatch("", time.Time{})
		batch.Document.DateConfidence = math.NaN()
		_, err = history.Begin(t.Context(), []yagocrawlcontract.IngestBatch{batch})
		if err == nil {
			t.Fatal("identity error was ignored")
		}
	})

	t.Run("decode", func(t *testing.T) {
		history, engine := openObservationFaultHistory(t)
		batch := historyBatch("current", time.Now())
		key := string(observationURLKey(batch.SourceURL))
		engine.buckets[urlObservationBucket][key] = []byte("{")
		_, err := history.Begin(t.Context(), []yagocrawlcontract.IngestBatch{batch})
		if err == nil {
			t.Fatal("decode error was ignored")
		}
	})

	t.Run("view", func(t *testing.T) {
		history, engine := openObservationFaultHistory(t)
		engine.viewErr = errors.New("view failed")
		if _, err := history.Begin(t.Context(), []yagocrawlcontract.IngestBatch{
			historyBatch("current", time.Now()),
		}); err == nil {
			t.Fatal("view error was ignored")
		}
	})
}

func TestURLObservationHistoryResetsDecisionsOnViewReplay(t *testing.T) {
	history, engine := openObservationFaultHistory(t)
	base := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	current := historyBatch("current", base.Add(time.Hour))
	if err := history.Complete(t.Context(), []yagocrawlcontract.IngestBatch{current}); err != nil {
		t.Fatal(err)
	}
	key := string(observationURLKey(current.SourceURL))
	engine.betweenViews = func() { delete(engine.buckets[urlObservationBucket], key) }
	dispositions, err := history.Begin(t.Context(), []yagocrawlcontract.IngestBatch{
		historyBatch("older", base),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dispositions) != 1 || dispositions[0] != observationApply {
		t.Fatalf("replayed dispositions = %v, want apply", dispositions)
	}
}

func TestURLObservationHistoryCompleteOrdering(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	history, err := OpenURLObservationHistory(storage)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	older := historyBatch("older", base)
	newer := historyBatch("newer", base.Add(time.Hour))
	if err := history.Complete(t.Context(), []yagocrawlcontract.IngestBatch{older}); err != nil {
		t.Fatal(err)
	}
	if err := history.Complete(t.Context(), []yagocrawlcontract.IngestBatch{older}); err != nil {
		t.Fatal(err)
	}
	if err := history.Complete(t.Context(), []yagocrawlcontract.IngestBatch{newer}); err != nil {
		t.Fatal(err)
	}
	if err := history.Complete(t.Context(), []yagocrawlcontract.IngestBatch{older}); err == nil {
		t.Fatal("older completion replaced newer observation")
	}
	dispositions, err := history.Begin(t.Context(), []yagocrawlcontract.IngestBatch{newer, older})
	if err != nil {
		t.Fatal(err)
	}
	if len(dispositions) != 2 || dispositions[0] != observationDuplicate ||
		dispositions[1] != observationSuperseded {
		t.Fatalf("dispositions = %v", dispositions)
	}
}

func TestURLObservationHistoryCompleteErrors(t *testing.T) {
	t.Run("identity", func(t *testing.T) {
		history, _ := openObservationFaultHistory(t)
		batch := historyBatch("", time.Time{})
		batch.Document.DateConfidence = math.NaN()
		err := history.Complete(t.Context(), []yagocrawlcontract.IngestBatch{batch})
		if err == nil {
			t.Fatal("identity error was ignored")
		}
	})

	t.Run("decode", func(t *testing.T) {
		history, engine := openObservationFaultHistory(t)
		batch := historyBatch("current", time.Now())
		key := string(observationURLKey(batch.SourceURL))
		engine.buckets[urlObservationBucket][key] = []byte("{")
		err := history.Complete(t.Context(), []yagocrawlcontract.IngestBatch{batch})
		if err == nil {
			t.Fatal("decode error was ignored")
		}
	})

	t.Run("write", func(t *testing.T) {
		history, engine := openObservationFaultHistory(t)
		engine.putErrBucket = urlObservationBucket
		if err := history.Complete(t.Context(), []yagocrawlcontract.IngestBatch{
			historyBatch("current", time.Now()),
		}); err == nil {
			t.Fatal("write error was ignored")
		}
	})

	t.Run("update", func(t *testing.T) {
		history, engine := openObservationFaultHistory(t)
		engine.updateErr = errors.New("update failed")
		if err := history.Complete(t.Context(), []yagocrawlcontract.IngestBatch{
			historyBatch("current", time.Now()),
		}); err == nil {
			t.Fatal("update error was ignored")
		}
	})
}
