package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

// countingRedirectFrontier records how often the run's visited-set was asked
// about a redirect target, so a refusal that never reaches the frontier can be
// told apart from one the frontier issued.
type countingRedirectFrontier struct {
	basicPageCompletionFrontier
	resolved []string
	admit    bool
}

func (frontier *countingRedirectFrontier) ResolveRedirect(
	_ crawljob.CrawlJob,
	finalURL string,
) bool {
	frontier.resolved = append(frontier.resolved, finalURL)

	return frontier.admit
}

// maximumLengthRedirectTarget is a URL whose normalized form is exactly
// MaximumCrawlURLBytes: the largest identity the ingest contract can carry.
func maximumLengthRedirectTarget(t *testing.T) string {
	t.Helper()
	const prefix = "https://example.com/"
	target := prefix + strings.Repeat(
		"x",
		yagocrawlcontract.MaximumCrawlURLBytes-len(prefix),
	)
	if len(target) != yagomodel.MaximumURLIdentityBytes {
		t.Fatalf("fixture length = %d, want %d", len(target), yagomodel.MaximumURLIdentityBytes)
	}

	return target
}

// TestRedirectAdmittedAcceptsIdentityURLAtTheLimit is the accepting half of the
// redirect identity bound. The rejecting half is pinned by
// TestRedirectAdmittedRejectsOverlongIdentityURL, which passes just as well if
// the comparison is off by one — and a strict-greater bound that became
// greater-or-equal would silently drop every legitimate page whose URL sits on
// the contract's maximum instead of only the ones that exceed it.
func TestRedirectAdmittedAcceptsIdentityURLAtTheLimit(t *testing.T) {
	frontier := &countingRedirectFrontier{admit: true}
	pipeline := &Pipeline{frontier: frontier}
	target := maximumLengthRedirectTarget(t)

	if !pipeline.redirectAdmitted(
		context.Background(),
		crawljob.CrawlJob{URL: "https://example.com/start"},
		target,
	) {
		t.Fatal("a redirect target of exactly the maximum identity length must be admitted")
	}
	if len(frontier.resolved) != 1 || frontier.resolved[0] != target {
		t.Fatalf("visited-set checks = %d, want the target checked once", len(frontier.resolved))
	}
}

// TestRedirectAdmittedRefusesOverlongTargetBeforeTheVisitedSet pins which
// refusal fired. An overlong target and an already-visited target both leave
// the page unindexed under the same outcome reason, so "not admitted" alone
// cannot tell them apart. The length bound must refuse before the frontier is
// consulted: recording a 2 KiB-plus URL in the run's visited set is exactly the
// identity the ingest contract will later refuse, and a crawl that recorded it
// would carry that unusable identity through checkpointing.
func TestRedirectAdmittedRefusesOverlongTargetBeforeTheVisitedSet(t *testing.T) {
	frontier := &countingRedirectFrontier{admit: true}
	pipeline := &Pipeline{frontier: frontier}
	overlong := maximumLengthRedirectTarget(t) + "y"

	if pipeline.redirectAdmitted(
		context.Background(),
		crawljob.CrawlJob{URL: "https://example.com/start"},
		overlong,
	) {
		t.Fatal("a redirect target one byte over the identity limit must not be admitted")
	}
	if len(frontier.resolved) != 0 {
		t.Fatalf("visited-set checks = %d, want the target refused before the frontier",
			len(frontier.resolved))
	}
}
