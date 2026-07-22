package searchremote

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type primaryAbstractSecondaryPlan struct {
	terms         []yagomodel.Hash
	evidenceTerms []string
	results       []peerSearchResult
	budget        *remoteQueryBudget
}

func (s searcher) primaryAbstractSecondarySearch(
	ctx context.Context,
	req searchcore.Request,
	plan primaryAbstractSecondaryPlan,
) ([]peerSearchResult, []searchcore.PartialFailure) {
	abstracts, failures := primaryIndexAbstractCatalogWithinBudget(
		plan.results,
		plan.terms,
		plan.budget,
	)
	urls := intersectTermAbstracts(plan.terms, abstracts.terms)
	if len(urls) == 0 {
		return nil, failures
	}
	targets := primaryIndexAbstractTargets(plan.results, plan.terms, abstracts)
	results := s.queryPeerJobsWithinBudget(ctx, secondarySearchJobs(secondarySearchPlan{
		request:       req,
		targets:       targets,
		urls:          limitSecondaryURLs(req, urls),
		evidenceTerms: plan.evidenceTerms,
		abstracts:     abstracts,
	}, s.networkName, s.perPeerTimeout), plan.budget)

	return results, failures
}

func primaryIndexAbstractCatalogWithinBudget(
	results []peerSearchResult,
	terms []yagomodel.Hash,
	budget *remoteQueryBudget,
) (termAbstractCatalog, []searchcore.PartialFailure) {
	catalog := termAbstractCatalog{
		terms:     make(map[yagomodel.Hash]map[yagomodel.Hash]struct{}, len(terms)),
		peerTerms: make(map[string]map[yagomodel.Hash]map[yagomodel.Hash]struct{}),
	}
	slots := primaryIndexAbstractTotal(results, terms)
	limits := distributedLimits(
		budget.abstractEntriesRemaining,
		slots,
		budget.abstractEntriesRemaining,
	)
	position := 0
	var failures []searchcore.PartialFailure
	for _, result := range results {
		if result.err != nil {
			continue
		}
		for _, term := range terms {
			encoded, found := result.response.IndexAbstract[term]
			if !found {
				continue
			}
			limit := limits[position]
			position++
			if limit == 0 {
				continue
			}
			urls, err := yagomodel.DecodeSearchIndexAbstractWithLimit(encoded, limit)
			if err != nil {
				failures = append(failures, peerFailure(result.peer, err))
				continue
			}
			catalog.admit(term, result.peer, urls)
			if catalog.terms[term] == nil {
				catalog.terms[term] = make(map[yagomodel.Hash]struct{}, len(urls))
			}
			for _, url := range urls {
				catalog.terms[term][url] = struct{}{}
			}
			budget.abstractEntriesRemaining -= len(urls)
		}
	}

	return catalog, failures
}

func primaryIndexAbstractTotal(results []peerSearchResult, terms []yagomodel.Hash) int {
	total := 0
	for _, result := range results {
		if result.err != nil {
			continue
		}
		for _, term := range terms {
			if _, found := result.response.IndexAbstract[term]; found {
				total++
			}
		}
	}

	return total
}

func primaryIndexAbstractTargets(
	results []peerSearchResult,
	terms []yagomodel.Hash,
	abstracts termAbstractCatalog,
) []termPeerTargets {
	targets := make([]termPeerTargets, 0, len(terms))
	for _, term := range terms {
		peers := make([]yagomodel.Seed, 0, len(results))
		for _, result := range results {
			if result.err == nil && abstracts.peerReported(result.peer, term) {
				peers = append(peers, result.peer)
			}
		}
		targets = append(targets, termPeerTargets{term: term, peers: peers})
	}

	return targets
}
