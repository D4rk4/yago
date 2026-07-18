package searchremote

import (
	"slices"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

type queryMatchEvidenceBinding struct {
	wireRequirements       []string
	rankingRequirements    []string
	singleRequirementRemap bool
	resourceAllowlist      map[yagomodel.Hash]struct{}
}

func identityQueryMatchEvidenceBinding(
	requirements []string,
) queryMatchEvidenceBinding {
	normalized, valid := normalizedQueryMatchEvidenceRequirements(requirements)
	if !valid {
		return queryMatchEvidenceBinding{}
	}

	return queryMatchEvidenceBinding{
		wireRequirements:    normalized,
		rankingRequirements: slices.Clone(normalized),
	}
}

func singleWordMorphologyQueryMatchEvidenceBinding(
	wireRequirement string,
	rankingRequirement string,
) queryMatchEvidenceBinding {
	wire, wireValid := normalizedQueryMatchEvidenceRequirements([]string{wireRequirement})
	ranking, rankingValid := normalizedQueryMatchEvidenceRequirements([]string{rankingRequirement})
	if !wireValid || !rankingValid {
		return queryMatchEvidenceBinding{}
	}

	return queryMatchEvidenceBinding{
		wireRequirements:       wire,
		rankingRequirements:    ranking,
		singleRequirementRemap: true,
	}
}

func normalizedQueryMatchEvidenceRequirements(
	requirements []string,
) ([]string, bool) {
	if len(requirements) == 0 {
		return nil, false
	}
	normalized := make([]string, len(requirements))
	for index, requirement := range requirements {
		requirement = strings.TrimSpace(requirement)
		if requirement == "" {
			return nil, false
		}
		normalized[index] = requirement
	}

	return normalized, true
}

func (binding queryMatchEvidenceBinding) request(
	request *yagoproto.SearchRequest,
) {
	requestQueryMatchEvidence(request, binding.wireRequirements)
}

func (binding queryMatchEvidenceBinding) negotiated(
	request yagoproto.SearchRequest,
) queryMatchEvidenceBinding {
	wireRequirements := negotiatedQueryEvidenceTerms(request)
	if !binding.valid() || !slices.Equal(wireRequirements, binding.wireRequirements) ||
		!queryMatchEvidenceRequirementsBound(request, wireRequirements) {
		return queryMatchEvidenceBinding{}
	}

	negotiated := binding.clone()
	negotiated.resourceAllowlist = queryMatchEvidenceResourceAllowlist(request.URLs)

	return negotiated
}

func (binding queryMatchEvidenceBinding) valid() bool {
	if len(binding.wireRequirements) == 0 ||
		len(binding.wireRequirements) != len(binding.rankingRequirements) {
		return false
	}
	if binding.singleRequirementRemap && len(binding.wireRequirements) != 1 {
		return false
	}
	if !binding.singleRequirementRemap &&
		!slices.Equal(binding.wireRequirements, binding.rankingRequirements) {
		return false
	}
	for index, wireRequirement := range binding.wireRequirements {
		if strings.TrimSpace(wireRequirement) != wireRequirement || wireRequirement == "" ||
			strings.TrimSpace(binding.rankingRequirements[index]) !=
				binding.rankingRequirements[index] || binding.rankingRequirements[index] == "" {
			return false
		}
	}

	return true
}

func (binding queryMatchEvidenceBinding) clone() queryMatchEvidenceBinding {
	return queryMatchEvidenceBinding{
		wireRequirements:       slices.Clone(binding.wireRequirements),
		rankingRequirements:    slices.Clone(binding.rankingRequirements),
		singleRequirementRemap: binding.singleRequirementRemap,
	}
}

func (binding queryMatchEvidenceBinding) allowsResource(hash yagomodel.Hash) bool {
	if binding.resourceAllowlist == nil {
		return true
	}
	_, allowed := binding.resourceAllowlist[hash]

	return allowed
}

func queryMatchEvidenceResourceAllowlist(
	resources []yagomodel.Hash,
) map[yagomodel.Hash]struct{} {
	if len(resources) == 0 {
		return nil
	}
	allowlist := make(map[yagomodel.Hash]struct{}, len(resources))
	for _, resource := range resources {
		allowlist[resource] = struct{}{}
	}

	return allowlist
}

func queryMatchEvidenceRequirementsBound(
	request yagoproto.SearchRequest,
	requirements []string,
) bool {
	if len(request.URLs) > 0 {
		return true
	}
	if len(request.Query) == 0 || len(request.Query) != len(requirements) {
		return false
	}
	remaining := make(map[yagomodel.Hash]int, len(request.Query))
	for _, hash := range request.Query {
		remaining[hash]++
	}
	for _, requirement := range requirements {
		hash := yagomodel.WordHash(requirement)
		if remaining[hash] == 0 {
			return false
		}
		remaining[hash]--
	}

	return true
}
