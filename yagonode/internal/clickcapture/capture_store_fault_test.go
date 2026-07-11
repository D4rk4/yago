package clickcapture

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type evidenceFaultEngine struct {
	buckets map[vault.Name]*evidenceFaultBucket
}

type evidenceFaultTransaction struct {
	engine   *evidenceFaultEngine
	writable bool
}

type evidenceFaultBucket struct {
	values    map[string][]byte
	scanErr   error
	deleteErr error
	putErr    error
}

func newEvidenceFaultEngine() *evidenceFaultEngine {
	return &evidenceFaultEngine{buckets: map[vault.Name]*evidenceFaultBucket{}}
}

func (e *evidenceFaultEngine) Update(
	ctx context.Context,
	operation func(vault.EngineTxn) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fault update: %w", err)
	}

	return operation(evidenceFaultTransaction{engine: e, writable: true})
}

func (e *evidenceFaultEngine) View(
	ctx context.Context,
	operation func(vault.EngineTxn) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fault view: %w", err)
	}

	return operation(evidenceFaultTransaction{engine: e})
}

func (e *evidenceFaultEngine) Provision(name vault.Name) error {
	if e.buckets[name] == nil {
		e.buckets[name] = &evidenceFaultBucket{values: map[string][]byte{}}
	}

	return nil
}

func (e *evidenceFaultEngine) UsedBytes(context.Context) (int64, error) {
	return 0, nil
}

func (e *evidenceFaultEngine) QuotaBytes() int64 {
	return 0
}

func (e *evidenceFaultEngine) Close() error {
	return nil
}

func (t evidenceFaultTransaction) Bucket(name vault.Name) vault.EngineBucket {
	return t.engine.buckets[name]
}

func (t evidenceFaultTransaction) Writable() bool {
	return t.writable
}

func (b *evidenceFaultBucket) Get(key vault.Key) []byte {
	return append([]byte(nil), b.values[string(key)]...)
}

func (b *evidenceFaultBucket) Put(key vault.Key, value []byte) error {
	if b.putErr != nil {
		return b.putErr
	}
	b.values[string(key)] = append([]byte(nil), value...)

	return nil
}

func (b *evidenceFaultBucket) Delete(key vault.Key) error {
	if b.deleteErr != nil {
		return b.deleteErr
	}
	delete(b.values, string(key))

	return nil
}

