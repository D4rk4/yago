package frontiercheckpoint

import (
	"errors"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type partialRecoveryFixture struct {
	path       string
	provenance []byte
	identity   []byte
	first      Page
	second     Page
}

func TestReopenRecoversPartialSeedingAndOutstandingPages(t *testing.T) {
	path := testCheckpointPath(t)
	provenance := []byte("order-a")
	identity := []byte("identity-a")
	first := testPage("https://one.example/a", "one.example", "observation-a", 0)
	second := testPage("https://two.example/b", "two.example", "observation-b", 1)
	checkpoint := openTestCheckpoint(t, path)
	beginTestRun(t, checkpoint, provenance, identity)
	admitted, err := checkpoint.Admit(testContext, provenance, []Page{first, second, first})
	if err != nil || admitted != 2 {
		t.Fatalf("admit = %d, %v, want 2", admitted, err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close before reopen: %v", err)
	}
	requirePartialSeedingAfterReopen(t, partialRecoveryFixture{
		path:       path,
		provenance: provenance,
		identity:   identity,
		first:      first,
		second:     second,
	})
}

func requirePartialSeedingAfterReopen(
	t *testing.T,
	fixture partialRecoveryFixture,
) {
	t.Helper()
	reopened := openTestCheckpoint(t, fixture.path)
	snapshot, err := reopened.Load(testContext, fixture.provenance)
	if err != nil {
		t.Fatalf("load reopened checkpoint: %v", err)
	}
	if !snapshot.Seeding || snapshot.Completed || snapshot.Failed {
		t.Fatalf("partial run flags = %+v", snapshot)
	}
	if snapshot.Counters != (Counters{Pages: 2, Pending: 2}) {
		t.Fatalf("counters = %+v", snapshot.Counters)
	}
	if !reflect.DeepEqual(snapshot.OrderIdentity, fixture.identity) ||
		snapshot.Priority != yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery {
		t.Fatalf("run identity = %q, priority = %q", snapshot.OrderIdentity, snapshot.Priority)
	}
	if len(snapshot.Outstanding) != 2 {
		t.Fatalf("outstanding pages = %d", len(snapshot.Outstanding))
	}
	requirePageEqual(t, snapshot.Outstanding[0], fixture.first)
	requirePageEqual(t, snapshot.Outstanding[1], fixture.second)
	if _, found := snapshot.Visited[fixture.first.URL]; !found {
		t.Errorf("first page missing from exact visited set")
	}
	if _, found := snapshot.Visited[fixture.second.URL]; !found {
		t.Errorf("second page missing from exact visited set")
	}
}

func TestRedirectHostStateAndFailureSurviveReopen(t *testing.T) {
	path := testCheckpointPath(t)
	provenance := []byte("order-b")
	identity := []byte("identity-b")
	page := testPage("https://one.example/start", "one.example", "observation-start", 0)
	checkpoint := openTestCheckpoint(t, path)
	beginTestRun(t, checkpoint, provenance, identity)
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{page},
	); err != nil ||
		admitted != 1 {
		t.Fatalf("admit = %d, %v", admitted, err)
	}
	recorded, err := checkpoint.RecordRedirect(
		testContext,
		provenance,
		testRedirect(page, "https://two.example/final", "two.example", true),
	)
	if err != nil || !recorded {
		t.Fatalf("record redirect = %v, %v", recorded, err)
	}
	if recorded, err = checkpoint.RecordRedirect(
		testContext,
		provenance,
		testRedirect(page, "https://two.example/final", "two.example", true),
	); err != nil || !recorded {
		t.Fatalf("same-source redirect replay = %v, %v", recorded, err)
	}
	if err := checkpoint.RecordHostState(
		testContext, provenance, "two.example", HostProgress{Failures: 3, Retired: true}, nil,
	); err != nil {
		t.Fatalf("record host state: %v", err)
	}
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish seeding: %v", err)
	}
	if err := checkpoint.CompletePage(
		testContext,
		provenance,
		page.URL,
		testFailedPageCompletion(),
	); err != nil {
		t.Fatalf("complete page: %v", err)
	}
	if err := checkpoint.CompletePage(
		testContext,
		provenance,
		page.URL,
		testFailedPageCompletion(),
	); err != nil {
		t.Fatalf("repeat completion: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close before reopen: %v", err)
	}
	requireRedirectHostStateAfterReopen(t, path, provenance)
}

