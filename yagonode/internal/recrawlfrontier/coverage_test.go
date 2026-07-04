package recrawlfrontier

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// ctrlEngine is a controllable in-memory vault.Engine used to drive the error
// branches of the frontier: it can be told to fail Provision, Put or Delete on a
// named bucket, and lets tests seed raw (possibly corrupt) bytes into a bucket.
type ctrlEngine struct {
	buckets  map[vault.Name]map[string][]byte
	failProv map[vault.Name]bool
	failPut  map[vault.Name]bool
	failDel  map[vault.Name]bool
}

func newCtrlEngine() *ctrlEngine {
	return &ctrlEngine{
		buckets:  map[vault.Name]map[string][]byte{},
		failProv: map[vault.Name]bool{},
		failPut:  map[vault.Name]bool{},
		failDel:  map[vault.Name]bool{},
	}
}

func (e *ctrlEngine) seed(bucket vault.Name, key string, val []byte) {
	if e.buckets[bucket] == nil {
		e.buckets[bucket] = map[string][]byte{}
	}
	e.buckets[bucket][key] = val
}

func (e *ctrlEngine) Provision(name vault.Name) error {
	if e.failProv[name] {
		return fmt.Errorf("ctrl provision %s", name)
	}
	if _, ok := e.buckets[name]; !ok {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *ctrlEngine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("scripted engine: %w", err)
	}
	staged := snapshotCtrl(e.buckets)
	if err := fn(&ctrlTxn{engine: e, buckets: staged, writable: true}); err != nil {
		return err
	}
	e.buckets = staged

	return nil
}

func (e *ctrlEngine) View(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("scripted engine: %w", err)
	}

	return fn(&ctrlTxn{engine: e, buckets: e.buckets, writable: false})
}

func (e *ctrlEngine) Close() error { return nil }

func (e *ctrlEngine) QuotaBytes() int64 { return 0 }

func (e *ctrlEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }

func snapshotCtrl(src map[vault.Name]map[string][]byte) map[vault.Name]map[string][]byte {
	out := make(map[vault.Name]map[string][]byte, len(src))
	for name, bucket := range src {
		entries := make(map[string][]byte, len(bucket))
		for k, v := range bucket {
			entries[k] = copyBytes(v)
		}
		out[name] = entries
	}

	return out
}

// copyBytes clones val while keeping an empty-but-present value non-nil, so the
// presence codec's empty payload is not mistaken for an absent key on Delete.
func copyBytes(val []byte) []byte {
	out := make([]byte, len(val))
	copy(out, val)

	return out
}

type ctrlTxn struct {
	engine   *ctrlEngine
	buckets  map[vault.Name]map[string][]byte
	writable bool
}

func (t *ctrlTxn) Writable() bool { return t.writable }

func (t *ctrlTxn) Bucket(name vault.Name) vault.EngineBucket {
	return &ctrlBucket{engine: t.engine, name: name, entries: t.buckets[name]}
}

type ctrlBucket struct {
	engine  *ctrlEngine
	name    vault.Name
	entries map[string][]byte
}

func (b *ctrlBucket) Get(key vault.Key) []byte {
	val, ok := b.entries[string(key)]
	if !ok {
		return nil
	}

	return val
}

func (b *ctrlBucket) Put(key vault.Key, val []byte) error {
	if b.engine.failPut[b.name] {
		return fmt.Errorf("ctrl put %s", b.name)
	}
	b.entries[string(key)] = copyBytes(val)

	return nil
}

func (b *ctrlBucket) Delete(key vault.Key) error {
	if b.engine.failDel[b.name] {
		return fmt.Errorf("ctrl delete %s", b.name)
	}
	delete(b.entries, string(key))

	return nil
}

func (b *ctrlBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	keys := make([]string, 0, len(b.entries))
	for k := range b.entries {
		if strings.HasPrefix(k, string(prefix)) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		keep, err := fn(vault.Key(k), b.entries[k])
		if err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		if !keep {
			return nil
		}
	}

	return nil
}

func openCtrlFrontier(t *testing.T) (*Frontier, *ctrlEngine) {
	t.Helper()
	engine := newCtrlEngine()
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	frontier, err := Open(v)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	return frontier, engine
}

func hashKey(t *testing.T, url string) string {
	t.Helper()
	hash, err := yagomodel.HashURL(url)
	if err != nil {
		t.Fatalf("hash %s: %v", url, err)
	}

	return string(hash)
}

// year10000 makes NextDueAt = fetchedAt+48h land in year 10000, whose
// time.Time.MarshalJSON fails, exercising the record-encode error paths.
var year10000Base = time.Date(9999, 12, 31, 23, 0, 0, 0, time.UTC)

func TestOpenRecordRegisterError(t *testing.T) {
	engine := newCtrlEngine()
	engine.failProv[recordBucket] = true
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	if _, err := Open(v); err == nil {
		t.Fatal("expected error registering record bucket")
	}
}

func TestOpenDueRegisterError(t *testing.T) {
	engine := newCtrlEngine()
	engine.failProv[dueBucket] = true
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	if _, err := Open(v); err == nil {
		t.Fatal("expected error registering due bucket")
	}
}

func TestOpenProfileRegisterError(t *testing.T) {
	engine := newCtrlEngine()
	engine.failProv[profileBucket] = true
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	if _, err := Open(v); err == nil {
		t.Fatal("expected error registering profile bucket")
	}
}

func TestObserveEncodeError(t *testing.T) {
	f, _ := openCtrlFrontier(t)
	err := f.Observe(context.Background(), "https://a.example/", "h", 48*time.Hour, year10000Base)
	if err == nil {
		t.Fatal("expected encode error observing a year-10000 due time")
	}
}

