package frontiercheckpoint

import (
	"errors"
	"math"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestRedirectValidationAcceptsNoRedirectAndRejectsIncompleteTargets(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	admitted, err := checkpoint.RecordRedirect(
		testContext,
		provenance,
		Redirect{SourceURL: page.URL},
	)
	if err != nil || !admitted {
		t.Fatalf("no-redirect outcome = %v, %v", admitted, err)
	}
	for _, redirect := range []Redirect{
		{SourceURL: page.URL, FinalHost: "target.example"},
		{SourceURL: page.URL, IncrementHost: true},
		{SourceURL: page.URL, FinalURL: " "},
		{SourceURL: page.URL, FinalURL: page.URL, FinalHost: page.Host},
		{SourceURL: page.URL, FinalURL: "https://target.example/page"},
		{SourceURL: page.URL, FinalURL: "https://target.example/page", FinalHost: "target.example"},
	} {
		if _, err := checkpoint.RecordRedirect(
			testContext,
			provenance,
			redirect,
		); !errors.Is(
			err,
			ErrInvalidPage,
		) {
			t.Fatalf("invalid redirect %+v error = %v", redirect, err)
		}
	}
}

func TestRedirectReservationRejectsCorruptVisitedRows(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	prefix, _ := provenancePrefix(provenance)
	target := "https://target.example/page"
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		return transaction.Bucket(visitedBucket).Put(childRowKey(prefix, target), []byte{2})
	})
	if _, err := checkpoint.RecordRedirect(testContext, provenance, Redirect{
		SourceURL: page.URL, FinalURL: target, FinalHost: "target.example", IncrementHost: true,
	}); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("corrupt redirect visited marker error = %v", err)
	}
}

func TestRedirectReservationCanClearAndReplaceTargets(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	first := Redirect{
		SourceURL:     page.URL,
		FinalURL:      "https://first.example/page",
		FinalHost:     "first.example",
		IncrementHost: true,
	}
	if admitted, err := checkpoint.RecordRedirect(
		testContext,
		provenance,
		first,
	); err != nil ||
		!admitted {
		t.Fatalf("record first redirect = %v, %v", admitted, err)
	}
	if admitted, err := checkpoint.RecordRedirect(
		testContext, provenance, Redirect{SourceURL: page.URL},
	); err != nil || !admitted {
		t.Fatalf("clear redirect = %v, %v", admitted, err)
	}
	second := Redirect{
		SourceURL:     page.URL,
		FinalURL:      "https://second.example/page",
		FinalHost:     "second.example",
		IncrementHost: true,
	}
	if admitted, err := checkpoint.RecordRedirect(
		testContext,
		provenance,
		second,
	); err != nil ||
		!admitted {
		t.Fatalf("record second redirect = %v, %v", admitted, err)
	}
	prefix, _ := provenancePrefix(provenance)
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		return transaction.Bucket(visitedBucket).Put(
			childRowKey(prefix, "https://third.example/page"), visitedMarker,
		)
	})
	third := Redirect{
		SourceURL:     page.URL,
		FinalURL:      "https://third.example/page",
		FinalHost:     "third.example",
		IncrementHost: true,
	}
	if admitted, err := checkpoint.RecordRedirect(
		testContext,
		provenance,
		third,
	); err != nil ||
		admitted {
		t.Fatalf("replace with already visited target = %v, %v", admitted, err)
	}
}

func TestRedirectReplayRejectsChangedOwnershipAndMissingReservation(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx, []byte, Page) error
		replay Redirect
	}{
		{name: "ownership", replay: Redirect{
			FinalURL: "https://target.example/page", FinalHost: "wrong.example", IncrementHost: true,
		}},
		{name: "missing reservation", mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			return transaction.Bucket(visitedBucket).Delete(childRowKey(prefix, page.RedirectURL))
		}, replay: Redirect{FinalURL: "https://other.example/page", FinalHost: "other.example", IncrementHost: true}},
		{name: "invalid ownership", mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			page.RedirectHost = ""
			encoded, _ := encodeRow("page", page)
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), encoded)
		}, replay: Redirect{FinalURL: "https://other.example/page", FinalHost: "other.example", IncrementHost: true}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, page := admittedCheckpoint(t)
			target := "https://target.example/page"
			if admitted, err := checkpoint.RecordRedirect(testContext, provenance, Redirect{
				SourceURL:     page.URL,
				FinalURL:      target,
				FinalHost:     "target.example",
				IncrementHost: true,
			}); err != nil || !admitted {
				t.Fatalf("record redirect fixture = %v, %v", admitted, err)
			}
			prefix, _ := provenancePrefix(provenance)
			if testCase.mutate != nil {
				snapshot, err := checkpoint.Load(testContext, provenance)
				if err != nil {
					t.Fatalf("load redirect fixture: %v", err)
				}
				mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
					return testCase.mutate(transaction, prefix, snapshot.Outstanding[0])
				})
			}
			testCase.replay.SourceURL = page.URL
			if _, err := checkpoint.RecordRedirect(
				testContext, provenance, testCase.replay,
			); !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("changed redirect ownership error = %v", err)
			}
		})
	}
}

