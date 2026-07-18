package frontier

import (
	"context"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestPrepareSeedCandidatesRejectsOverlongIdentityURL(t *testing.T) {
	profile := internalProfile(t)
	requests := internalRequests(
		profile,
		"https://example.org/"+strings.Repeat(
			"x",
			yagocrawlcontract.MaximumCrawlURLBytes,
		),
	)
	if candidates := prepareSeedCandidates(
		context.Background(),
		requests,
		nil,
		profile,
	); len(candidates) != 0 {
		t.Fatalf("candidates = %v, want none", candidates)
	}
}

func TestPrepareSeedCandidatesKeepsExplicitFormatOverride(t *testing.T) {
	profile := internalProfile(t)
	requests := internalRequests(
		profile,
		"https://example.org/archive.tar.gz",
		"https://example.org/setup.msi",
		"https://example.org/system.iso",
	)
	if candidates := prepareSeedCandidates(
		t.Context(),
		requests,
		nil,
		profile,
	); len(candidates) != len(requests) {
		t.Fatalf("explicit seed candidates = %d, want %d", len(candidates), len(requests))
	}
}
