package searchcore

import (
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/stopwords"
)

type rerankQueryRequirement struct {
	term    string
	ordinal int
}

func rerankQueryRequirements(req Request) []rerankQueryRequirement {
	return fallbackRerankRequirements(allRerankQueryRequirements(req))
}

func allRerankQueryRequirements(req Request) []rerankQueryRequirement {
	raw := req.Terms
	if len(raw) == 0 {
		raw = strings.Fields(req.Query)
	}
	seen := map[string]bool{}
	requirements := make([]rerankQueryRequirement, 0, len(raw))
	ordinal := 0
	for _, term := range raw {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		requirement := rerankQueryRequirement{term: term, ordinal: ordinal}
		ordinal++
		if seen[term] {
			continue
		}
		seen[term] = true
		requirements = append(requirements, requirement)
	}

	return requirements
}

func fallbackRerankRequirements(
	requirements []rerankQueryRequirement,
) []rerankQueryRequirement {
	content := make([]rerankQueryRequirement, 0, len(requirements))
	for _, requirement := range requirements {
		if !stopwords.IsStopword(requirement.term) {
			content = append(content, requirement)
		}
	}
	if len(content) > 0 {
		return content
	}

	return requirements
}

func rerankResultRequirements(
	req Request,
	result Result,
) []rerankQueryRequirement {
	requirements := allRerankQueryRequirements(req)
	if result.EvidenceReady && result.EvidenceRequirementOrdinals != nil {
		aligned, complete := completeRerankRequirements(
			requirements,
			result.EvidenceRequirementOrdinals,
		)
		if complete {
			return aligned
		}
	}

	return fallbackRerankRequirements(requirements)
}

func completeRerankRequirements(
	requirements []rerankQueryRequirement,
	ordinals []int,
) ([]rerankQueryRequirement, bool) {
	byOrdinal := make(map[int]rerankQueryRequirement, len(requirements))
	for _, requirement := range requirements {
		byOrdinal[requirement.ordinal] = requirement
	}
	aligned := make([]rerankQueryRequirement, 0, len(ordinals))
	previous := -1
	for _, ordinal := range ordinals {
		requirement, found := byOrdinal[ordinal]
		if !found || ordinal <= previous {
			return nil, false
		}
		aligned = append(aligned, requirement)
		previous = ordinal
	}

	return aligned, true
}

func rerankRequirementTerms(requirements []rerankQueryRequirement) []string {
	terms := make([]string, len(requirements))
	for index, requirement := range requirements {
		terms[index] = requirement.term
	}

	return terms
}

func rerankQueryTerms(req Request) []string {
	return rerankRequirementTerms(rerankQueryRequirements(req))
}
