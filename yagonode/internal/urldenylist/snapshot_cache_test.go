package urldenylist_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestSnapshotCacheTracksCommittedMutations(t *testing.T) {
	store := openStore(t)
	ctx := t.Context()
	if err := store.Add(ctx, urldenylist.KindDomain, "blocked.example"); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(ctx, urldenylist.KindURL, "https://allowed.example/blocked"); err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	if !snapshot.Blocks("https://sub.blocked.example/") ||
		!snapshot.Blocks("https://allowed.example/blocked") {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if removed, err := store.Remove(
		ctx,
		urldenylist.KindDomain,
		"blocked.example",
	); err != nil ||
		!removed {
		t.Fatalf("remove domain = %t, %v", removed, err)
	}
	if removed, err := store.Remove(
		ctx,
		urldenylist.KindURL,
		"https://allowed.example/blocked",
	); err != nil ||
		!removed {
		t.Fatalf("remove url = %t, %v", removed, err)
	}
	snapshot = store.Snapshot()
	if !snapshot.IsEmpty() {
		t.Fatalf("snapshot after removal = %#v", snapshot)
	}
}

func TestSnapshotBlocksCanonicalURLAliasesUntilEveryRawRuleIsRemoved(t *testing.T) {
	store := openStore(t)
	first := "https://EXAMPLE.test:443/a/../blocked?utm_source=synthetic"
	second := "https://example.test/blocked?utm_campaign=synthetic"
	canonical := "https://example.test/blocked"
	for _, value := range []string{first, second} {
		if err := store.Add(t.Context(), urldenylist.KindURL, value); err != nil {
			t.Fatalf("add URL rule: %v", err)
		}
	}

	if !store.Snapshot().Blocks(canonical) ||
		store.Snapshot().Blocks("https://example.test/allowed") {
		t.Fatal("canonical exact URL rule did not match only the equivalent URL")
	}
	if removed, err := store.Remove(
		t.Context(),
		urldenylist.KindURL,
		first,
	); err != nil || !removed {
		t.Fatalf("remove first URL alias = %t, %v", removed, err)
	}
	if !store.Snapshot().Blocks(canonical) {
		t.Fatal("removing one raw alias unblocked a retained equivalent rule")
	}
	if removed, err := store.Remove(
		t.Context(),
		urldenylist.KindURL,
		second,
	); err != nil || !removed {
		t.Fatalf("remove second URL alias = %t, %v", removed, err)
	}
	if store.Snapshot().Blocks(canonical) {
		t.Fatal("canonical URL remained blocked after every equivalent rule was removed")
	}
}

func TestAddEnforcesDenylistEntryAndCountBoundsAtomically(t *testing.T) {
	engine := newFakeEngine()
	seedDenylist(
		engine,
		urldenylist.KindDomain,
		domainEntries(yagocrawlcontract.MaximumCrawlURLDenylistEntries),
	)
	now := time.Unix(100, 0).UTC()
	store := openSeededStore(t, engine, func() time.Time { return now })
	duplicate := "domain-0000.example"
	now = now.Add(time.Minute)
	if err := store.Add(t.Context(), urldenylist.KindDomain, duplicate); err != nil {
		t.Fatalf("refresh at entry limit: %v", err)
	}
	entries, err := store.Entries(t.Context())
	if err != nil {
		t.Fatalf("entries after refresh: %v", err)
	}
	if len(entries) != yagocrawlcontract.MaximumCrawlURLDenylistEntries ||
		entries[0].AddedAt != now {
		t.Fatalf("entry count or refreshed timestamp = %d, %v", len(entries), entries[0].AddedAt)
	}
	before := store.Snapshot()
	if err := store.Add(t.Context(), urldenylist.KindDomain, "new.example"); err == nil {
		t.Fatal("add beyond the protocol entry limit succeeded")
	}
	entries, err = store.Entries(t.Context())
	if err != nil {
		t.Fatalf("entries after rejected add: %v", err)
	}
	if len(entries) != yagocrawlcontract.MaximumCrawlURLDenylistEntries ||
		!before.Blocks("https://domain-0000.example/") ||
		store.Snapshot().Blocks("https://new.example/") {
		t.Fatal("rejected add changed durable entries or the published snapshot")
	}
	invalidEntries := []struct {
		kind  urldenylist.Kind
		value string
	}{
		{urldenylist.KindURL, strings.Repeat("x", yagocrawlcontract.MaximumCrawlURLBytes+1)},
		{urldenylist.KindURL, string([]byte{0xff})},
		{
			urldenylist.KindDomain,
			strings.Repeat("d", yagocrawlcontract.MaximumCrawlURLDenylistDomainBytes+1),
		},
	}
	for _, entry := range invalidEntries {
		if err := openStore(t).Add(t.Context(), entry.kind, entry.value); err == nil {
			t.Fatalf("invalid raw %s entry was accepted", entry.kind)
		}
	}
}

func TestAddAcceptsMaximumRawDomainLength(t *testing.T) {
	store := openStore(t)
	maximumDomain := strings.Repeat("d", yagocrawlcontract.MaximumCrawlURLDenylistDomainBytes)
	if err := store.Add(t.Context(), urldenylist.KindDomain, maximumDomain); err != nil {
		t.Fatalf("add maximum-length domain: %v", err)
	}
	if !store.Snapshot().Blocks("https://" + maximumDomain + "/") {
		t.Fatal("maximum-length domain rule was not published")
	}
}

func TestAddRejectsConcurrentAliasesPastRawEntryLimit(t *testing.T) {
	engine := newFakeEngine()
	seedDenylist(
		engine,
		urldenylist.KindDomain,
		domainEntries(yagocrawlcontract.MaximumCrawlURLDenylistEntries-1),
	)
	store := openSeededStore(t, engine, time.Now)
	values := []string{
		"https://EXAMPLE.test:443/a/../blocked?utm_source=synthetic",
		"https://example.test/blocked?utm_campaign=synthetic",
	}
	start := make(chan struct{})
	results := make(chan error, len(values))
	for _, value := range values {
		go func() {
			<-start
			results <- store.Add(t.Context(), urldenylist.KindURL, value)
		}()
	}
	close(start)

	succeeded := 0
	failed := 0
	for range values {
		if err := <-results; err != nil {
			failed++
		} else {
			succeeded++
		}
	}
	entries, err := store.Entries(t.Context())
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if succeeded != 1 || failed != 1 ||
		len(entries) != yagocrawlcontract.MaximumCrawlURLDenylistEntries {
		t.Fatalf(
			"concurrent adds: succeeded=%d failed=%d entries=%d",
			succeeded,
			failed,
			len(entries),
		)
	}
}

func TestAddAcceptsExactByteLimitAndRejectsOverflow(t *testing.T) {
	engine := newFakeEngine()
	values := make(
		[]string,
		yagocrawlcontract.MaximumCrawlURLDenylistBytes/
			(yagocrawlcontract.MaximumCrawlURLBytes+5),
	)
	for index := range values {
		values[index] = longURL(index)
	}
	seedDenylist(engine, urldenylist.KindURL, values)
	store := openSeededStore(t, engine, time.Now)
	remainingURLBytes := yagocrawlcontract.MaximumCrawlURLDenylistBytes -
		len(values)*(yagocrawlcontract.MaximumCrawlURLBytes+5) - 5
	byteLimitURL := urlOfLength(len(values), remainingURLBytes)
	if err := store.Add(t.Context(), urldenylist.KindURL, byteLimitURL); err != nil {
		t.Fatalf("add at exact protocol byte limit: %v", err)
	}
	tooManyBytes := "https://example.test/overflow"
	if err := store.Add(t.Context(), urldenylist.KindURL, tooManyBytes); err == nil {
		t.Fatal("add beyond protocol byte limit succeeded")
	}
	entries, err := store.Entries(t.Context())
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != len(values)+1 || !store.Snapshot().Blocks(values[0]) ||
		!store.Snapshot().Blocks(byteLimitURL) ||
		store.Snapshot().Blocks(tooManyBytes) {
		t.Fatal("byte-limit add or rejection produced the wrong durable entries or snapshot")
	}
}

func TestCanonicalTrackingRemovalCannotBypassRawPolicyByteLimit(t *testing.T) {
	engine := newFakeEngine()
	values := make([]string, 510)
	for index := range values {
		values[index] = longTrackedURL(index)
	}
	canonical, ok := yagocrawlcontract.CanonicalURL(values[0])
	if !ok || len(canonical) >= yagocrawlcontract.MaximumCrawlURLBytes/2 {
		t.Fatalf("tracking variant canonical URL length = %d", len(canonical))
	}
	seedDenylist(engine, urldenylist.KindURL, values)
	store := openSeededStore(t, engine, time.Now)
	newAlias := longTrackedURL(len(values))
	if err := store.Add(t.Context(), urldenylist.KindURL, newAlias); err == nil {
		t.Fatal("canonical tracking removal bypassed the raw policy byte limit")
	}
	entries, err := store.Entries(t.Context())
	if err != nil || len(entries) != len(values) ||
		!store.Snapshot().Blocks(canonical) ||
		store.Snapshot().Blocks(mustCanonical(t, newAlias)) {
		t.Fatal("raw byte-limit rejection changed durable entries or the canonical snapshot")
	}
}

func TestAddRejectsCanonicalExpansionPastPerEntryLimit(t *testing.T) {
	store := openStore(t)
	prefix := "https://example.test/"
	value := prefix + strings.Repeat(
		"a",
		yagocrawlcontract.MaximumCrawlURLBytes-len(prefix)-len("é"),
	) + "é"
	canonical, ok := yagocrawlcontract.CanonicalURL(value)
	if !ok || len(value) != yagocrawlcontract.MaximumCrawlURLBytes ||
		len(canonical) <= yagocrawlcontract.MaximumCrawlURLBytes {
		t.Fatalf("test URL lengths raw=%d canonical=%d", len(value), len(canonical))
	}
	if err := store.Add(t.Context(), urldenylist.KindURL, value); err == nil {
		t.Fatal("URL whose canonical wire form exceeds the per-entry limit was accepted")
	}
	if !store.Snapshot().IsEmpty() {
		t.Fatal("rejected canonical expansion changed the published snapshot")
	}
	entries, err := store.Entries(t.Context())
	if err != nil || len(entries) != 0 {
		t.Fatalf("durable entries after rejected canonical expansion = %d, %v", len(entries), err)
	}
}

func TestLegacyOverLimitRulesRemainAvailableAndRemovable(t *testing.T) {
	engine := newFakeEngine()
	seedDenylist(
		engine,
		urldenylist.KindDomain,
		domainEntries(yagocrawlcontract.MaximumCrawlURLDenylistEntries+1),
	)
	store := openSeededStore(t, engine, time.Now)
	if !store.Snapshot().Blocks("https://domain-0000.example/") {
		t.Fatal("opening an over-limit legacy policy dropped a blocking rule")
	}
	if err := store.Add(t.Context(), urldenylist.KindDomain, "new.example"); err == nil {
		t.Fatal("add to an over-limit legacy policy succeeded")
	}
	for _, value := range []string{"domain-0000.example", "domain-0001.example"} {
		removed, err := store.Remove(t.Context(), urldenylist.KindDomain, value)
		if err != nil || !removed {
			t.Fatalf("remove legacy entry %q = %t, %v", value, removed, err)
		}
	}
	if err := store.Add(t.Context(), urldenylist.KindDomain, "new.example"); err != nil {
		t.Fatalf("add after legacy policy returned within bounds: %v", err)
	}
	if store.Snapshot().Blocks("https://domain-0000.example/") ||
		!store.Snapshot().Blocks("https://domain-0002.example/") ||
		!store.Snapshot().Blocks("https://new.example/") {
		t.Fatal("legacy removal or bounded policy recovery changed unrelated rules")
	}
}

func TestLegacyInvalidRawURLRemainsBlockedAndCanBeRemoved(t *testing.T) {
	engine := newFakeEngine()
	values := []string{
		strings.Repeat("x", yagocrawlcontract.MaximumCrawlURLBytes+1),
		string([]byte{'h', 't', 't', 'p', ':', '/', '/', 0xff}),
	}
	seedDenylist(engine, urldenylist.KindURL, values)
	store := openSeededStore(t, engine, time.Now)
	for _, value := range values {
		if !store.Snapshot().Blocks(value) {
			t.Fatal("opening the legacy policy dropped an invalid raw blocking key")
		}
	}
	if err := store.Add(t.Context(), urldenylist.KindDomain, "new.example"); err == nil {
		t.Fatal("add alongside an invalid legacy URL succeeded")
	}
	for _, value := range values {
		removed, err := store.Remove(t.Context(), urldenylist.KindURL, value)
		if err != nil || !removed {
			t.Fatalf("remove invalid legacy URL = %t, %v", removed, err)
		}
	}
	if err := store.Add(t.Context(), urldenylist.KindDomain, "new.example"); err != nil {
		t.Fatalf("add after invalid legacy URLs were removed: %v", err)
	}
}

func seedDenylist(engine *fakeEngine, kind urldenylist.Kind, values []string) {
	records := make(map[string][]byte, len(values))
	for _, value := range values {
		records[string(kind)+"\x00"+value] = []byte(`{}`)
	}
	engine.buckets["urldenylist"] = records
}

func domainEntries(size int) []string {
	entries := make([]string, size)
	for index := range entries {
		entries[index] = fmt.Sprintf("domain-%04d.example", index)
	}

	return entries
}

func longURL(index int) string {
	return urlOfLength(index, yagocrawlcontract.MaximumCrawlURLBytes)
}

func urlOfLength(index, size int) string {
	prefix := "https://example.test/"
	suffix := fmt.Sprintf("%08x", index)

	return prefix + strings.Repeat("a", size-len(prefix)-len(suffix)) + suffix
}

func longTrackedURL(index int) string {
	prefix := fmt.Sprintf("https://example.test/page-%08x?utm_source=", index)

	return prefix + strings.Repeat("t", yagocrawlcontract.MaximumCrawlURLBytes-len(prefix))
}

func mustCanonical(t *testing.T, value string) string {
	t.Helper()
	canonical, ok := yagocrawlcontract.CanonicalURL(value)
	if !ok {
		t.Fatal("test URL was not canonicalizable")
	}

	return canonical
}

func openSeededStore(t *testing.T, engine *fakeEngine, now func() time.Time) *urldenylist.Store {
	t.Helper()
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	store, err := urldenylist.Open(v, now)
	if err != nil {
		t.Fatalf("urldenylist.Open: %v", err)
	}

	return store
}

func TestSnapshotCachePublishesImmutableVersions(t *testing.T) {
	store := openStore(t)
	ctx := t.Context()
	if err := store.Add(ctx, urldenylist.KindDomain, "first.example"); err != nil {
		t.Fatal(err)
	}
	first := store.Snapshot()
	if _, err := store.Remove(ctx, urldenylist.KindDomain, "first.example"); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(ctx, urldenylist.KindDomain, "second.example"); err != nil {
		t.Fatal(err)
	}
	second := store.Snapshot()
	if !first.Blocks("https://first.example/") || first.Blocks("https://second.example/") {
		t.Fatalf("first snapshot changed after publication: %#v", first)
	}
	if second.Blocks("https://first.example/") || !second.Blocks("https://second.example/") {
		t.Fatalf("second snapshot = %#v", second)
	}
}

func TestSnapshotCacheChangesOnlyAfterDurableMutation(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.failPut = true
	if err := store.Add(t.Context(), urldenylist.KindDomain, "blocked.example"); err == nil {
		t.Fatal("failed add succeeded")
	}
	snapshot := store.Snapshot()
	if !snapshot.IsEmpty() {
		t.Fatalf("snapshot after failed add = %#v", snapshot)
	}
	engine.failPut = false
	if err := store.Add(t.Context(), urldenylist.KindDomain, "blocked.example"); err != nil {
		t.Fatal(err)
	}
	engine.failDel = true
	if _, err := store.Remove(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	); err == nil {
		t.Fatal("failed remove succeeded")
	}
	snapshot = store.Snapshot()
	if !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after failed remove = %#v", snapshot)
	}
}

