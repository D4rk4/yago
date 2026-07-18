package searchremote

import (
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type secondarySearchPlan struct {
	request       searchcore.Request
	targets       []termPeerTargets
	urls          []yagomodel.Hash
	evidenceTerms []string
	abstracts     termAbstractCatalog
}

type secondaryMetadataRoute struct {
	term yagomodel.Hash
	peer yagomodel.Seed
	urls []yagomodel.Hash
}

func secondarySearchJobs(
	plan secondarySearchPlan,
	networkName string,
	perPeerTimeout time.Duration,
) []peerSearchJob {
	routes := secondaryMetadataRoutes(plan)
	jobs := make([]peerSearchJob, 0, len(routes))
	for _, route := range routes {
		searchRequest := secondaryRemoteSearchRequest(
			plan.request,
			route.term,
			route.urls,
			networkName,
			perPeerTimeout,
		)
		evidenceBinding := identityQueryMatchEvidenceBinding(plan.evidenceTerms)
		evidenceBinding.request(&searchRequest)
		jobs = append(jobs, peerSearchJob{
			term:            route.term,
			peer:            route.peer,
			request:         searchRequest,
			evidenceBinding: evidenceBinding,
		})
	}

	return jobs
}

func secondaryMetadataRoutes(plan secondarySearchPlan) []secondaryMetadataRoute {
	peers, terms := distinctSecondaryPeersAndTerms(plan.targets)
	routes := make([]secondaryMetadataRoute, 0, len(peers))
	for _, peer := range peers {
		remaining := admittedSecondaryURLs(plan.urls, peer, terms, plan.abstracts)
		for len(remaining) > 0 {
			term, covered := broadestSecondaryTerm(peer, terms, remaining, plan.abstracts)
			routes = append(routes, secondaryMetadataRoute{
				term: term,
				peer: peer,
				urls: covered,
			})
			remaining = subtractSecondaryURLs(remaining, covered)
		}
	}

	return routes
}

func distinctSecondaryPeersAndTerms(
	targets []termPeerTargets,
) ([]yagomodel.Seed, []yagomodel.Hash) {
	peers := make([]yagomodel.Seed, 0)
	terms := make([]yagomodel.Hash, 0, len(targets))
	seenPeers := make(map[string]struct{})
	seenTerms := make(map[yagomodel.Hash]struct{}, len(targets))
	for _, target := range targets {
		if _, found := seenTerms[target.term]; !found {
			seenTerms[target.term] = struct{}{}
			terms = append(terms, target.term)
		}
		for _, peer := range target.peers {
			identity := peerRankingIdentity(peer)
			if _, found := seenPeers[identity]; found {
				continue
			}
			seenPeers[identity] = struct{}{}
			peers = append(peers, peer)
		}
	}

	return peers, terms
}

func admittedSecondaryURLs(
	urls []yagomodel.Hash,
	peer yagomodel.Seed,
	terms []yagomodel.Hash,
	abstracts termAbstractCatalog,
) []yagomodel.Hash {
	admitted := make([]yagomodel.Hash, 0, len(urls))
	for _, url := range urls {
		for _, term := range terms {
			if abstracts.peerAdmitted(peer, term, url) {
				admitted = append(admitted, url)
				break
			}
		}
	}

	return admitted
}

func broadestSecondaryTerm(
	peer yagomodel.Seed,
	terms []yagomodel.Hash,
	urls []yagomodel.Hash,
	abstracts termAbstractCatalog,
) (yagomodel.Hash, []yagomodel.Hash) {
	var selected yagomodel.Hash
	var covered []yagomodel.Hash
	for _, term := range terms {
		candidate := make([]yagomodel.Hash, 0, len(urls))
		for _, url := range urls {
			if abstracts.peerAdmitted(peer, term, url) {
				candidate = append(candidate, url)
			}
		}
		if len(candidate) > len(covered) {
			selected = term
			covered = candidate
		}
	}

	return selected, covered
}

func subtractSecondaryURLs(
	remaining []yagomodel.Hash,
	covered []yagomodel.Hash,
) []yagomodel.Hash {
	removed := make(map[yagomodel.Hash]struct{}, len(covered))
	for _, url := range covered {
		removed[url] = struct{}{}
	}
	pending := make([]yagomodel.Hash, 0, len(remaining)-len(covered))
	for _, url := range remaining {
		if _, found := removed[url]; !found {
			pending = append(pending, url)
		}
	}

	return pending
}
