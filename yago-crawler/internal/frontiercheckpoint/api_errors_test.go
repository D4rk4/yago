package frontiercheckpoint

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestClosedCheckpointRejectsEveryDatabaseOperation(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close checkpoint: %v", err)
	}
	page := testPage("https://example.com/", "example.com", "observation", 0)
	operations := []struct {
		name string
		run  func() error
	}{
		{
			name: "worker identity",
			run:  func() error { _, err := checkpoint.WorkerID("crawler"); return err },
		},
		{
			name: "status",
			run:  func() error { _, err := checkpoint.Status(testContext, []byte("p"), []byte("i")); return err },
		},
		{
			name: "begin",
			run:  func() error { return checkpoint.Begin(testContext, []byte("p"), []byte("i"), "") },
		},
		{
			name: "admit",
			run:  func() error { _, err := checkpoint.Admit(testContext, []byte("p"), []Page{page}); return err },
		},
		{
			name: "finish seeding",
			run:  func() error { return checkpoint.FinishSeeding(testContext, []byte("p"), testRunTally()) },
		},
		{
			name: "complete page",
			run:  func() error { return checkpoint.CompletePage(testContext, []byte("p"), page.URL, testPageCompletion()) },
		},
		{name: "redirect", run: func() error {
			_, err := checkpoint.RecordRedirect(
				testContext,
				[]byte("p"),
				testRedirect(page, "https://final.example/", "final.example", true),
			)
			return err
		}},
		{name: "host state", run: func() error {
			return checkpoint.RecordHostState(
				testContext,
				[]byte("p"),
				page.Host,
				HostProgress{},
				nil,
			)
		}},
		{
			name: "load",
			run:  func() error { _, err := checkpoint.Load(testContext, []byte("p")); return err },
		},
		{name: "delete", run: func() error { return checkpoint.Delete(testContext, []byte("p")) }},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			requireErrorIs(t, operation.run(), ErrClosed)
		})
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("repeat close: %v", err)
	}
}

func TestCancelledContextRejectsReadsAndWrites(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	ctx, cancel := context.WithCancel(testContext)
	cancel()
	if _, err := checkpoint.Status(
		ctx,
		[]byte("p"),
		[]byte("i"),
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("cancelled read error = %v", err)
	}
	if err := checkpoint.Begin(
		ctx,
		[]byte("p"),
		[]byte("i"),
		"",
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("cancelled write error = %v", err)
	}
}

type invalidInputCheck struct {
	name   string
	target error
	run    func() error
}

func TestInvalidInputsAreRejectedBeforeMutation(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	validPage := testPage("https://example.com/", "example.com", "observation", 0)
	checks := append(
		invalidRunInputChecks(checkpoint, validPage),
		invalidProgressInputChecks(checkpoint, validPage)...,
	)
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			requireErrorIs(t, check.run(), check.target)
		})
	}
}

func invalidRunInputChecks(checkpoint *FrontierCheckpoint, validPage Page) []invalidInputCheck {
	invalidPage := testPage("", "example.com", "observation", 0)
	return []invalidInputCheck{
		{name: "status provenance", target: ErrInvalidProvenance, run: func() error {
			_, err := checkpoint.Status(testContext, nil, []byte("i"))
			return err
		}},
		{name: "status identity", target: ErrInvalidIdentity, run: func() error {
			_, err := checkpoint.Status(testContext, []byte("p"), nil)
			return err
		}},
		{name: "begin provenance", target: ErrInvalidProvenance, run: func() error {
			return checkpoint.Begin(testContext, nil, []byte("i"), "")
		}},
		{name: "begin identity", target: ErrInvalidIdentity, run: func() error {
			return checkpoint.Begin(testContext, []byte("p"), nil, "")
		}},
		{name: "admit provenance", target: ErrInvalidProvenance, run: func() error {
			_, err := checkpoint.Admit(testContext, nil, []Page{validPage})
			return err
		}},
		{name: "admit page", target: ErrInvalidPage, run: func() error {
			_, err := checkpoint.Admit(testContext, []byte("p"), []Page{invalidPage})
			return err
		}},
		{name: "finish provenance", target: ErrInvalidProvenance, run: func() error {
			return checkpoint.FinishSeeding(testContext, nil, testRunTally())
		}},
		{name: "complete provenance", target: ErrInvalidProvenance, run: func() error {
			return checkpoint.CompletePage(testContext, nil, validPage.URL, testPageCompletion())
		}},
		{name: "complete url", target: ErrInvalidPage, run: func() error {
			return checkpoint.CompletePage(testContext, []byte("p"), "", testPageCompletion())
		}},
	}
}