func requireRedirectHostStateAfterReopen(
	t *testing.T,
	path string,
	provenance []byte,
) {
	t.Helper()
	reopened := openTestCheckpoint(t, path)
	snapshot, err := reopened.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !snapshot.Completed || snapshot.Failed || snapshot.Seeding ||
		len(snapshot.Outstanding) != 0 {
		t.Fatalf("completed run = %+v", snapshot)
	}
	if snapshot.Tally.Failed != 1 {
		t.Fatalf("completed tally = %+v, want one failed page", snapshot.Tally)
	}
	if snapshot.Counters != (Counters{Pages: 1, Pending: 0}) {
		t.Fatalf("counters = %+v", snapshot.Counters)
	}
	if snapshot.HostStates["one.example"].Pages != 1 {
		t.Fatalf("source host state = %+v", snapshot.HostStates["one.example"])
	}
	wantFinal := HostState{Pages: 1, Failures: 3, Retired: true}
	if snapshot.HostStates["two.example"] != wantFinal {
		t.Fatalf("final host state = %+v, want %+v", snapshot.HostStates["two.example"], wantFinal)
	}
	if len(snapshot.Visited) != 2 {
		t.Fatalf("visited set = %v", snapshot.Visited)
	}
}

func TestHostRetirementDropsOnlyNamedOutstandingPages(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("order-c")
	beginTestRun(t, checkpoint, provenance, []byte("identity-c"))
	first := testPage("https://one.example/a", "one.example", "observation-a", 0)
	second := testPage("https://one.example/b", "one.example", "observation-b", 1)
	third := testPage("https://two.example/c", "two.example", "observation-c", 2)
	if admitted, err := checkpoint.Admit(
		testContext, provenance, []Page{first, second, third},
	); err != nil || admitted != 3 {
		t.Fatalf("admit = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish seeding: %v", err)
	}
	if err := checkpoint.RecordHostState(
		testContext,
		provenance,
		"one.example",
		HostProgress{Failures: 5, Retired: true},
		[]string{first.URL, second.URL, first.URL},
	); err != nil {
		t.Fatalf("retire host: %v", err)
	}
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if snapshot.Counters != (Counters{Pages: 3, Pending: 1}) || snapshot.Completed {
		t.Fatalf("remaining run = %+v", snapshot)
	}
	if len(snapshot.Outstanding) != 1 {
		t.Fatalf("outstanding pages = %v", snapshot.Outstanding)
	}
	requirePageEqual(t, snapshot.Outstanding[0], third)
	if snapshot.HostStates["one.example"] != (HostState{Pages: 2, Failures: 5, Retired: true}) {
		t.Fatalf("retired host = %+v", snapshot.HostStates["one.example"])
	}
	if err := checkpoint.CompletePage(
		testContext,
		provenance,
		third.URL,
		testPageCompletion(),
	); err != nil {
		t.Fatalf("complete remaining page: %v", err)
	}
	status, err := checkpoint.Status(testContext, provenance, []byte("identity-c"))
	if err != nil || status != RunCompleted {
		t.Fatalf("status = %v, %v", status, err)
	}
}

func TestCompletionMarkerAndAckDeleteAreIdempotent(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	provenance := []byte("order-d")
	identity := []byte("identity-d")
	beginTestRun(t, checkpoint, provenance, identity)
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish empty seeding: %v", err)
	}
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("repeat finish seeding: %v", err)
	}
	status, err := checkpoint.Status(testContext, provenance, identity)
	if err != nil || status != RunCompleted {
		t.Fatalf("completed status = %v, %v", status, err)
	}
	if _, err := checkpoint.Admit(testContext, provenance, []Page{
		testPage("https://late.example/", "late.example", "late-observation", 0),
	}); !errors.Is(err, ErrRunCompleted) {
		t.Fatalf("late admission error = %v", err)
	}
	if _, err := checkpoint.RecordRedirect(
		testContext,
		provenance,
		Redirect{
			SourceURL: "https://source.example/",
			FinalURL:  "https://late.example/",
			FinalHost: "late.example",
		},
	); !errors.Is(err, ErrRunCompleted) {
		t.Fatalf("late redirect error = %v", err)
	}
	if err := checkpoint.Delete(testContext, provenance); err != nil {
		t.Fatalf("delete after ack: %v", err)
	}
	if err := checkpoint.Delete(testContext, provenance); err != nil {
		t.Fatalf("repeat delete after ack: %v", err)
	}
	status, err = checkpoint.Status(testContext, provenance, identity)
	if err != nil || status != RunMissing {
		t.Fatalf("deleted status = %v, %v", status, err)
	}
	if _, err := checkpoint.Load(testContext, provenance); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("load deleted error = %v", err)
	}
}
