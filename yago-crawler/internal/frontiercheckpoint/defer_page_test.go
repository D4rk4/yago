package frontiercheckpoint

import (
	"errors"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
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

func TestDeferPageRejectsInvalidInputsAndRunStates(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("defer-validation")
	validRetry := time.Now().Add(time.Minute)
	for _, test := range []struct {
		name       string
		provenance []byte
		pageURL    string
		attempt    uint8
		retryAt    time.Time
		wantError  error
	}{
		{
			name: "empty provenance", pageURL: "https://example.test/page",
			attempt: 1, retryAt: validRetry, wantError: ErrInvalidProvenance,
		},
		{
			name: "empty URL", provenance: provenance, attempt: 1,
			retryAt: validRetry, wantError: ErrInvalidPage,
		},
		{
			name: "zero attempt", provenance: provenance, pageURL: "https://example.test/page",
			retryAt: validRetry, wantError: ErrInvalidPage,
		},
		{
			name: "zero retry time", provenance: provenance, pageURL: "https://example.test/page",
			attempt: 1, wantError: ErrInvalidPage,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := checkpoint.DeferPage(
				testContext,
				test.provenance,
				test.pageURL,
				test.attempt,
				test.retryAt,
			)
			requireErrorIs(t, err, test.wantError)
		})
	}
	if err := checkpoint.DeferPage(
		testContext,
		provenance,
		"https://example.test/missing-run",
		1,
		validRetry,
	); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("missing run error = %v, want %v", err, ErrRunNotFound)
	}
	activeProvenance := []byte("defer-active-without-page")
	beginTestRun(t, checkpoint, activeProvenance, []byte("active-identity"))
	if err := checkpoint.DeferPage(
		testContext,
		activeProvenance,
		"https://example.test/not-admitted",
		1,
		validRetry,
	); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("missing outstanding page error = %v, want %v", err, ErrInvalidPage)
	}
	completedProvenance := []byte("defer-completed")
	beginTestRun(t, checkpoint, completedProvenance, []byte("identity"))
	if err := checkpoint.FinishSeeding(
		testContext,
		completedProvenance,
		testRunTally(),
	); err != nil {
		t.Fatalf("finish empty run: %v", err)
	}
	if err := checkpoint.DeferPage(
		testContext,
		completedProvenance,
		"https://example.test/completed-run",
		1,
		validRetry,
	); !errors.Is(err, ErrRunCompleted) {
		t.Fatalf("completed run error = %v, want %v", err, ErrRunCompleted)
	}
}

func TestDeferPageRejectsMissingPageBucket(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("defer-missing-bucket")
	page := admittedDeferredTestPage(t, checkpoint, provenance)
	if err := checkpoint.database.Update(func(transaction *bolt.Tx) error {
		return transaction.DeleteBucket(pagesBucket)
	}); err != nil {
		t.Fatalf("remove page bucket: %v", err)
	}
	if err := checkpoint.DeferPage(
		testContext,
		provenance,
		page.URL,
		1,
		time.Now().Add(time.Minute),
	); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("missing page bucket error = %v", err)
	}
}

func TestDeferPageRejectsCorruptPagePosition(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("defer-corrupt-position")
	page := admittedDeferredTestPage(t, checkpoint, provenance)
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		t.Fatalf("provenance prefix: %v", err)
	}
	if err := checkpoint.database.Update(func(transaction *bolt.Tx) error {
		return transaction.Bucket(pagePositionsBucket).Put(
			childRowKey(prefix, page.URL),
			[]byte{1},
		)
	}); err != nil {
		t.Fatalf("corrupt page position: %v", err)
	}
	if err := checkpoint.DeferPage(
		testContext,
		provenance,
		page.URL,
		1,
		time.Now().Add(time.Minute),
	); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("corrupt page position error = %v", err)
	}
}

func TestDeferPageRejectsUnrepresentableRetryTime(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("defer-unrepresentable-time")
	page := admittedDeferredTestPage(t, checkpoint, provenance)
	retryAfter := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
	if err := checkpoint.DeferPage(
		testContext,
		provenance,
		page.URL,
		1,
		retryAfter,
	); err == nil {
		t.Fatal("unrepresentable retry time was stored")
	}
	snapshot, err := checkpoint.LoadBounded(testContext, provenance, 10)
	if err != nil {
		t.Fatalf("load unchanged page: %v", err)
	}
	if got := snapshot.Outstanding[0]; got.RetryAttempt != 0 || !got.RetryAfter.IsZero() {
		t.Fatalf("failed encoding changed page: %+v", got)
	}
}

func admittedDeferredTestPage(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
) Page {
	t.Helper()
	beginTestRun(t, checkpoint, provenance, []byte("identity"))
	page := testPage("https://example.test/page", "example.test", "observation", 0)
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{page},
	); err != nil ||
		admitted != 1 {
		t.Fatalf("admit page = %d, %v", admitted, err)
	}
	return page
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
