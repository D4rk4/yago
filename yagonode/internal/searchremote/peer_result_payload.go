package searchremote

import (
	"slices"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func mergedRemoteResultPayload(
	existing searchcore.Result,
	incoming searchcore.Result,
) searchcore.Result {
	mergedEvidence := existing.Evidence.Overlay(incoming.Evidence)
	if remoteResultPayloadPreferred(incoming, existing) {
		existing.Analyzer = incoming.Analyzer
		existing.EvidenceReady = incoming.EvidenceReady
		existing.EvidenceRequirementOrdinals = slices.Clone(
			incoming.EvidenceRequirementOrdinals,
		)
		existing.Snippet = incoming.Snippet
		existing.QueryMatches = slices.Clone(incoming.QueryMatches)
		existing.BodyQueryMatches = slices.Clone(incoming.BodyQueryMatches)
		existing.FieldTermPositions = clonedRemoteFieldPositions(
			incoming.FieldTermPositions,
		)
	}
	existing = remoteResultWithMissingDisplay(existing, incoming)
	existing.Evidence = mergedEvidence

	return existing
}

func remoteResultPayloadPreferred(
	candidate searchcore.Result,
	current searchcore.Result,
) bool {
	candidateStrength := remoteResultPayloadStrength(candidate)
	currentStrength := remoteResultPayloadStrength(current)
	for index := range candidateStrength {
		if candidateStrength[index] != currentStrength[index] {
			return candidateStrength[index] > currentStrength[index]
		}
	}

	return remoteResultPayloadIdentity(candidate) < remoteResultPayloadIdentity(current)
}

func remoteResultPayloadStrength(result searchcore.Result) [9]int {
	return [9]int{
		booleanWeight(result.EvidenceReady),
		booleanWeight(result.BodyQueryMatches != nil),
		len(result.BodyQueryMatches),
		booleanWeight(result.FieldTermPositions != nil),
		remoteFieldPositionTotal(result.FieldTermPositions),
		booleanWeight(result.QueryMatches != nil),
		len(result.QueryMatches),
		len(result.EvidenceRequirementOrdinals),
		booleanWeight(result.Analyzer != ""),
	}
}

func booleanWeight(value bool) int {
	if value {
		return 1
	}

	return 0
}

func remoteFieldPositionTotal(fields map[string]map[string][]int) int {
	total := 0
	for _, terms := range fields {
		for _, positions := range terms {
			total += len(positions)
		}
	}

	return total
}

func remoteResultPayloadIdentity(result searchcore.Result) string {
	var identity strings.Builder
	identity.WriteString(result.Analyzer)
	identity.WriteByte(0)
	identity.WriteString(result.Snippet)
	identity.WriteByte(0)
	identity.WriteString(result.Title)
	identity.WriteByte(0)
	identity.WriteString(result.URL)
	appendRemoteMatches(&identity, result.QueryMatches)
	appendRemoteMatches(&identity, result.BodyQueryMatches)
	for _, ordinal := range result.EvidenceRequirementOrdinals {
		identity.WriteByte(0)
		identity.WriteString(strconv.Itoa(ordinal))
	}

	return identity.String()
}

func appendRemoteMatches(identity *strings.Builder, matches []searchcore.QueryMatch) {
	for _, match := range matches {
		identity.WriteByte(0)
		identity.WriteString(strconv.Itoa(match.Start))
		identity.WriteByte(':')
		identity.WriteString(strconv.Itoa(match.End))
	}
}

func remoteResultWithMissingDisplay(
	result searchcore.Result,
	supplement searchcore.Result,
) searchcore.Result {
	if result.Title == "" {
		result.Title = supplement.Title
	}
	if result.URL == "" {
		result.URL = supplement.URL
	}
	if result.DisplayURL == "" {
		result.DisplayURL = supplement.DisplayURL
	}
	if result.Host == "" {
		result.Host = supplement.Host
	}
	if result.Path == "" {
		result.Path = supplement.Path
	}
	if result.File == "" {
		result.File = supplement.File
	}
	if result.ContentType == "" {
		result.ContentType = supplement.ContentType
	}
	if result.Language == "" {
		result.Language = supplement.Language
	}
	if result.Date == "" {
		result.Date = supplement.Date
	}

	return result
}

func clonedRemoteFieldPositions(
	fields map[string]map[string][]int,
) map[string]map[string][]int {
	if fields == nil {
		return nil
	}
	cloned := make(map[string]map[string][]int, len(fields))
	for field, terms := range fields {
		cloned[field] = make(map[string][]int, len(terms))
		for term, positions := range terms {
			cloned[field][term] = slices.Clone(positions)
		}
	}

	return cloned
}

func retainStrongestRemotePayloads(
	fused []searchcore.Result,
	rankings [][]searchcore.Result,
) []searchcore.Result {
	strongest := make(map[string]searchcore.Result, len(fused))
	for _, ranking := range rankings {
		for _, result := range ranking {
			identity := remoteResultIdentity(result)
			if retained, found := strongest[identity]; found {
				strongest[identity] = mergedRemoteResultPayload(retained, result)
				continue
			}
			strongest[identity] = result
		}
	}
	for index := range fused {
		fused[index] = mergedRemoteResultPayload(
			fused[index],
			strongest[remoteResultIdentity(fused[index])],
		)
	}

	return fused
}

func fuseRemoteVariantRankings(rankings [][]searchcore.Result) []searchcore.Result {
	return retainStrongestRemotePayloads(
		searchcore.FuseByReciprocalRank(rankings...),
		rankings,
	)
}
