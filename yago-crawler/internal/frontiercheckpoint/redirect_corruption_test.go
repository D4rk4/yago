package frontiercheckpoint

import (
	"errors"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func redirectCheckpoint(
	t *testing.T,
) (*FrontierCheckpoint, []byte, Page, Redirect) {
	t.Helper()
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("redirect-corruption")
	source := testPage("https://source.example/page", "source.example", "source-observation", 0)
	beginTestRun(t, checkpoint, provenance, []byte("redirect-corruption-identity"))
	if admitted, err := checkpoint.Admit(
		testContext, provenance, []Page{source},
	); err != nil || admitted != 1 {
		t.Fatalf("admit redirect source = %d, %v", admitted, err)
	}
	redirect := testRedirect(source, "https://final.example/page", "final.example", true)
	return checkpoint, provenance, source, redirect
}

func mutateOutstandingRedirect(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
	sourceURL string,
	mutate func(*Page),
) {
	t.Helper()
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		t.Fatalf("provenance prefix: %v", err)
	}
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		row, found, err := findOutstandingPage(buckets, prefix, sourceURL)
		if err != nil {
			return err
		}
		if !found {
			return ErrRunNotFound
		}
		mutate(&row.page)
		encoded, err := encodeRow("mutated redirect page", row.page)
		if err != nil {
			return err
		}
		return putRow(buckets.pages, row.key, encoded, "mutated redirect page")
	})
}

func TestRecordRedirectRejectsCorruptSourceRows(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*bolt.Tx, []byte, Page) error
	}{
		{name: "position", mutate: func(transaction *bolt.Tx, prefix []byte, source Page) error {
			return putRow(
				transaction.Bucket(pagePositionsBucket),
				childRowKey(prefix, source.URL),
				[]byte{1},
				"corrupt redirect position",
			)
		}},
		{name: "missing page", mutate: func(transaction *bolt.Tx, prefix []byte, _ Page) error {
			return deleteRow(
				transaction.Bucket(pagesBucket),
				sequenceRowKey(prefix, 1),
				"redirect source page",
			)
		}},
		{name: "page encoding", mutate: func(transaction *bolt.Tx, prefix []byte, _ Page) error {
			return putRow(
				transaction.Bucket(pagesBucket),
				sequenceRowKey(prefix, 1),
				[]byte("{"),
				"corrupt redirect page",
			)
		}},
		{
			name: "page identity",
			mutate: func(transaction *bolt.Tx, prefix []byte, source Page) error {
				source.URL += "-changed"
				encoded, err := encodeRow("changed redirect source", source)
				if err != nil {
					return err
				}
				return putRow(
					transaction.Bucket(pagesBucket),
					sequenceRowKey(prefix, 1),
					encoded,
					"changed redirect source",
				)
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, source, redirect := redirectCheckpoint(t)
			prefix, _ := provenancePrefix(provenance)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, prefix, source)
			})
			if _, err := checkpoint.RecordRedirect(
				testContext,
				provenance,
				redirect,
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("corrupt redirect source error = %v", err)
			}
		})
	}
}

func TestRecordRedirectRejectsCorruptVisitedRows(t *testing.T) {
	t.Run("first reservation", func(t *testing.T) {
		checkpoint, provenance, _, redirect := redirectCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return putRow(
				transaction.Bucket(visitedBucket),
				childRowKey(prefix, redirect.FinalURL),
				[]byte{2},
				"corrupt redirect visited row",
			)
		})
		if _, err := checkpoint.RecordRedirect(
			testContext,
			provenance,
			redirect,
		); !errors.Is(
			err,
			ErrCorruptCheckpoint,
		) {
			t.Fatalf("corrupt first reservation error = %v", err)
		}
	})
	t.Run("replay reservation", func(t *testing.T) {
		checkpoint, provenance, _, redirect := redirectCheckpoint(t)
		if admitted, err := checkpoint.RecordRedirect(
			testContext, provenance, redirect,
		); err != nil || !admitted {
			t.Fatalf("record redirect = %v, %v", admitted, err)
		}
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return deleteRow(
				transaction.Bucket(visitedBucket),
				childRowKey(prefix, redirect.FinalURL),
				"redirect reservation",
			)
		})
		if _, err := checkpoint.RecordRedirect(
			testContext,
			provenance,
			redirect,
		); !errors.Is(
			err,
			ErrCorruptCheckpoint,
		) {
			t.Fatalf("missing replay reservation error = %v", err)
		}
	})
}

func TestLoadRejectsCorruptRedirectAssociations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*FrontierCheckpoint, []byte, Page, Redirect)
	}{
		{
			name: "same target",
			mutate: func(checkpoint *FrontierCheckpoint, provenance []byte, source Page, _ Redirect) {
				mutateOutstandingRedirect(t, checkpoint, provenance, source.URL, func(page *Page) {
					page.RedirectURL = page.URL
				})
			},
		},
		{
			name: "unreserved target",
			mutate: func(checkpoint *FrontierCheckpoint, provenance []byte, source Page, redirect Redirect) {
				mutateOutstandingRedirect(t, checkpoint, provenance, source.URL, func(page *Page) {
					page.RedirectURL = redirect.FinalURL
				})
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, source, redirect := redirectCheckpoint(t)
			testCase.mutate(checkpoint, provenance, source, redirect)
			requireCorruptLoad(t, checkpoint, provenance)
		})
	}
}

func TestLoadRejectsRedirectOwnedByMultipleSources(t *testing.T) {
	checkpoint, provenance, first, redirect := redirectCheckpoint(t)
	second := testPage("https://second.example/page", "second.example", "second-observation", 1)
	if admitted, err := checkpoint.Admit(
		testContext, provenance, []Page{second},
	); err != nil || admitted != 1 {
		t.Fatalf("admit second source = %d, %v", admitted, err)
	}
	mutateOutstandingRedirect(t, checkpoint, provenance, first.URL, func(page *Page) {
		page.RedirectURL = redirect.FinalURL
	})
	mutateOutstandingRedirect(t, checkpoint, provenance, second.URL, func(page *Page) {
		page.RedirectURL = redirect.FinalURL
	})
	prefix, _ := provenancePrefix(provenance)
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		return putRow(
			transaction.Bucket(visitedBucket),
			childRowKey(prefix, redirect.FinalURL),
			visitedMarker,
			"shared redirect target",
		)
	})
	requireCorruptLoad(t, checkpoint, provenance)
}

func TestLoadRejectsOutstandingRedirectTarget(t *testing.T) {
	checkpoint, provenance, first, _ := redirectCheckpoint(t)
	second := testPage("https://second.example/page", "second.example", "second-observation", 1)
	if admitted, err := checkpoint.Admit(
		testContext, provenance, []Page{second},
	); err != nil || admitted != 1 {
		t.Fatalf("admit target page = %d, %v", admitted, err)
	}
	mutateOutstandingRedirect(t, checkpoint, provenance, first.URL, func(page *Page) {
		page.RedirectURL = second.URL
	})
	requireCorruptLoad(t, checkpoint, provenance)
}
