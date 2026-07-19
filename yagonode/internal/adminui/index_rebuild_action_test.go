package adminui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakeIndexRebuildScheduler struct {
	pending     bool
	statusErr   error
	scheduleErr error
	calls       int
}

func (f *fakeIndexRebuildScheduler) RebuildPending(context.Context) (bool, error) {
	return f.pending, f.statusErr
}

func (f *fakeIndexRebuildScheduler) ScheduleRebuild(context.Context) error {
	f.calls++
	if f.scheduleErr == nil {
		f.pending = true
	}

	return f.scheduleErr
}

func TestConsoleIndexRendersSafeRestartRebuildControl(t *testing.T) {
	t.Parallel()

	scheduler := &fakeIndexRebuildScheduler{}
	body := do(t, New(Options{
		Index:        fakeIndex{snap: IndexStats{Available: true}},
		IndexRebuild: scheduler,
	}), indexPath).body
	for _, want := range []string{
		"Index maintenance",
		"active index remains untouched until the next node restart",
		`action="/admin/index/rebuild"`,
		"Schedule full rebuild",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rebuild control missing %q", want)
		}
	}

	scheduler.pending = true
	pending := do(t, New(Options{
		Index:        fakeIndex{snap: IndexStats{Available: true}},
		IndexRebuild: scheduler,
	}), indexPath).body
	if !strings.Contains(pending, "is scheduled for the next node restart") ||
		strings.Contains(pending, "Schedule full rebuild</button>") {
		t.Fatalf("pending rebuild state = %s", pending)
	}
}

func TestConsoleIndexRebuildActionReportsOutcome(t *testing.T) {
	t.Parallel()

	scheduler := &fakeIndexRebuildScheduler{}
	console := New(Options{
		Index:        fakeIndex{snap: IndexStats{Available: true}},
		IndexRebuild: scheduler,
	})
	got := doPost(t, console, indexRebuildPath, url.Values{})
	if got.status != http.StatusOK || scheduler.calls != 1 ||
		!strings.Contains(got.body, "Rebuild request accepted") ||
		strings.Count(got.body, "A full search-index rebuild is scheduled") != 1 {
		t.Fatalf(
			"successful schedule = status %d calls %d body %s",
			got.status,
			scheduler.calls,
			got.body,
		)
	}

	scheduler.scheduleErr = errors.New("private path detail")
	failed := doPost(t, console, indexRebuildPath, url.Values{})
	if failed.status != http.StatusOK ||
		!strings.Contains(failed.body, "Could not schedule the rebuild.") ||
		strings.Contains(failed.body, "private path detail") {
		t.Fatalf("failed schedule = status %d body %s", failed.status, failed.body)
	}
}

func TestConsoleIndexRebuildUnavailableStates(t *testing.T) {
	t.Parallel()

	missing := doPost(t, New(Options{}), indexRebuildPath, url.Values{})
	if missing.status != http.StatusNotFound {
		t.Fatalf("missing scheduler status = %d", missing.status)
	}
	body := do(t, New(Options{
		Index: fakeIndex{snap: IndexStats{Available: true}},
		IndexRebuild: &fakeIndexRebuildScheduler{
			statusErr: errors.New("private status detail"),
		},
	}), indexPath).body
	if !strings.Contains(body, "Rebuild status is unavailable.") ||
		strings.Contains(body, "private status detail") ||
		strings.Contains(body, "Schedule full rebuild</button>") {
		t.Fatalf("unavailable status = %s", body)
	}
}
