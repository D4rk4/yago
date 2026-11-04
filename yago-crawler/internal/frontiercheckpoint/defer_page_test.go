package frontiercheckpoint

import (
	"errors"
	"testing"
	"time"
)

func TestDeferPagePersistsNextAttemptWithoutCompletingRun(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("deferred-page")
	beginTestRun(t, checkpoint, provenance, []byte("identity"))
	page := testPage("https://example.test/a", "example.test", "observation", 0)
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{page},
	); err != nil ||
		admitted != 1 {
		t.Fatalf("admit page = %d, %v", admitted, err)
	}
	retryAfter := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	if err := checkpoint.DeferPage(testContext, provenance, page.URL, 1, retryAfter); err != nil {
		t.Fatalf("defer page: %v", err)
	}
	snapshot, err := checkpoint.LoadBounded(testContext, provenance, 10)
	if err != nil {
		t.Fatalf("load deferred page: %v", err)
	}
	if snapshot.Counters.Pending != 1 || len(snapshot.Outstanding) != 1 {
		t.Fatalf(
			"deferred run state = pending %d, outstanding %d",
			snapshot.Counters.Pending,
			len(snapshot.Outstanding),
		)
	}
	got := snapshot.Outstanding[0]
	if got.RetryAttempt != 1 || !got.RetryAfter.Equal(retryAfter) {
		t.Fatalf("deferred page = %+v", got)
	}
}

func TestDeferPageRejectsSkippedAttemptWithoutChangingPage(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("invalid-deferred-page")
	beginTestRun(t, checkpoint, provenance, []byte("identity"))
	page := testPage("https://example.test/a", "example.test", "observation", 0)
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{page},
	); err != nil ||
		admitted != 1 {
		t.Fatalf("admit page = %d, %v", admitted, err)
	}
	if err := checkpoint.DeferPage(
		testContext,
		provenance,
		page.URL,
		2,
		time.Now().Add(time.Minute),
	); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("skipped retry attempt error = %v", err)
	}
	snapshot, err := checkpoint.LoadBounded(testContext, provenance, 10)
	if err != nil {
		t.Fatalf("load unchanged page: %v", err)
	}
	if got := snapshot.Outstanding[0]; got.RetryAttempt != 0 || !got.RetryAfter.IsZero() {
		t.Fatalf("rejected transition changed page: %+v", got)
	}
}

func TestLegacyCheckpointPageDefaultsToFirstPass(t *testing.T) {
	var page Page
	if err := decodeRow("legacy page", []byte(
		`{"URL":"https://example.test/legacy","Host":"example.test","Depth":1,"ProfileHandle":"focused"}`,
	), &page); err != nil {
		t.Fatalf("decode legacy page: %v", err)
	}
	if page.URL != "https://example.test/legacy" || page.RetryAttempt != 0 ||
		!page.RetryAfter.IsZero() {
		t.Fatalf("legacy page = %+v", page)
	}
}
