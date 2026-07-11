package hosttrust

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type rawPolicyCodec struct{}

func (rawPolicyCodec) Encode(value []byte) ([]byte, error) {
	return append([]byte(nil), value...), nil
}

func (rawPolicyCodec) Decode(value []byte) ([]byte, error) {
	return append([]byte(nil), value...), nil
}

type failingPutEngine struct {
	fail bool
}

func (e *failingPutEngine) Update(_ context.Context, operation func(vault.EngineTxn) error) error {
	return operation(failingPutTransaction{engine: e, writable: true})
}

func (e *failingPutEngine) View(_ context.Context, operation func(vault.EngineTxn) error) error {
	return operation(failingPutTransaction{engine: e})
}

func (*failingPutEngine) Provision(vault.Name) error               { return nil }
func (*failingPutEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (*failingPutEngine) QuotaBytes() int64                        { return 0 }
func (*failingPutEngine) Close() error                             { return nil }

type failingPutTransaction struct {
	engine   *failingPutEngine
	writable bool
}

func (t failingPutTransaction) Bucket(vault.Name) vault.EngineBucket {
	return failingPutBucket{engine: t.engine}
}

func (t failingPutTransaction) Writable() bool { return t.writable }

type failingPutBucket struct {
	engine *failingPutEngine
}

func (failingPutBucket) Get(vault.Key) []byte { return nil }

func (b failingPutBucket) Put(vault.Key, []byte) error {
	if b.engine.fail {
		return errors.New("write failed")
	}

	return nil
}

func (failingPutBucket) Delete(vault.Key) error { return nil }

func (failingPutBucket) Scan(vault.Key, func(vault.Key, []byte) (bool, error)) error {
	return nil
}

func TestCatalogCanonicalizesPersistsAndNotifies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host-trust.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(t.Context(), storage)
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.Current(); got.Blend != 0 || len(got.Domains) != 0 {
		t.Fatalf("initial policy = %#v", got)
	}
	want := Policy{Blend: 0.4, Domains: []string{"127.0.0.1", "example.co.uk"}}
	if err := catalog.Replace(t.Context(), Policy{
		Blend: 0.4,
		Domains: []string{
			" HTTPS://www.Example.co.uk/path ",
			"example.co.uk",
			"http://127.0.0.1/path",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if got := catalog.Current(); !reflect.DeepEqual(got, want) {
		t.Fatalf("current policy = %#v, want %#v", got, want)
	}
	select {
	case <-catalog.Changes():
	default:
		t.Fatal("policy change was not signaled")
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	reopenedStorage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopenedStorage.Close() })
	reopened, err := Open(t.Context(), reopenedStorage)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Current(); !reflect.DeepEqual(got, want) {
		t.Fatalf("reopened policy = %#v, want %#v", got, want)
	}
}

func TestCatalogSnapshotsAreImmutableAndSignalsCoalesce(t *testing.T) {
	catalog, storage := openCatalog(t)
	firstPolicy := Policy{Blend: 0.2, Domains: []string{"a.example"}}
	if err := catalog.Replace(t.Context(), firstPolicy); err != nil {
		t.Fatal(err)
	}
	first := catalog.Current()
	first.Domains[0] = "changed.example"
	if got := catalog.Current().Domains[0]; got != "a.example" {
		t.Fatalf("catalog aliased caller snapshot: %q", got)
	}
	secondPolicy := Policy{Blend: 0.3, Domains: []string{"b.example"}}
	if err := catalog.Replace(t.Context(), secondPolicy); err != nil {
		t.Fatal(err)
	}
	if len(catalog.changes) != 1 {
		t.Fatalf("coalesced signals = %d", len(catalog.changes))
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	before := catalog.Current()
	closedPolicy := Policy{Blend: 0.5, Domains: []string{"c.example"}}
	if err := catalog.Replace(t.Context(), closedPolicy); err == nil {
		t.Fatal("replace on closed storage succeeded")
	}
	if got := catalog.Current(); !reflect.DeepEqual(got, before) {
		t.Fatalf("failed replace changed policy: %#v", got)
	}
}

func TestCatalogRejectsInvalidPolicies(t *testing.T) {
	catalog, _ := openCatalog(t)
	tooMany := make([]string, MaximumDomains+1)
	for index := range tooMany {
		tooMany[index] = "example.com"
	}
	cases := []Policy{
		{Blend: -0.1},
		{Blend: 1.1},
		{Blend: math.NaN()},
		{Blend: math.Inf(1)},
		{Domains: tooMany},
		{Domains: []string{" "}},
		{Domains: []string{"bad_label.example"}},
		{Domains: []string{"-bad.example"}},
		{Domains: []string{strings.Repeat("a", 64) + ".example"}},
		{Domains: []string{strings.Repeat("a", maximumDomainBytes+1)}},
	}
	for _, policy := range cases {
		if err := catalog.Replace(t.Context(), policy); err == nil {
			t.Errorf("invalid policy was accepted: %#v", policy)
		}
	}
}

func TestCatalogNilAndRegistrationBehavior(t *testing.T) {
	var catalog *Catalog
	if got := catalog.Current(); got.Domains == nil || len(got.Domains) != 0 {
		t.Fatalf("nil current = %#v", got)
	}
	if catalog.Changes() != nil {
		t.Fatal("nil catalog returned a changes channel")
	}
	if got := (&Catalog{}).Current(); got.Domains == nil || len(got.Domains) != 0 {
		t.Fatalf("unset current = %#v", got)
	}
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), storage); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), storage); err == nil {
		t.Fatal("duplicate catalog registration succeeded")
	}
}

func TestPolicyCodecRejectsMalformedAndNoncanonicalRecords(t *testing.T) {
	codec := policyCodec{}
	if _, err := codec.Decode([]byte("{")); err == nil {
		t.Fatal("malformed JSON decoded")
	}
	records := []catalogRecord{
		{},
		{Format: catalogFormat, Policy: Policy{Domains: []string{"B.example", "a.example"}}},
		{Format: catalogFormat, Policy: Policy{Domains: []string{"bad_label.example"}}},
	}
	for _, record := range records {
		if _, err := codec.Encode(record); err == nil {
			t.Errorf("invalid record encoded: %#v", record)
		}
	}
	valid := catalogRecord{
		Format: catalogFormat,
		Policy: Policy{Blend: 0.5, Domains: []string{"a.example", "b.example"}},
	}
	encoded, err := codec.Encode(valid)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(encoded)
	if err != nil || !reflect.DeepEqual(decoded, valid) {
		t.Fatalf("round trip = %#v, %v", decoded, err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	document["format"] = "future"
	unsupported, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(unsupported); err == nil {
		t.Fatal("unsupported record decoded")
	}
}

func TestOpenRejectsCorruptPersistedPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := vault.Register(storage, catalogBucket, rawPolicyCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return raw.Put(tx, catalogKey, []byte("{"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if _, err := Open(t.Context(), reopened); err == nil {
		t.Fatal("corrupt persisted policy was loaded")
	}
}

func TestCatalogSurfacesCollectionWriteFailure(t *testing.T) {
	engine := &failingPutEngine{}
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(t.Context(), storage)
	if err != nil {
		t.Fatal(err)
	}
	engine.fail = true
	if err := catalog.Replace(t.Context(), Policy{Domains: []string{"example.com"}}); err == nil {
		t.Fatal("collection write failure was ignored")
	}
}

func openCatalog(t *testing.T) (*Catalog, *vault.Vault) {
	t.Helper()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	catalog, err := Open(context.Background(), storage)
	if err != nil {
		t.Fatal(err)
	}

	return catalog, storage
}
