package frontiercheckpoint

import (
	"errors"
	"sync"
	"testing"
)

func TestRedirectReservationSurvivesReopenAndSameSourceReplay(t *testing.T) {
	path := testCheckpointPath(t)
	provenance := []byte("redirect-reopen")
	source := testPage("https://source.example/start", "source.example", "redirect-observation", 0)
	redirect := testRedirect(source, "https://final.example/page", "final.example", true)
	checkpoint := openTestCheckpoint(t, path)
	beginTestRun(t, checkpoint, provenance, []byte("redirect-identity"))
	if admitted, err := checkpoint.Admit(
		testContext, provenance, []Page{source},
	); err != nil || admitted != 1 {
		t.Fatalf("admit source = %d, %v", admitted, err)
	}
	if admitted, err := checkpoint.RecordRedirect(
		testContext,
		provenance,
		redirect,
	); err != nil ||
		!admitted {
		t.Fatalf("record redirect = %v, %v", admitted, err)
	}
	assertRedirectSnapshot(t, checkpoint, provenance, source, redirect)
	if admitted, err := checkpoint.RecordRedirect(
		testContext,
		provenance,
		redirect,
	); err != nil ||
		!admitted {
		t.Fatalf("same-process replay = %v, %v", admitted, err)
	}
	assertRedirectSnapshot(t, checkpoint, provenance, source, redirect)
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close before replay: %v", err)
	}

	reopened := openTestCheckpoint(t, path)
	assertRedirectSnapshot(t, reopened, provenance, source, redirect)
	if admitted, err := reopened.RecordRedirect(
		testContext,
		provenance,
		redirect,
	); err != nil ||
		!admitted {
		t.Fatalf("reopened replay = %v, %v", admitted, err)
	}
	assertRedirectSnapshot(t, reopened, provenance, source, redirect)
}

func TestConcurrentSameSourceRedirectReplayIncrementsHostOnce(t *testing.T) {
	checkpoint, provenance, source, redirect := redirectCheckpoint(t)
	const callers = 16
	errorsSeen := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			admitted, err := checkpoint.RecordRedirect(testContext, provenance, redirect)
			if err != nil {
				errorsSeen <- err
				return
			}
			if !admitted {
				errorsSeen <- errors.New("same-source redirect replay was rejected")
			}
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("concurrent replay: %v", err)
	}
	assertRedirectSnapshot(t, checkpoint, provenance, source, redirect)
}

func assertRedirectSnapshot(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
	source Page,
	redirect Redirect,
) {
	t.Helper()
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load redirect snapshot: %v", err)
	}
	if len(snapshot.Outstanding) != 1 {
		t.Fatalf("outstanding pages = %v", snapshot.Outstanding)
	}
	want := source
	want.RedirectURL = redirect.FinalURL
	want.RedirectHost = redirect.FinalHost
	want.RedirectHostBump = redirect.IncrementHost
	requirePageEqual(t, snapshot.Outstanding[0], want)
	if _, found := snapshot.Visited[redirect.FinalURL]; !found {
		t.Fatalf("redirect target %q is not reserved", redirect.FinalURL)
	}
	if snapshot.HostStates[redirect.FinalHost].Pages != 1 {
		t.Fatalf("redirect host state = %+v", snapshot.HostStates[redirect.FinalHost])
	}
}

func TestRedirectReservationReplacesChangedTargetAndRejectsDifferentSource(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("redirect-owner")
	first := testPage("https://one.example/start", "one.example", "first-observation", 0)
	second := testPage("https://two.example/start", "two.example", "second-observation", 0)
	beginTestRun(t, checkpoint, provenance, []byte("redirect-owner-identity"))
	if admitted, err := checkpoint.Admit(
		testContext, provenance, []Page{first, second},
	); err != nil || admitted != 2 {
		t.Fatalf("admit redirect sources = %d, %v", admitted, err)
	}
	firstRedirect := testRedirect(first, "https://final.example/page", "final.example", true)
	if admitted, err := checkpoint.RecordRedirect(
		testContext, provenance, firstRedirect,
	); err != nil || !admitted {
		t.Fatalf("record first redirect = %v, %v", admitted, err)
	}
	changed := testRedirect(first, "https://changed.example/page", "changed.example", true)
	if admitted, err := checkpoint.RecordRedirect(
		testContext,
		provenance,
		changed,
	); err != nil || !admitted {
		t.Fatalf("changed redirect = %v, %v", admitted, err)
	}
	secondRedirect := testRedirect(second, changed.FinalURL, changed.FinalHost, true)
	if admitted, err := checkpoint.RecordRedirect(
		testContext, provenance, secondRedirect,
	); err != nil || admitted {
		t.Fatalf("different-source redirect = %v, %v", admitted, err)
	}
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load redirect owners: %v", err)
	}
	if snapshot.Outstanding[0].RedirectURL != changed.FinalURL ||
		snapshot.Outstanding[1].RedirectURL != "" {
		t.Fatalf("redirect owners = %+v", snapshot.Outstanding)
	}
	if snapshot.HostStates[firstRedirect.FinalHost].Pages != 0 ||
		snapshot.HostStates[changed.FinalHost].Pages != 1 {
		t.Fatalf("redirect host totals = %+v", snapshot.HostStates)
	}
	if _, found := snapshot.Visited[firstRedirect.FinalURL]; found {
		t.Fatalf("superseded redirect remained visited: %v", snapshot.Visited)
	}
}

func TestRedirectRequiresOutstandingSource(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("redirect-source")
	source := testPage("https://source.example/", "source.example", "source-observation", 0)
	beginTestRun(t, checkpoint, provenance, []byte("redirect-source-identity"))
	if admitted, err := checkpoint.Admit(
		testContext, provenance, []Page{source},
	); err != nil || admitted != 1 {
		t.Fatalf("admit source = %d, %v", admitted, err)
	}
	if err := checkpoint.CompletePage(
		testContext,
		provenance,
		source.URL,
		testPageCompletion(),
	); err != nil {
		t.Fatalf("complete source: %v", err)
	}
	redirect := testRedirect(source, "https://final.example/", "final.example", true)
	if _, err := checkpoint.RecordRedirect(
		testContext,
		provenance,
		redirect,
	); !errors.Is(
		err,
		ErrCorruptCheckpoint,
	) {
		t.Fatalf("missing source error = %v", err)
	}
}