func TestObserveClearDueReadError(t *testing.T) {
	f, engine := openCtrlFrontier(t)
	url := "https://a.example/"
	engine.seed(recordBucket, hashKey(t, url), []byte("{corrupt"))
	if err := f.Observe(context.Background(), url, "h", time.Hour, testBase); err == nil {
		t.Fatal("expected clearDue read error on corrupt record")
	}
}

func TestObserveDeleteRecordError(t *testing.T) {
	f, engine := openCtrlFrontier(t)
	ctx := context.Background()
	url := "https://a.example/"
	if err := f.Observe(ctx, url, "h", time.Hour, testBase); err != nil {
		t.Fatalf("seed observe: %v", err)
	}
	engine.failDel[recordBucket] = true
	if err := f.Observe(ctx, url, "h", 0, testBase); err == nil {
		t.Fatal("expected delete error dropping record on non-positive interval")
	}
}

func TestObserveClearDueDeleteError(t *testing.T) {
	f, engine := openCtrlFrontier(t)
	ctx := context.Background()
	url := "https://a.example/"
	if err := f.Observe(ctx, url, "h", time.Hour, testBase); err != nil {
		t.Fatalf("seed observe: %v", err)
	}
	engine.failDel[dueBucket] = true
	if err := f.Observe(ctx, url, "h", time.Hour, testBase); err == nil {
		t.Fatal("expected clearDue delete error on rescheduling")
	}
}

func TestObserveDuePutError(t *testing.T) {
	f, engine := openCtrlFrontier(t)
	engine.failPut[dueBucket] = true
	err := f.Observe(context.Background(), "https://a.example/", "h", time.Hour, testBase)
	if err == nil {
		t.Fatal("expected due-index put error scheduling")
	}
}

func TestClaimDueScanMalformedKey(t *testing.T) {
	f, engine := openCtrlFrontier(t)
	engine.seed(dueBucket, "malformed-no-separator", []byte{})
	if _, err := f.ClaimDue(context.Background(), testBase, 10); err == nil {
		t.Fatal("expected scan error on malformed due key")
	}
}

func TestClaimDueRecordReadError(t *testing.T) {
	f, engine := openCtrlFrontier(t)
	ctx := context.Background()
	url := "https://a.example/"
	if err := f.Observe(ctx, url, "h", time.Hour, testBase); err != nil {
		t.Fatalf("seed observe: %v", err)
	}
	engine.seed(recordBucket, hashKey(t, url), []byte("{corrupt"))
	if _, err := f.ClaimDue(ctx, testBase.Add(2*time.Hour), 10); err == nil {
		t.Fatal("expected read error on corrupt due record")
	}
}

func TestClaimDueDeleteError(t *testing.T) {
	f, engine := openCtrlFrontier(t)
	ctx := context.Background()
	url := "https://a.example/"
	if err := f.Observe(ctx, url, "h", time.Hour, testBase); err != nil {
		t.Fatalf("seed observe: %v", err)
	}
	engine.failDel[dueBucket] = true
	if _, err := f.ClaimDue(ctx, testBase.Add(2*time.Hour), 10); err == nil {
		t.Fatal("expected delete error clearing claimed due entry")
	}
}

func TestClaimDueScheduleError(t *testing.T) {
	f, engine := openCtrlFrontier(t)
	ctx := context.Background()
	url := "https://a.example/"
	if err := f.Observe(ctx, url, "h", time.Hour, testBase); err != nil {
		t.Fatalf("seed observe: %v", err)
	}
	engine.failPut[recordBucket] = true
	if _, err := f.ClaimDue(ctx, testBase.Add(2*time.Hour), 10); err == nil {
		t.Fatal("expected schedule error rewriting claimed record")
	}
}

func TestRecordProfilePutError(t *testing.T) {
	f, engine := openCtrlFrontier(t)
	engine.failPut[profileBucket] = true
	profile := profileWithRecrawl("Example", time.Hour)
	if err := f.RecordProfile(context.Background(), profile); err == nil {
		t.Fatal("expected put error recording profile")
	}
}

func TestProfileByHandleReadError(t *testing.T) {
	f, engine := openCtrlFrontier(t)
	engine.seed(profileBucket, "h", []byte("{corrupt"))
	if _, _, err := f.ProfileByHandle(context.Background(), "h"); err == nil {
		t.Fatal("expected read error decoding corrupt profile")
	}
}

func TestOwnsProfileReadError(t *testing.T) {
	f, engine := openCtrlFrontier(t)
	engine.seed(profileBucket, "h", []byte("{corrupt"))
	if _, err := f.OwnsProfile(context.Background(), "h"); err == nil {
		t.Fatal("expected read error checking ownership of corrupt profile")
	}
}

func TestRecordFetchProfileReadError(t *testing.T) {
	f, engine := openCtrlFrontier(t)
	engine.seed(profileBucket, "h", []byte("{corrupt"))
	if err := f.RecordFetch(context.Background(), "https://a.example/", "h", testBase); err == nil {
		t.Fatal("expected read error resolving profile in RecordFetch")
	}
}

func TestRecordFetchObserveError(t *testing.T) {
	f, _ := openCtrlFrontier(t)
	ctx := context.Background()
	profile := profileWithRecrawl("Example", 48*time.Hour)
	if err := f.RecordProfile(ctx, profile); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	err := f.RecordFetch(ctx, "https://a.example/", profile.Handle, year10000Base)
	if err == nil {
		t.Fatal("expected observe error scheduling a year-10000 due time")
	}
}