func invalidProgressInputChecks(
	checkpoint *FrontierCheckpoint,
	validPage Page,
) []invalidInputCheck {
	return []invalidInputCheck{
		{name: "redirect url", target: ErrInvalidPage, run: func() error {
			_, err := checkpoint.RecordRedirect(testContext, []byte("p"), Redirect{})
			return err
		}},
		{name: "redirect provenance", target: ErrInvalidProvenance, run: func() error {
			_, err := checkpoint.RecordRedirect(
				testContext,
				nil,
				testRedirect(validPage, "https://final.example/", "", false),
			)
			return err
		}},
		{name: "redirect host", target: ErrInvalidPage, run: func() error {
			_, err := checkpoint.RecordRedirect(
				testContext,
				[]byte("p"),
				testRedirect(validPage, "https://final.example/", "", true),
			)
			return err
		}},
		{name: "host state provenance", target: ErrInvalidProvenance, run: func() error {
			return checkpoint.RecordHostState(testContext, nil, "example.com", HostProgress{}, nil)
		}},
		{name: "host state host", target: ErrInvalidPage, run: func() error {
			return checkpoint.RecordHostState(testContext, []byte("p"), "", HostProgress{}, nil)
		}},
		{name: "host state url", target: ErrInvalidPage, run: func() error {
			return checkpoint.RecordHostState(
				testContext, []byte("p"), "example.com", HostProgress{}, []string{""},
			)
		}},
		{name: "load provenance", target: ErrInvalidProvenance, run: func() error {
			_, err := checkpoint.Load(testContext, nil)
			return err
		}},
		{name: "delete provenance", target: ErrInvalidProvenance, run: func() error {
			return checkpoint.Delete(testContext, nil)
		}},
	}
}

func TestInvalidPageFieldsAreRejected(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	beginTestRun(t, checkpoint, []byte("p"), []byte("i"))
	pages := []Page{
		testPage("https://example.com/", "", "observation", 0),
		testPage("https://example.com/", "example.com", "", 0),
		testPage("https://example.com/", "example.com", "observation", -1),
	}
	for index, page := range pages {
		if _, err := checkpoint.Admit(
			testContext,
			[]byte("p"),
			[]Page{page},
		); !errors.Is(
			err,
			ErrInvalidPage,
		) {
			t.Fatalf("invalid page %d error = %v", index, err)
		}
	}
}

func TestMissingRunRejectsRunMutations(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	page := testPage("https://example.com/", "example.com", "observation", 0)
	operations := []func() error{
		func() error { _, err := checkpoint.Admit(testContext, []byte("missing"), []Page{page}); return err },
		func() error { return checkpoint.FinishSeeding(testContext, []byte("missing"), testRunTally()) },
		func() error {
			return checkpoint.CompletePage(
				testContext,
				[]byte("missing"),
				page.URL,
				testPageCompletion(),
			)
		},
		func() error {
			_, err := checkpoint.RecordRedirect(
				testContext,
				[]byte("missing"),
				testRedirect(page, "https://final.example/", "final.example", false),
			)
			return err
		},
		func() error {
			return checkpoint.RecordHostState(
				testContext,
				[]byte("missing"),
				page.Host,
				HostProgress{},
				nil,
			)
		},
		func() error { _, err := checkpoint.Load(testContext, []byte("missing")); return err },
	}
	for index, operation := range operations {
		if err := operation(); !errors.Is(err, ErrRunNotFound) {
			t.Fatalf("missing run operation %d error = %v", index, err)
		}
	}
}

func TestInvalidSourceTimeRollsBackAdmission(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("invalid-time")
	beginTestRun(t, checkpoint, provenance, []byte("identity"))
	page := testPage("https://example.com/", "example.com", "observation", 0)
	page.SourceModifiedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := checkpoint.Admit(testContext, provenance, []Page{page}); err == nil {
		t.Fatal("out-of-range time was encoded")
	}
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load after rejected admission: %v", err)
	}
	if snapshot.Counters != (Counters{}) || len(snapshot.Visited) != 0 {
		t.Fatalf("rejected admission mutated run: %+v", snapshot)
	}
}

func TestCounterOverflowRollsBackAdmission(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*runRecord)
	}{
		{
			name:   "sequence",
			mutate: func(record *runRecord) { record.NextSequence = math.MaxUint64 },
		},
		{name: "pages", mutate: func(record *runRecord) { record.Pages = math.MaxUint64 }},
		{name: "pending", mutate: func(record *runRecord) { record.Pending = math.MaxUint64 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			provenance := []byte("overflow")
			beginTestRun(t, checkpoint, provenance, []byte("identity"))
			mutateRunRecord(t, checkpoint, provenance, testCase.mutate)
			page := testPage("https://example.com/", "example.com", "observation", 0)
			if _, err := checkpoint.Admit(
				testContext,
				provenance,
				[]Page{page},
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("overflow error = %v", err)
			}
		})
	}
}

func TestCloseToleratesAlreadyClosedUnderlyingDatabase(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	if err := checkpoint.database.Close(); err != nil {
		t.Fatalf("close underlying database: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close checkpoint after underlying close: %v", err)
	}
}

func mutateRunRecord(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
	mutate func(*runRecord),
) {
	t.Helper()
	if err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		mutate(&record)
		return writeRunRecord(transaction, provenance, record)
	}); err != nil {
		t.Fatalf("mutate run record: %v", err)
	}
}