func TestPageCompletionRejectsInvalidHostProgressAndTallyOverflow(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	for _, progress := range []*PageHostProgress{
		{Host: ""},
		{Host: page.Host, DroppedURLs: []string{" "}, Progress: HostProgress{Retired: true}},
		{Host: page.Host, DroppedURLs: []string{page.URL}},
	} {
		if err := checkpoint.CompletePage(
			testContext, provenance, page.URL, PageCompletion{HostProgress: progress},
		); err == nil {
			t.Fatalf("invalid host progress %+v succeeded", progress)
		}
	}
	mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) {
		record.Tally.Fetched = math.MaxUint64
	})
	if err := checkpoint.CompletePage(testContext, provenance, page.URL, PageCompletion{
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 1},
	}); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("completion tally overflow error = %v", err)
	}
}

func TestCompleteOutstandingPageRejectsMissingAndCorruptRows(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx, []byte, []byte, Page) error
	}{
		{name: "missing run", mutate: func(transaction *bolt.Tx, provenance, _ []byte, _ Page) error {
			return transaction.Bucket(runsBucket).Delete(provenance)
		}},
		{name: "missing bucket", mutate: func(transaction *bolt.Tx, _, _ []byte, _ Page) error {
			return transaction.DeleteBucket(visitedBucket)
		}},
		{name: "position", mutate: func(transaction *bolt.Tx, _, prefix []byte, page Page) error {
			return transaction.Bucket(pagePositionsBucket).Put(childRowKey(prefix, page.URL), []byte{1})
		}},
		{name: "host progress", mutate: func(transaction *bolt.Tx, _, prefix []byte, page Page) error {
			return transaction.Bucket(hostsBucket).Put(childRowKey(prefix, page.Host), []byte("{"))
		}},
		{name: "pending", mutate: func(transaction *bolt.Tx, provenance, _ []byte, _ Page) error {
			record, _, err := readRunRecord(transaction, provenance)
			if err != nil {
				return err
			}
			record.Pending = 0
			return writeRunRecord(transaction, provenance, record)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, page := admittedCheckpoint(t)
			prefix, _ := provenancePrefix(provenance)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, provenance, prefix, page)
			})
			completion := PageCompletion{}
			if testCase.name == "host progress" {
				completion.HostProgress = &PageHostProgress{
					Host:     page.Host,
					Progress: HostProgress{Generation: 1},
				}
			}
			if err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
				_, err := completeOutstandingPage(
					transaction,
					provenance,
					prefix,
					page.URL,
					completion,
				)
				return err
			}); !errors.Is(err, ErrCorruptCheckpoint) && !errors.Is(err, ErrRunNotFound) {
				t.Fatalf("complete corrupt row error = %v", err)
			}
		})
	}
}

func TestHostStateRejectsDroppedPagesWithoutRetirement(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	if err := checkpoint.RecordHostState(
		testContext, provenance, page.Host, HostProgress{}, []string{page.URL},
	); !errors.Is(err, ErrInvalidHostState) {
		t.Fatalf("nonretired dropped pages error = %v", err)
	}
}

func TestHostStateReplayAndStaleGenerationDoNotRepeatRetirement(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	progress := HostProgress{Generation: 2, Failures: 1, Retired: true}
	if err := checkpoint.RecordHostState(
		testContext,
		provenance,
		page.Host,
		progress,
		nil,
	); err != nil {
		t.Fatalf("record generated host state: %v", err)
	}
	if err := checkpoint.RecordHostState(
		testContext,
		provenance,
		page.Host,
		progress,
		nil,
	); err != nil {
		t.Fatalf("replay generated host state: %v", err)
	}
	stale := HostProgress{Generation: 1}
	if err := checkpoint.RecordHostState(
		testContext,
		provenance,
		page.Host,
		stale,
		nil,
	); err != nil {
		t.Fatalf("record stale host state: %v", err)
	}
}

func TestDecrementHostPagesRejectsMissingAndEmptyHostState(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	prefix := []byte{1, 2}
	if err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
		if err := decrementHostPages(
			transaction.Bucket(hostsBucket),
			prefix,
			"missing.example",
		); !errors.Is(
			err,
			ErrCorruptCheckpoint,
		) {
			t.Fatalf("missing host decrement error = %v", err)
		}
		if err := writeHostRecord(
			transaction.Bucket(hostsBucket), prefix, "empty.example", hostRecord{},
		); err != nil {
			return err
		}
		if err := decrementHostPages(
			transaction.Bucket(hostsBucket),
			prefix,
			"empty.example",
		); !errors.Is(
			err,
			ErrCorruptCheckpoint,
		) {
			t.Fatalf("empty host decrement error = %v", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect host decrement: %v", err)
	}
}
