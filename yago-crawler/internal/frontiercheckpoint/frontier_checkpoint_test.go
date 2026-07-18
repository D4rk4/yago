package frontiercheckpoint

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

var testContext = context.Background()

func testRunTally() yagocrawlcontract.CrawlRunTally {
	return yagocrawlcontract.CrawlRunTally{}
}

func testPageCompletion() PageCompletion {
	return PageCompletion{}
}

func testFailedPageCompletion() PageCompletion {
	return PageCompletion{Tally: yagocrawlcontract.CrawlRunTally{Failed: 1}}
}

func testCheckpointPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "checkpoint", "frontier.db")
}

func openTestCheckpoint(t *testing.T, path string) *FrontierCheckpoint {
	t.Helper()
	checkpoint, err := Open(path)
	if err != nil {
		t.Fatalf("open checkpoint: %v", err)
	}
	t.Cleanup(func() {
		if err := checkpoint.Close(); err != nil {
			t.Errorf("close checkpoint: %v", err)
		}
	})
	return checkpoint
}

func testPage(rawURL, host, observationID string, depth int) Page {
	return Page{
		URL:              rawURL,
		Host:             host,
		Depth:            depth,
		ProfileHandle:    "profile-1",
		ObservationID:    observationID,
		ObservedAt:       time.Date(2026, 7, 16, 10, depth, 0, 0, time.UTC),
		SourceModifiedAt: time.Date(2026, 7, 15, 9, depth, 0, 0, time.UTC),
		Index:            depth%2 == 0,
	}
}

func testRedirect(source Page, finalURL, finalHost string, incrementHost bool) Redirect {
	return Redirect{
		SourceURL:     source.URL,
		FinalURL:      finalURL,
		FinalHost:     finalHost,
		IncrementHost: incrementHost,
	}
}

func beginTestRun(t *testing.T, checkpoint *FrontierCheckpoint, provenance, identity []byte) {
	t.Helper()
	if err := checkpoint.Begin(
		testContext,
		provenance,
		identity,
		yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
	); err != nil {
		t.Fatalf("begin run: %v", err)
	}
}

func requireErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want %v", err, target)
	}
}

func requirePageEqual(t *testing.T, got, want Page) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("page = %+v, want %+v", got, want)
	}
}

func workerSuffix(workerID, prefix string) string {
	return strings.TrimPrefix(workerID, prefix+"-")
}
