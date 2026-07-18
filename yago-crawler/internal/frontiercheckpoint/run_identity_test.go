package frontiercheckpoint

import (
	"bytes"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestBeginAndStatusProtectProvenanceIdentity(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("same-provenance")
	identity := []byte("identity")
	beginTestRun(t, checkpoint, provenance, identity)
	beginTestRun(t, checkpoint, provenance, identity)
	status, err := checkpoint.Status(testContext, provenance, identity)
	if err != nil || status != RunActive {
		t.Fatalf("status = %v, %v", status, err)
	}
	if err := checkpoint.Begin(
		testContext,
		provenance,
		[]byte("other-identity"),
		yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
	); !errors.Is(err, ErrProvenanceCollision) {
		t.Fatalf("identity collision error = %v", err)
	}
	if err := checkpoint.Begin(
		testContext,
		provenance,
		identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); !errors.Is(err, ErrProvenanceCollision) {
		t.Fatalf("priority collision error = %v", err)
	}
	if _, err := checkpoint.Status(
		testContext, provenance, []byte("other-identity"),
	); !errors.Is(err, ErrProvenanceCollision) {
		t.Fatalf("status collision error = %v", err)
	}
}

func TestPrefixFreeProvenancesRemainIndependent(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	shortProvenance := []byte("a")
	longProvenance := []byte("ab")
	beginTestRun(t, checkpoint, shortProvenance, []byte("short"))
	beginTestRun(t, checkpoint, longProvenance, []byte("long"))
	shortPage := testPage("https://short.example/", "short.example", "short-observation", 0)
	longPage := testPage("https://long.example/", "long.example", "long-observation", 0)
	if admitted, err := checkpoint.Admit(
		testContext, shortProvenance, []Page{shortPage},
	); err != nil || admitted != 1 {
		t.Fatalf("admit short provenance = %d, %v", admitted, err)
	}
	if admitted, err := checkpoint.Admit(
		testContext, longProvenance, []Page{longPage},
	); err != nil || admitted != 1 {
		t.Fatalf("admit long provenance = %d, %v", admitted, err)
	}
	if err := checkpoint.Delete(testContext, shortProvenance); err != nil {
		t.Fatalf("delete short provenance: %v", err)
	}
	snapshot, err := checkpoint.Load(testContext, longProvenance)
	if err != nil {
		t.Fatalf("load long provenance: %v", err)
	}
	if len(snapshot.Outstanding) != 1 || snapshot.Outstanding[0].URL != longPage.URL {
		t.Fatalf("long provenance outstanding = %v", snapshot.Outstanding)
	}
}

func TestProvenancePrefixEscapesEmbeddedSeparator(t *testing.T) {
	plain, err := provenancePrefix([]byte("a"))
	if err != nil {
		t.Fatalf("plain provenance prefix: %v", err)
	}
	escaped, err := provenancePrefix([]byte{'a', 0, 'b'})
	if err != nil {
		t.Fatalf("escaped provenance prefix: %v", err)
	}
	if bytes.HasPrefix(plain, escaped) || bytes.HasPrefix(escaped, plain) {
		t.Fatalf("provenance prefixes overlap: %x and %x", plain, escaped)
	}
}

func TestDuplicateAdmissionsDoNotAdvanceCountersOrOrder(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("duplicates")
	beginTestRun(t, checkpoint, provenance, []byte("duplicates-identity"))
	first := testPage("https://example.com/first", "example.com", "first-observation", 0)
	second := testPage("https://example.com/second", "example.com", "second-observation", 1)
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{first},
	); err != nil ||
		admitted != 1 {
		t.Fatalf("first admission = %d, %v", admitted, err)
	}
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{first},
	); err != nil ||
		admitted != 0 {
		t.Fatalf("duplicate admission = %d, %v", admitted, err)
	}
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{second},
	); err != nil ||
		admitted != 1 {
		t.Fatalf("second admission = %d, %v", admitted, err)
	}
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load duplicate run: %v", err)
	}
	if snapshot.Counters != (Counters{Pages: 2, Pending: 2}) {
		t.Fatalf("duplicate counters = %+v", snapshot.Counters)
	}
	requirePageEqual(t, snapshot.Outstanding[0], first)
	requirePageEqual(t, snapshot.Outstanding[1], second)
}
