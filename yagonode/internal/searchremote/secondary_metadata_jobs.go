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
	terms []yagomodel.Hash
	peer  yagomodel.Seed
	urls  []yagomodel.Hash
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
			route.terms,
			route.urls,
			networkName,
			perPeerTimeout,
		)
		evidenceBinding := identityQueryMatchEvidenceBinding(plan.evidenceTerms)
		evidenceBinding.request(&searchRequest)
		jobs = append(jobs, peerSearchJob{
			term:            route.terms[0],
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
		urls := admittedSecondaryURLs(plan.urls, peer, terms, plan.abstracts)
		if len(urls) == 0 {
			continue
		}
		routes = append(routes, secondaryMetadataRoute{
			terms: admittedSecondaryTerms(peer, terms, urls, plan.abstracts),
			peer:  peer,
			urls:  urls,
		})
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

func admittedSecondaryTerms(
	peer yagomodel.Seed,
	terms []yagomodel.Hash,
	urls []yagomodel.Hash,
	abstracts termAbstractCatalog,
) []yagomodel.Hash {
	admitted := make([]yagomodel.Hash, 0, len(terms))
	for _, term := range terms {
		for _, url := range urls {
			if abstracts.peerAdmitted(peer, term, url) {
				admitted = append(admitted, term)
				break
			}
		}
	}

	return admitted
}
