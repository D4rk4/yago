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
	if result.EvidenceReady && len(result.FieldTermPositions) > 0 {
		retained := map[string]struct{}{}
		for _, terms := range result.FieldTermPositions {
			for term := range terms {
				retained[term] = struct{}{}
			}
		}
		aligned := make([]rerankQueryRequirement, 0, len(requirements))
		for _, requirement := range requirements {
			if _, found := retained[requirement.term]; found {
				aligned = append(aligned, requirement)
			}
		}
		if len(aligned) > 0 {
			return aligned
		}
	}

	return fallbackRerankRequirements(requirements)
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
