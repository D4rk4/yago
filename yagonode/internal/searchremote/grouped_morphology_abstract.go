package searchremote

import (
	"context"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	maximumSwarmMorphologyRequirements = 32
	maximumSwarmMorphologySurfaces     = 20
	maximumSwarmMorphologyForms        = 12
	maximumObservedFormCandidates      = 24
	maximumMorphologyPeersPerSurface   = 2
)

type queryWordRequirement struct {
	forms []yagomodel.Hash
}

func boundedObservedForms(word string, expanded []string) []string {
	word = strings.TrimSpace(word)
	if word == "" {
		return nil
	}

	forms := make([]string, 0, maximumSwarmMorphologyForms)
	seen := make(map[yagomodel.Hash]struct{}, maximumSwarmMorphologyForms)
	originalHash := yagomodel.WordHash(word)
	seen[originalHash] = struct{}{}
	forms = append(forms, word)
	for position, candidate := range expanded {
		if position == maximumObservedFormCandidates {
			break
		}
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		hash := yagomodel.WordHash(candidate)
		if _, found := seen[hash]; found {
			continue
		}
		seen[hash] = struct{}{}
		forms = append(forms, candidate)
		if len(forms) == maximumSwarmMorphologyForms {
			break
		}
	}

	return forms
}

func (s searcher) groupedMorphologyRequirements(
	terms []string,
) ([]queryWordRequirement, bool) {
	if s.expandWord == nil || len(terms) < 2 ||
		len(terms) > maximumSwarmMorphologyRequirements {
		return nil, false
	}

	candidates := make([][]yagomodel.Hash, 0, len(terms))
	for _, term := range terms {
		forms := boundedObservedForms(term, s.expandWord(term))
		if len(forms) == 0 {
			continue
		}
		hashes := make([]yagomodel.Hash, len(forms))
		for position, form := range forms {
			hashes[position] = yagomodel.WordHash(form)
		}
		candidates = append(candidates, hashes)
	}
	if len(candidates) < 2 {
		return nil, false
	}

	requirements := make([]queryWordRequirement, len(candidates))
	for position, forms := range candidates {
		requirements[position] = queryWordRequirement{
			forms: []yagomodel.Hash{forms[0]},
		}
	}

	remaining := maximumSwarmMorphologySurfaces - len(requirements)
	expanded := false
	for formPosition := 1; remaining > 0; formPosition++ {
		added := false
		for requirementPosition, forms := range candidates {
			if formPosition >= len(forms) {
				continue
			}
			requirements[requirementPosition].forms = append(
				requirements[requirementPosition].forms,
				forms[formPosition],
			)
			remaining--
			expanded = true
			added = true
			if remaining == 0 {
				break
			}
		}
		if !added {
			break
		}
	}

	return requirements, expanded
}

func (s searcher) secondarySearch(
	ctx context.Context,
	req searchcore.Request,
	plan secondaryRetrievalPlan,
) ([]peerSearchResult, []searchcore.PartialFailure) {
	requirements, expanded := s.groupedMorphologyRequirements(req.Terms)
	if !expanded {
		return s.primaryAbstractSecondarySearch(
			ctx,
			req,
			primaryAbstractSecondaryPlan{
				terms:         plan.exactTerms,
				evidenceTerms: plan.evidenceTerms,
				results:       plan.primaryResults,
				budget:        plan.budget,
			},
		)
	}

	return s.groupedMorphologyAbstractSearch(
		ctx,
		req,
		requirements,
		plan,
	)
}

type secondaryRetrievalPlan struct {
	exactTerms     []yagomodel.Hash
	evidenceTerms  []string
	primaryResults []peerSearchResult
	reputation     *reputationSession
	budget         *remoteQueryBudget
}

func (s searcher) groupedMorphologyAbstractSearch(
	ctx context.Context,
	req searchcore.Request,
	requirements []queryWordRequirement,
	plan secondaryRetrievalPlan,
) ([]peerSearchResult, []searchcore.PartialFailure) {
	surfaces := distinctRequirementForms(requirements)
	targets, failures := s.termTargets(ctx, surfaces)
	targets = boundedMorphologyTargets(targets)
	abstracts, abstractFailures := s.termAbstractCatalogWithinBudget(
		ctx,
		req,
		targets,
		plan.reputation,
		plan.budget,
	)
	failures = append(failures, abstractFailures...)
	urls := intersectRequirementAbstracts(requirements, abstracts.terms)
	if len(urls) == 0 {
		return nil, failures
	}

	results := s.queryPeerJobsWithinBudget(ctx, secondarySearchJobs(secondarySearchPlan{
		request:       req,
		targets:       targets,
		urls:          limitSecondaryURLs(req, urls),
		evidenceTerms: plan.evidenceTerms,
		abstracts:     abstracts,
	},
		s.networkName,
		s.perPeerTimeout,
	), plan.budget)

	return results, failures
}

func distinctRequirementForms(requirements []queryWordRequirement) []yagomodel.Hash {
	forms := make([]yagomodel.Hash, 0, maximumSwarmMorphologySurfaces)
	seen := make(map[yagomodel.Hash]struct{}, maximumSwarmMorphologySurfaces)
	for formPosition := 0; ; formPosition++ {
		available := false
		for _, requirement := range requirements {
			if formPosition >= len(requirement.forms) {
				continue
			}
			available = true
			form := requirement.forms[formPosition]
			if _, found := seen[form]; found {
				continue
			}
			seen[form] = struct{}{}
			forms = append(forms, form)
		}
		if !available {
			break
		}
	}

	return forms
}

func boundedMorphologyTargets(targets []termPeerTargets) []termPeerTargets {
	bounded := make([]termPeerTargets, len(targets))
	for position, target := range targets {
		bounded[position] = target
		bounded[position].morphology = true
		if len(target.peers) > maximumMorphologyPeersPerSurface {
			bounded[position].peers = target.peers[:maximumMorphologyPeersPerSurface]
		}
	}

	return bounded
}

func intersectRequirementAbstracts(
	requirements []queryWordRequirement,
	abstracts map[yagomodel.Hash]map[yagomodel.Hash]struct{},
) []yagomodel.Hash {
	if len(requirements) == 0 {
		return nil
	}

	intersection := requirementAbstractUnion(requirements[0], abstracts)
	if len(intersection) == 0 {
		return nil
	}
	for _, requirement := range requirements[1:] {
		union := requirementAbstractUnion(requirement, abstracts)
		if len(union) == 0 {
			return nil
		}
		for hash := range intersection {
			if _, found := union[hash]; !found {
				delete(intersection, hash)
			}
		}
	}

	return sortedHashes(intersection)
}

func requirementAbstractUnion(
	requirement queryWordRequirement,
	abstracts map[yagomodel.Hash]map[yagomodel.Hash]struct{},
) map[yagomodel.Hash]struct{} {
	union := make(map[yagomodel.Hash]struct{})
	for _, form := range requirement.forms {
		for hash := range abstracts[form] {
			union[hash] = struct{}{}
		}
	}

	return union
}
