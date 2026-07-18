package searchremote

import (
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestSecondaryMetadataJobsSplitOnlyWhenNoAdmittingTermCoversEveryURL(t *testing.T) {
	peer := searchSeed(t, "peer")
	firstTerm := yagomodel.WordHash("first")
	secondTerm := yagomodel.WordHash("second")
	firstURL := hashFor("first-url")
	secondURL := hashFor("second-url")
	abstracts := termAbstractCatalog{
		peerTerms: make(map[string]map[yagomodel.Hash]map[yagomodel.Hash]struct{}),
	}
	abstracts.admit(firstTerm, peer, []yagomodel.Hash{firstURL})
	abstracts.admit(secondTerm, peer, []yagomodel.Hash{secondURL})
	jobs := secondarySearchJobs(secondarySearchPlan{
		request: searchcore.Request{Limit: 10},
		targets: []termPeerTargets{
			{term: firstTerm, peers: []yagomodel.Seed{peer}},
			{term: secondTerm, peers: []yagomodel.Seed{peer}},
		},
		urls:          []yagomodel.Hash{firstURL, secondURL},
		evidenceTerms: []string{"first", "second"},
		abstracts:     abstracts,
	}, "freeworld", DefaultPerPeerTimeout)
	if len(jobs) != 2 || !slices.Equal(jobs[0].request.Query, []yagomodel.Hash{firstTerm}) ||
		!slices.Equal(jobs[0].request.URLs, []yagomodel.Hash{firstURL}) ||
		!slices.Equal(jobs[1].request.Query, []yagomodel.Hash{secondTerm}) ||
		!slices.Equal(jobs[1].request.URLs, []yagomodel.Hash{secondURL}) {
		t.Fatalf("jobs = %#v", jobs)
	}
}

func TestSecondaryMetadataJobsUseOneAdmittingTermForOverlappingURLs(t *testing.T) {
	peer := searchSeed(t, "peer")
	firstTerm := yagomodel.WordHash("first")
	secondTerm := yagomodel.WordHash("second")
	firstURL := hashFor("first-url")
	secondURL := hashFor("second-url")
	urls := []yagomodel.Hash{firstURL, secondURL}
	abstracts := termAbstractCatalog{
		peerTerms: make(map[string]map[yagomodel.Hash]map[yagomodel.Hash]struct{}),
	}
	abstracts.admit(firstTerm, peer, urls)
	abstracts.admit(secondTerm, peer, []yagomodel.Hash{firstURL})
	jobs := secondarySearchJobs(secondarySearchPlan{
		request: searchcore.Request{Limit: 10},
		targets: []termPeerTargets{
			{term: secondTerm, peers: []yagomodel.Seed{peer}},
			{term: firstTerm, peers: []yagomodel.Seed{peer}},
		},
		urls:          urls,
		evidenceTerms: []string{"first", "second"},
		abstracts:     abstracts,
	}, "freeworld", DefaultPerPeerTimeout)
	if len(jobs) != 1 || !slices.Equal(jobs[0].request.Query, []yagomodel.Hash{firstTerm}) ||
		!slices.Equal(jobs[0].request.URLs, urls) {
		t.Fatalf("jobs = %#v", jobs)
	}
}

func TestTermAbstractCatalogAdmitsFromZeroValue(t *testing.T) {
	peer := searchSeed(t, "peer")
	term := yagomodel.WordHash("term")
	resource := hashFor("resource")
	var abstracts termAbstractCatalog
	abstracts.admit(term, peer, []yagomodel.Hash{resource})
	if !abstracts.peerAdmitted(peer, term, resource) {
		t.Fatalf("resource was not admitted: %#v", abstracts)
	}
}
