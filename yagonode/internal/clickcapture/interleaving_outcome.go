package clickcapture

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	teamDraftAssignmentPrefix = "team-draft-v1:"
	LexicalRevision           = "lexical"
)

type InterleavingOutcome struct {
	PrimaryRevision   string `json:"primary_revision"`
	SecondaryRevision string `json:"secondary_revision"`
	Impressions       int    `json:"impressions"`
	PrimaryClicks     int    `json:"primary_clicks"`
	SecondaryClicks   int    `json:"secondary_clicks"`
}

func teamDraftAssignment(primary, secondary string) (string, error) {
	if !validRankingRevision(primary) || !validRankingRevision(secondary) || primary == secondary {
		return "", fmt.Errorf("team-draft ranking revisions are invalid")
	}
	sum := sha256.Sum256([]byte(primary + "\x00" + secondary))

	return teamDraftAssignmentPrefix + hex.EncodeToString(sum[:12]), nil
}

func mergeInterleavingOutcome(
	current *InterleavingOutcome,
	addition InterleavingOutcome,
) *InterleavingOutcome {
	if current == nil {
		copy := addition

		return &copy
	}
	if current.PrimaryRevision != addition.PrimaryRevision ||
		current.SecondaryRevision != addition.SecondaryRevision {
		return current
	}
	copy := *current
	copy.Impressions = min(
		copy.Impressions+addition.Impressions,
		maximumAggregateValue,
	)

	return &copy
}

func addInterleavingClick(model *ModelEvidence, attribution string) {
	if model.Interleaving == nil ||
		model.Interleaving.PrimaryClicks+model.Interleaving.SecondaryClicks >=
			model.Interleaving.Impressions*maximumClicksPerImpression {
		return
	}
	switch attribution {
	case AttributionPrimary:
		model.Interleaving.PrimaryClicks = incrementAggregate(
			model.Interleaving.PrimaryClicks,
		)
	case AttributionSecondary:
		model.Interleaving.SecondaryClicks = incrementAggregate(
			model.Interleaving.SecondaryClicks,
		)
	}
}

func validRankingRevision(value string) bool {
	if value == "" || len(value) > 128 || value != strings.TrimSpace(value) {
		return false
	}
	for _, character := range []byte(value) {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' ||
			character == '.' {
			continue
		}

		return false
	}

	return true
}

func validateInterleavingEvidence(assignment string, outcome *InterleavingOutcome) error {
	if outcome == nil {
		return nil
	}
	expected, err := teamDraftAssignment(outcome.PrimaryRevision, outcome.SecondaryRevision)
	if err != nil || expected != assignment || !boundedAggregate(outcome.Impressions) ||
		outcome.Impressions == 0 || !boundedAggregate(outcome.PrimaryClicks) ||
		!boundedAggregate(outcome.SecondaryClicks) ||
		outcome.PrimaryClicks > outcome.Impressions*maximumClicksPerImpression ||
		outcome.SecondaryClicks > outcome.Impressions*maximumClicksPerImpression {
		return fmt.Errorf("interleaving evidence is invalid")
	}

	return nil
}
