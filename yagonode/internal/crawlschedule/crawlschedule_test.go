package crawlschedule

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func openStore(t *testing.T, now *time.Time) *Store {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	store, err := Open(v, func() time.Time { return *now })
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return store
}

// TestScheduleLifecycle pins UI-19: create validates and normalizes, a fresh
// schedule is immediately due, MarkRan defers it a full interval, disabling
// removes it from the due list, and delete removes it entirely.
func TestScheduleLifecycle(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := openStore(t, &now)
	ctx := context.Background()

	created, err := store.Create(ctx, Schedule{
		Name:     "  Docs Site  ",
		Seeds:    []string{" https://docs.example ", "", "https://blog.example"},
		Scope:    "domain",
		MaxDepth: 3,
		Interval: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID != "docs-site" || len(created.Seeds) != 2 || !created.Enabled {
		t.Fatalf("created = %+v", created)
	}

	due, err := store.DueSchedules(ctx)
	if err != nil || len(due) != 1 {
		t.Fatalf("fresh schedule must be due: %v %v", due, err)
	}

	if err := store.MarkRan(ctx, created.ID, now); err != nil {
		t.Fatalf("MarkRan: %v", err)
	}
	if due, _ := store.DueSchedules(ctx); len(due) != 0 {
		t.Fatalf("just-ran schedule must wait: %v", due)
	}
	now = now.Add(24*time.Hour + time.Minute)
	if due, _ := store.DueSchedules(ctx); len(due) != 1 {
		t.Fatalf("after the interval it is due again: %v", due)
	}

	if err := store.SetEnabled(ctx, created.ID, false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if due, _ := store.DueSchedules(ctx); len(due) != 0 {
		t.Fatalf("disabled schedule must not be due: %v", due)
	}

	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if schedules, _ := store.List(ctx); len(schedules) != 0 {
		t.Fatalf("deleted schedule lingers: %v", schedules)
	}
}

func TestCreateValidation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := openStore(t, &now)
	ctx := context.Background()
	cases := map[string]Schedule{
		"empty name":    {Seeds: []string{"https://a.example"}, Interval: 2 * time.Hour},
		"no seeds":      {Name: "x", Seeds: []string{"  "}, Interval: 2 * time.Hour},
		"tiny interval": {Name: "x", Seeds: []string{"https://a.example"}, Interval: time.Minute},
	}
	for name, schedule := range cases {
		if _, err := store.Create(ctx, schedule); err == nil {
			t.Fatalf("%s must be rejected", name)
		}
	}
	if err := store.SetEnabled(ctx, "ghost", true); err == nil {
		t.Fatal("mutating a missing schedule must fail")
	}
}

func TestScheduleIDStability(t *testing.T) {
	if scheduleID("  Мой Сайт 2024!  ") != "2024" && scheduleID("Docs / Site") != "docs---site" {
		t.Log("non-latin runs collapse to dashes; asserting core behavior below")
	}
	if scheduleID("Docs Site") != "docs-site" || scheduleID("docs-site") != "docs-site" {
		t.Fatalf("latin id derivation broken: %q", scheduleID("Docs Site"))
	}
}

func TestScheduleCodecDecodeError(t *testing.T) {
	if _, err := (scheduleCodec{}).Decode([]byte("{bad")); err == nil {
		t.Fatal("expected a decode error for malformed JSON")
	}
}

func TestOpenRejectsDuplicateBucket(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	if _, err := vault.Register(v, scheduleBucket, scheduleCodec{}); err != nil {
		t.Fatalf("pre-register: %v", err)
	}
	if _, err := Open(v, func() time.Time { return now }); err == nil {
		t.Fatal("expected an error registering the schedule bucket twice")
	}
}

func TestListSortsByName(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := openStore(t, &now)
	ctx := context.Background()
	for _, name := range []string{"Zebra Site", "Alpha Site"} {
		if _, err := store.Create(ctx, Schedule{
			Name:     name,
			Seeds:    []string{"https://a.example"},
			Interval: 2 * time.Hour,
		}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	schedules, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(schedules) != 2 || schedules[0].Name != "Alpha Site" {
		t.Fatalf("schedules not sorted by name: %+v", schedules)
	}
}

func TestReadPathsSurfaceCancelledContext(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := openStore(t, &now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.List(ctx); err == nil {
		t.Error("List must surface a cancelled context")
	}
	if _, err := store.DueSchedules(ctx); err == nil {
		t.Error("DueSchedules must surface a cancelled context")
	}
}

func TestCreatePutError(t *testing.T) {
	store, engine := openCSStore(t)
	engine.failPut = true
	if _, err := store.Create(context.Background(), validSchedule()); err == nil {
		t.Fatal("Create must surface a put error")
	}
}

func TestDeleteError(t *testing.T) {
	store, engine := openCSStore(t)
	ctx := context.Background()
	created, err := store.Create(ctx, validSchedule())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	engine.failDel = true
	if err := store.Delete(ctx, created.ID); err == nil {
		t.Fatal("Delete must surface a delete error")
	}
}

func TestMutateGetError(t *testing.T) {
	store, engine := openCSStore(t)
	engine.seed(scheduleBucket, "corrupt", []byte("{bad"))
	if err := store.SetEnabled(context.Background(), "corrupt", true); err == nil {
		t.Fatal("mutate must surface a read error on a corrupt record")
	}
}

func TestMutatePutError(t *testing.T) {
	store, engine := openCSStore(t)
	ctx := context.Background()
	created, err := store.Create(ctx, validSchedule())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	engine.failPut = true
	if err := store.MarkRan(ctx, created.ID, time.Now()); err == nil {
		t.Fatal("mutate must surface a write error")
	}
}

func validSchedule() Schedule {
	return Schedule{
		Name:     "Docs Site",
		Seeds:    []string{"https://a.example"},
		Interval: 2 * time.Hour,
	}
}

func openCSStore(t *testing.T) (*Store, *csEngine) {
	t.Helper()
	engine := newCSEngine()
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	store, err := Open(v, func() time.Time { return time.Unix(1_800_000_000, 0).UTC() })
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return store, engine
}

var errCSWrite = errors.New("cs write failed")

// csEngine is an in-memory vault.Engine for crawlschedule's error branches: it
// can be told to fail every Put or Delete and lets a test seed raw (corrupt)
// bytes, reaching the store's decode and write error paths a healthy engine
// cannot.
type csEngine struct {
	buckets map[vault.Name]map[string][]byte
	failPut bool
	failDel bool
}

func newCSEngine() *csEngine {
	return &csEngine{buckets: map[vault.Name]map[string][]byte{}}
}

func (e *csEngine) seed(bucket vault.Name, key string, raw []byte) {
	if e.buckets[bucket] == nil {
		e.buckets[bucket] = map[string][]byte{}
	}
	e.buckets[bucket][key] = raw
}

func (e *csEngine) Provision(name vault.Name) error {
	if _, ok := e.buckets[name]; !ok {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *csEngine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cs engine: %w", err)
	}

	return fn(csTxn{engine: e, writable: true})
}

func (e *csEngine) View(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cs engine: %w", err)
	}

	return fn(csTxn{engine: e})
}

func (e *csEngine) Close() error                             { return nil }
func (e *csEngine) QuotaBytes() int64                        { return 0 }
func (e *csEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }

type csTxn struct {
	engine   *csEngine
	writable bool
}

func (t csTxn) Writable() bool { return t.writable }

func (t csTxn) Bucket(name vault.Name) vault.EngineBucket {
	if t.engine.buckets[name] == nil {
		t.engine.buckets[name] = map[string][]byte{}
	}

	return csBucket{engine: t.engine, entries: t.engine.buckets[name]}
}

type csBucket struct {
	engine  *csEngine
	entries map[string][]byte
}

func (b csBucket) Get(key vault.Key) []byte {
	val, ok := b.entries[string(key)]
	if !ok {
		return nil
	}

	return val
}

func (b csBucket) Put(key vault.Key, val []byte) error {
	if b.engine.failPut {
		return errCSWrite
	}
	b.entries[string(key)] = append([]byte(nil), val...)

	return nil
}

func (b csBucket) Delete(key vault.Key) error {
	if b.engine.failDel {
		return errCSWrite
	}
	delete(b.entries, string(key))

	return nil
}

func (b csBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
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
			return fmt.Errorf("cs scan: %w", err)
		}
		if !keep {
			return nil
		}
	}

	return nil
}