func (b *evidenceFaultBucket) Scan(
	prefix vault.Key,
	visit func(vault.Key, []byte) (bool, error),
) error {
	if b.scanErr != nil {
		return b.scanErr
	}
	keys := make([]string, 0, len(b.values))
	for key := range b.values {
		if len(prefix) == 0 || len(key) >= len(prefix) && key[:len(prefix)] == string(prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		keepGoing, err := visit(vault.Key(key), append([]byte(nil), b.values[key]...))
		if err != nil {
			return err
		}
		if !keepGoing {
			break
		}
	}

	return nil
}

func TestStoreSurfacesCorruptRecordReads(t *testing.T) {
	clickStore, clickEngine := openFaultStore(t)
	result := displayedFixture("https://a.example/")
	token := mustIssueToken(t, clickStore.issuer, result)
	clickEngine.buckets[clickBucket].values["query"] = []byte("{")
	if err := clickStore.RecordClick(t.Context(), token, result[0].URLIdentity, 1); err == nil {
		t.Fatal("click over corrupt evidence succeeded")
	}

	impressionStore, impressionEngine := openFaultStore(t)
	impressionEngine.buckets[clickBucket].values["query"] = []byte("{")
	if err := impressionStore.recordImpression(
		t.Context(),
		storeClaims("query", "model", result),
	); err == nil {
		t.Fatal("impression over corrupt evidence succeeded")
	}
	interleavingStore, interleavingEngine := openFaultStore(t)
	interleavingEngine.buckets[clickBucket].values["query"] = []byte("{")
	if _, err := interleavingStore.PrepareTeamDraft(
		t.Context(), "query", draftRanking("model", []Candidate{{
			URLIdentity: "url", ClusterIdentity: "cluster", Position: 1,
		}}), draftRanking(LexicalRevision, []Candidate{{
			URLIdentity: "url", ClusterIdentity: "cluster", Position: 1,
		}}), 1,
	); err == nil {
		t.Fatal("interleaving over corrupt evidence succeeded")
	}
}

func TestStoreSurfacesCapacityFailures(t *testing.T) {
	badLengthStore, badLengthEngine := openFaultStore(t)
	badLengthEngine.buckets[vault.Name("__lengths__")].values[string(clickBucket)] = []byte{1}
	if err := badLengthStore.recordImpression(
		t.Context(),
		storeClaims("query", "model", displayedFixture("url")),
	); err == nil {
		t.Fatal("bad length counter succeeded")
	}

	scanStore, scanEngine := openFaultStore(t)
	seedLength(scanEngine, maximumStoredQueries)
	scanEngine.buckets[clickBucket].scanErr = errors.New("scan failed")
	if err := scanStore.recordImpression(
		t.Context(),
		storeClaims("query", "model", displayedFixture("url")),
	); err == nil {
		t.Fatal("capacity scan failure succeeded")
	}
	if _, err := scanStore.PrepareTeamDraft(
		t.Context(), "query", draftRanking("model", []Candidate{{
			URLIdentity: "url", ClusterIdentity: "cluster", Position: 1,
		}}), draftRanking(LexicalRevision, []Candidate{{
			URLIdentity: "url", ClusterIdentity: "cluster", Position: 1,
		}}), 1,
	); err == nil {
		t.Fatal("interleaving capacity scan failure succeeded")
	}

	emptyStore, emptyEngine := openFaultStore(t)
	seedLength(emptyEngine, maximumStoredQueries)
	if err := emptyStore.recordImpression(
		t.Context(),
		storeClaims("query", "model", displayedFixture("url")),
	); err == nil {
		t.Fatal("empty capacity eviction succeeded")
	}

	deleteStore, deleteEngine := openFaultStore(t)
	seedLength(deleteEngine, maximumStoredQueries)
	encoded, err := evidenceCodec{}.Encode(newQueryEvidence("old"))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	deleteEngine.buckets[clickBucket].values["old"] = encoded
	deleteEngine.buckets[clickBucket].deleteErr = errors.New("delete failed")
	if err := deleteStore.recordImpression(
		t.Context(),
		storeClaims("query", "model", displayedFixture("url")),
	); err == nil {
		t.Fatal("capacity delete failure succeeded")
	}

	putStore, putEngine := openFaultStore(t)
	putEngine.buckets[clickBucket].putErr = errors.New("put failed")
	if err := putStore.recordImpression(
		t.Context(),
		storeClaims("query", "model", displayedFixture("url")),
	); err == nil {
		t.Fatal("impression put failure succeeded")
	}
	interleavingPutStore, interleavingPutEngine := openFaultStore(t)
	interleavingPutEngine.buckets[clickBucket].putErr = errors.New("put failed")
	if _, err := interleavingPutStore.PrepareTeamDraft(
		t.Context(), "query", draftRanking("model", []Candidate{{
			URLIdentity: "url", ClusterIdentity: "cluster", Position: 1,
		}}), draftRanking(LexicalRevision, []Candidate{{
			URLIdentity: "url", ClusterIdentity: "cluster", Position: 1,
		}}), 1,
	); err == nil {
		t.Fatal("interleaving put failure succeeded")
	}
}

func openFaultStore(t *testing.T) (*Store, *evidenceFaultEngine) {
	t.Helper()
	engine := newEvidenceFaultEngine()
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	store, err := OpenWithSources(v, &sequenceEntropy{}, nil)
	if err != nil {
		t.Fatalf("OpenWithSources: %v", err)
	}

	return store, engine
}

func seedLength(engine *evidenceFaultEngine, length uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], length)
	engine.buckets[vault.Name("__lengths__")].values[string(clickBucket)] = encoded[:]
}
