package crawlprofilelibrary

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func openProfileLibrary(t *testing.T) *Library {
	t.Helper()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	library, err := Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	library.entropy = bytes.NewReader(bytes.Repeat([]byte{7}, 4096))
	library.now = func() time.Time { return time.Unix(100, 0).UTC() }

	return library
}

func savedProfileDefinition(name string) yagocrawlcontract.CrawlProfile {
	maximum := 100
	return yagocrawlcontract.CrawlProfile{
		Name: name, Scope: yagocrawlcontract.ScopeDomain,
		URLMustMatch:      yagocrawlcontract.MatchAll,
		IndexURLMustMatch: yagocrawlcontract.MatchAll,
		MaxDepth:          3, MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		MaxPagesPerRun: &maximum,
	}
}

func TestProfileLibraryLifecycleKeepsDefinitionsIndependent(t *testing.T) {
	library := openProfileLibrary(t)
	created, err := library.Create(t.Context(), savedProfileDefinition("  Docs   crawl "))
	if err != nil {
		t.Fatal(err)
	}
	if created.Profile.Name != "Docs crawl" || created.Profile.Handle == "" {
		t.Fatalf("created = %#v", created)
	}
	loaded, err := library.Profile(t.Context(), created.ID)
	if err != nil || !reflect.DeepEqual(loaded, created) {
		t.Fatalf("loaded = %#v, %v", loaded, err)
	}
	updatedDefinition := savedProfileDefinition("Reference crawl")
	updatedDefinition.MaxDepth = 5
	updated, err := library.Update(t.Context(), created.ID, updatedDefinition)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || updated.Profile.MaxDepth != 5 ||
		updated.CreatedAt != created.CreatedAt {
		t.Fatalf("updated = %#v", updated)
	}
	profiles, err := library.Profiles(t.Context())
	if err != nil || len(profiles) != 1 || !reflect.DeepEqual(profiles[0], updated) {
		t.Fatalf("profiles = %#v, %v", profiles, err)
	}
	if err := library.Delete(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if profiles, err := library.Profiles(t.Context()); err != nil || len(profiles) != 0 {
		t.Fatalf("profiles after delete = %#v, %v", profiles, err)
	}
}

func TestProfileLibraryRejectsAmbiguousNamesAndInvalidDefinitions(t *testing.T) {
	library := openProfileLibrary(t)
	first, err := library.Create(t.Context(), savedProfileDefinition("Docs Crawl"))
	if err != nil {
		t.Fatal(err)
	}
	library.entropy = bytes.NewReader(bytes.Repeat([]byte{8}, 4096))
	if _, err := library.Create(
		t.Context(),
		savedProfileDefinition(" docs   crawl "),
	); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate create error = %v", err)
	}
	second, err := library.Create(t.Context(), savedProfileDefinition("Other"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.Update(
		t.Context(),
		second.ID,
		savedProfileDefinition("DOCS CRAWL"),
	); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate update error = %v", err)
	}
	invalid := savedProfileDefinition("Invalid")
	invalid.URLMustMatch = "["
	if _, err := library.Update(t.Context(), first.ID, invalid); err == nil ||
		!strings.Contains(err.Error(), "invalid saved crawl profile") {
		t.Fatalf("invalid profile error = %v", err)
	}
}

func TestProfileLibraryRejectsBadIdentityAndCanceledContext(t *testing.T) {
	library := openProfileLibrary(t)
	created, err := library.Create(t.Context(), savedProfileDefinition("Stored"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.Profile(t.Context(), "bad"); err == nil {
		t.Fatal("bad identity was accepted")
	}
	if _, err := library.Profile(t.Context(), strings.Repeat("0", 32)); err == nil {
		t.Fatal("missing profile was loaded")
	}
	if _, err := library.Update(t.Context(), "bad", savedProfileDefinition("Bad")); err == nil {
		t.Fatal("bad update identity was accepted")
	}
	if _, err := library.Update(
		t.Context(),
		strings.Repeat("0", 32),
		savedProfileDefinition("Missing"),
	); err == nil {
		t.Fatal("missing profile was updated")
	}
	if err := library.Delete(t.Context(), "bad"); err == nil {
		t.Fatal("bad delete identity was accepted")
	}
	if err := library.Delete(t.Context(), strings.Repeat("0", 32)); err == nil {
		t.Fatal("missing profile was deleted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := library.Create(ctx, savedProfileDefinition("Canceled")); err == nil {
		t.Fatal("canceled create succeeded")
	}
	if _, err := library.Profile(ctx, created.ID); err == nil {
		t.Fatal("canceled profile read succeeded")
	}
	if _, err := library.Profiles(ctx); err == nil {
		t.Fatal("canceled profile list succeeded")
	}
}

func TestProfileLibraryRejectsNamesEntropyCollisionsAndCapacity(t *testing.T) {
	library := openProfileLibrary(t)
	for _, name := range []string{"", strings.Repeat("x", MaximumProfileNameBytes+1), string([]byte{0xff})} {
		if _, err := library.Create(t.Context(), savedProfileDefinition(name)); err == nil {
			t.Fatalf("invalid name %q was accepted", name)
		}
	}
	invalid := savedProfileDefinition("Invalid create")
	invalid.URLMustMatch = "["
	if _, err := library.Create(t.Context(), invalid); err == nil {
		t.Fatal("invalid crawl profile was created")
	}
	library.entropy = bytes.NewReader(nil)
	if _, err := library.Create(t.Context(), savedProfileDefinition("No entropy")); err == nil {
		t.Fatal("profile identity without entropy was created")
	}
	library.entropy = bytes.NewReader(bytes.Repeat([]byte{9}, profileIdentityBytes*2))
	if _, err := library.Create(t.Context(), savedProfileDefinition("First identity")); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Create(t.Context(), savedProfileDefinition("Collision")); err == nil {
		t.Fatal("profile identity collision was accepted")
	}

	full := openProfileLibrary(t)
	entropy := make([]byte, 0, MaximumProfiles*profileIdentityBytes+profileIdentityBytes)
	for index := 0; index <= MaximumProfiles; index++ {
		entropy = append(entropy, bytes.Repeat([]byte{byte(index)}, profileIdentityBytes)...)
	}
	full.entropy = bytes.NewReader(entropy)
	for index := 0; index < MaximumProfiles; index++ {
		if _, err := full.Create(
			t.Context(),
			savedProfileDefinition(fmt.Sprintf("Profile %03d", index)),
		); err != nil {
			t.Fatalf("create profile %d: %v", index, err)
		}
	}
	if _, err := full.Create(t.Context(), savedProfileDefinition("Over capacity")); err == nil {
		t.Fatal("profile library exceeded its capacity")
	}
}

func TestProfileLibraryListsNamesInCanonicalOrder(t *testing.T) {
	library := openProfileLibrary(t)
	entropy := make([]byte, 0, 3*profileIdentityBytes)
	for _, value := range []byte{1, 2, 3} {
		entropy = append(entropy, bytes.Repeat([]byte{value}, profileIdentityBytes)...)
	}
	library.entropy = bytes.NewReader(entropy)
	for _, name := range []string{"Zulu", "alpha", "Middle"} {
		if _, err := library.Create(t.Context(), savedProfileDefinition(name)); err != nil {
			t.Fatal(err)
		}
	}
	profiles, err := library.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		names = append(names, profile.Profile.Name)
	}
	if want := []string{"alpha", "Middle", "Zulu"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("profile names = %v, want %v", names, want)
	}
}

func TestSavedProfileCodecRejectsMalformedAndInconsistentRecords(t *testing.T) {
	codec := savedProfileCodec{}
	if _, err := codec.Decode([]byte("{")); err == nil {
		t.Fatal("malformed saved profile was decoded")
	}
	if _, err := codec.Encode(SavedProfile{
		CreatedAt: time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC),
	}); err == nil {
		t.Fatal("unencodable saved profile was accepted")
	}
	profile := yagocrawlcontract.NewCrawlProfile(savedProfileDefinition("Valid"))
	valid := SavedProfile{
		ID: strings.Repeat("a", profileIdentityBytes*2), Profile: profile,
		CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(2, 0).UTC(),
	}
	for _, mutate := range []func(*SavedProfile){
		func(saved *SavedProfile) { saved.CreatedAt = time.Time{} },
		func(saved *SavedProfile) { saved.Profile.Name = " Valid " },
	} {
		candidate := valid
		mutate(&candidate)
		raw, err := codec.Encode(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := codec.Decode(raw); err == nil {
			t.Fatalf("inconsistent profile was decoded: %#v", candidate)
		}
	}
}

func TestProfileLibrarySurfacesStorageFailures(t *testing.T) {
	sentinel := errors.New("storage failed")
	provisionEngine := newProfileLibraryFaultEngine()
	storage, err := vault.New(provisionEngine)
	if err != nil {
		t.Fatal(err)
	}
	provisionEngine.provisionError = sentinel
	if _, err := Open(storage); err == nil {
		t.Fatal("profile bucket provisioning failure was hidden")
	}

	library, engine := openFaultProfileLibrary(t)
	library.entropy = bytes.NewReader(bytes.Repeat([]byte{4}, profileIdentityBytes*8))
	library.now = func() time.Time { return time.Unix(100, 0).UTC() }
	engine.badLength = true
	if _, err := library.Create(t.Context(), savedProfileDefinition("Bad length")); err == nil {
		t.Fatal("profile length failure was hidden")
	}
	engine.badLength = false
	created, err := library.Create(t.Context(), savedProfileDefinition("Stored"))
	if err != nil {
		t.Fatal(err)
	}
	engine.buckets[profileBucket][created.ID] = []byte("{")
	if _, err := library.Update(
		t.Context(),
		created.ID,
		savedProfileDefinition("Updated"),
	); err == nil {
		t.Fatal("corrupt profile update was accepted")
	}
	if _, err := library.Profile(t.Context(), created.ID); err == nil {
		t.Fatal("corrupt profile read was accepted")
	}
	validRaw, err := savedProfileCodec{}.Encode(created)
	if err != nil {
		t.Fatal(err)
	}
	engine.buckets[profileBucket][created.ID] = validRaw
	engine.deleteError = sentinel
	if err := library.Delete(t.Context(), created.ID); err == nil {
		t.Fatal("profile delete failure was hidden")
	}
	engine.deleteError = nil
	engine.scanError = sentinel
	if _, err := library.Profiles(t.Context()); err == nil {
		t.Fatal("profile scan failure was hidden")
	}
}

type profileLibraryFaultEngine struct {
	buckets        map[vault.Name]map[string][]byte
	provisionError error
	deleteError    error
	scanError      error
	badLength      bool
}

func newProfileLibraryFaultEngine() *profileLibraryFaultEngine {
	return &profileLibraryFaultEngine{buckets: make(map[vault.Name]map[string][]byte)}
}

func (engine *profileLibraryFaultEngine) Provision(name vault.Name) error {
	if engine.provisionError != nil {
		return engine.provisionError
	}
	if engine.buckets[name] == nil {
		engine.buckets[name] = make(map[string][]byte)
	}

	return nil
}

func (engine *profileLibraryFaultEngine) Update(
	_ context.Context,
	operation func(vault.EngineTxn) error,
) error {
	return operation(profileLibraryFaultTransaction{engine: engine, writable: true})
}

func (engine *profileLibraryFaultEngine) View(
	_ context.Context,
	operation func(vault.EngineTxn) error,
) error {
	return operation(profileLibraryFaultTransaction{engine: engine})
}

func (*profileLibraryFaultEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }

func (*profileLibraryFaultEngine) QuotaBytes() int64 { return 0 }

func (*profileLibraryFaultEngine) Close() error { return nil }

type profileLibraryFaultTransaction struct {
	engine   *profileLibraryFaultEngine
	writable bool
}

func (transaction profileLibraryFaultTransaction) Bucket(
	name vault.Name,
) vault.EngineBucket {
	return profileLibraryFaultBucket{engine: transaction.engine, name: name}
}

func (transaction profileLibraryFaultTransaction) Writable() bool {
	return transaction.writable
}

type profileLibraryFaultBucket struct {
	engine *profileLibraryFaultEngine
	name   vault.Name
}

func (bucket profileLibraryFaultBucket) Get(key vault.Key) []byte {
	if bucket.engine.badLength && bucket.name == vault.Name("__lengths__") {
		return []byte("bad")
	}

	return bucket.engine.buckets[bucket.name][string(key)]
}

func (bucket profileLibraryFaultBucket) Put(key vault.Key, raw []byte) error {
	bucket.engine.buckets[bucket.name][string(key)] = append([]byte(nil), raw...)

	return nil
}

func (bucket profileLibraryFaultBucket) Delete(key vault.Key) error {
	if bucket.name == profileBucket && bucket.engine.deleteError != nil {
		return bucket.engine.deleteError
	}
	delete(bucket.engine.buckets[bucket.name], string(key))

	return nil
}

func (bucket profileLibraryFaultBucket) Scan(
	prefix vault.Key,
	visit func(vault.Key, []byte) (bool, error),
) error {
	if bucket.name == profileBucket && bucket.engine.scanError != nil {
		return bucket.engine.scanError
	}
	keys := make([]string, 0, len(bucket.engine.buckets[bucket.name]))
	for key := range bucket.engine.buckets[bucket.name] {
		if strings.HasPrefix(key, string(prefix)) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		keepGoing, err := visit(
			vault.Key(key),
			append([]byte(nil), bucket.engine.buckets[bucket.name][key]...),
		)
		if err != nil {
			return err
		}
		if !keepGoing {
			return nil
		}
	}

	return nil
}

func openFaultProfileLibrary(
	t *testing.T,
) (*Library, *profileLibraryFaultEngine) {
	t.Helper()
	engine := newProfileLibraryFaultEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	library, err := Open(storage)
	if err != nil {
		t.Fatal(err)
	}

	return library, engine
}