func TestSnapshotCacheLoadsPersistedEntries(t *testing.T) {
	engine := newFakeEngine()
	engine.buckets["urldenylist"] = map[string][]byte{
		"domain\x00blocked.example":            []byte(`{"addedAt":"2026-07-13T00:00:00Z"}`),
		"url\x00https://exact.example/blocked": []byte(`{"addedAt":"2026-07-13T00:00:00Z"}`),
	}
	v, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	store, err := urldenylist.Open(v, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	if !snapshot.Blocks("https://sub.blocked.example/") ||
		!snapshot.Blocks("https://exact.example/blocked") {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestAddRejectsProspectivePolicyWithUnknownStoredKind(t *testing.T) {
	engine := newFakeEngine()
	engine.buckets["urldenylist"] = map[string][]byte{
		"unknown\x00entry": []byte(`{}`),
	}
	store := openSeededStore(t, engine, time.Now)
	before, err := store.Entries(t.Context())
	if err != nil || len(before) != 1 || before[0].Kind != urldenylist.Kind("unknown") {
		t.Fatalf("initial unknown-kind entries = %#v, %v", before, err)
	}
	if err := store.Add(t.Context(), urldenylist.KindDomain, "new.example"); err == nil {
		t.Fatal("add alongside an unknown stored kind succeeded")
	}
	after, err := store.Entries(t.Context())
	if err != nil || len(after) != 1 || after[0] != before[0] ||
		store.Snapshot().Blocks("https://new.example/") {
		t.Fatalf("state after rejected unknown-kind policy = %#v, %v", after, err)
	}
}

func TestSnapshotBlocksCanonicalPersistedDomainAliases(t *testing.T) {
	engine := newFakeEngine()
	seedDenylist(engine, urldenylist.KindDomain, []string{"EXAMPLE.test."})
	store := openSeededStore(t, engine, time.Now)
	if !store.Snapshot().Blocks("https://sub.example.test/path") {
		t.Fatal("canonical persisted domain did not block its subdomain")
	}
	if store.Snapshot().Blocks("https://notexample.test/path") {
		t.Fatal("canonical persisted domain blocked an unrelated host")
	}
}

func TestSnapshotRejectsWirePolicyWithUnnormalizableLegacyDomain(t *testing.T) {
	engine := newFakeEngine()
	seedDenylist(engine, urldenylist.KindDomain, []string{"."})
	store := openSeededStore(t, engine, time.Now)
	if _, err := store.Snapshot().CrawlURLDenylist(); err == nil {
		t.Fatal("dot-only legacy domain was silently dropped from the wire policy")
	}
	if err := store.Add(t.Context(), urldenylist.KindURL, "https://new.example/path"); err == nil {
		t.Fatal("add alongside an unnormalizable legacy domain succeeded")
	}
	entries, err := store.Entries(t.Context())
	if err != nil || len(entries) != 1 || entries[0].Kind != urldenylist.KindDomain ||
		entries[0].Value != "." {
		t.Fatalf("legacy domain state after rejected add = %#v, %v", entries, err)
	}
}

func TestLegacyInvalidRawDomainRemainsBlockedAndCanBeRemoved(t *testing.T) {
	value := strings.Repeat("d", yagocrawlcontract.MaximumCrawlURLDenylistDomainBytes+1)
	engine := newFakeEngine()
	seedDenylist(engine, urldenylist.KindDomain, []string{value})
	store := openSeededStore(t, engine, time.Now)
	target := "https://" + value + "/"
	if !store.Snapshot().Blocks(target) {
		t.Fatal("opening the legacy policy dropped an over-limit domain rule")
	}
	if err := store.Add(t.Context(), urldenylist.KindURL, "https://new.example/path"); err == nil {
		t.Fatal("add alongside an over-limit legacy domain succeeded")
	}
	entries, err := store.Entries(t.Context())
	if err != nil || len(entries) != 1 || entries[0].Value != value ||
		!store.Snapshot().Blocks(target) ||
		store.Snapshot().Blocks("https://new.example/path") {
		t.Fatal("rejected add changed the legacy domain or published snapshot")
	}
	if removed, err := store.Remove(
		t.Context(),
		urldenylist.KindDomain,
		value,
	); err != nil || !removed {
		t.Fatalf("remove over-limit legacy domain = %t, %v", removed, err)
	}
	if err := store.Add(t.Context(), urldenylist.KindURL, "https://new.example/path"); err != nil {
		t.Fatalf("add after legacy domain removal: %v", err)
	}
}

func TestSnapshotLoadsPersistedURLAliasesAndKeepsTheRemainingRule(t *testing.T) {
	engine := newFakeEngine()
	values := []string{
		"https://EXAMPLE.test:443/a/../blocked?utm_source=synthetic",
		"https://example.test/blocked?utm_campaign=synthetic",
	}
	seedDenylist(engine, urldenylist.KindURL, values)
	store := openSeededStore(t, engine, time.Now)
	canonical := "https://example.test/blocked"
	if !store.Snapshot().Blocks(canonical) {
		t.Fatal("restored canonical aliases did not block the normalized URL")
	}
	if removed, err := store.Remove(
		t.Context(),
		urldenylist.KindURL,
		values[0],
	); err != nil || !removed {
		t.Fatalf("remove persisted alias = %t, %v", removed, err)
	}
	if !store.Snapshot().Blocks(canonical) {
		t.Fatal("removing one persisted alias unblocked another retained rule")
	}
}

func TestSnapshotCacheReconcilesDurableMutationAfterUpdateError(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.updateErr = context.DeadlineExceeded
	if err := store.Add(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	); err == nil {
		t.Fatal("partially committed add succeeded")
	}
	snapshot := store.Snapshot()
	if !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after committed add = %#v", snapshot)
	}
	if _, err := store.Remove(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	); err == nil {
		t.Fatal("partially committed remove succeeded")
	}
	snapshot = store.Snapshot()
	if !snapshot.IsEmpty() {
		t.Fatalf("snapshot after committed remove = %#v", snapshot)
	}
}

func TestSnapshotCacheFailsClosedWhenMutationStateCannotBeRead(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.updateErr = context.DeadlineExceeded
	engine.failScan = true
	if err := store.Add(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	); err == nil {
		t.Fatal("indeterminate add succeeded")
	}
	snapshot := store.Snapshot()
	if !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after indeterminate add = %#v", snapshot)
	}
	if _, err := store.Remove(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	); err == nil {
		t.Fatal("indeterminate remove succeeded")
	}
	snapshot = store.Snapshot()
	if !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after indeterminate remove = %#v", snapshot)
	}
	if err := store.Add(
		t.Context(),
		urldenylist.KindURL,
		"https://exact.example/path",
	); err == nil {
		t.Fatal("indeterminate URL add succeeded")
	}
	if !store.Snapshot().Blocks("https://exact.example/path") {
		t.Fatal("snapshot after indeterminate URL add lost the blocking rule")
	}
}

func TestSnapshotCacheClearsIndeterminateAddAfterSuccessfulRemoval(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.failPut = true
	engine.failScan = true
	if err := store.Add(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	); err == nil {
		t.Fatal("indeterminate add succeeded")
	}
	snapshot := store.Snapshot()
	if !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after indeterminate add = %#v", snapshot)
	}
	engine.failPut = false
	engine.failScan = false
	removed, err := store.Remove(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	)
	if err != nil || removed {
		t.Fatalf("remove absent durable record = %t, %v", removed, err)
	}
	snapshot = store.Snapshot()
	if !snapshot.IsEmpty() {
		t.Fatalf("snapshot after recovery = %#v", snapshot)
	}
}
