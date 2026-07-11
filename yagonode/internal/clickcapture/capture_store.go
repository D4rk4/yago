package clickcapture

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	clickBucket                 vault.Name = "search_clicks"
	clickEvidenceVersion                   = 3
	legacyClickEvidenceVersion             = 2
	maximumModelsPerQuery                  = 8
	maximumResultsPerModel                 = 64
	maximumStoredQueries                   = 4096
	maximumAggregateValue                  = 1_000_000_000
	maximumAggregateWeight                 = 10_000_000_000
	maximumInversePropensity               = 10.0
	adjacentPairModelAssignment            = "fair-pairs-v2"
)

type ResultEvidence struct {
	URLIdentity           string  `json:"url_identity"`
	ClusterIdentity       string  `json:"cluster_identity"`
	Impressions           int     `json:"impressions"`
	RandomizedImpressions int     `json:"randomized_impressions"`
	Clicks                int     `json:"clicks"`
	ClippedExposureWeight float64 `json:"clipped_exposure_weight"`
	ClippedClickWeight    float64 `json:"clipped_click_weight"`
}

type ModelEvidence struct {
	Assignment            string                      `json:"assignment"`
	Impressions           int                         `json:"impressions"`
	RandomizedImpressions int                         `json:"randomized_impressions"`
	Results               map[string]ResultEvidence   `json:"results"`
	FairPairs             map[string]FairPairEvidence `json:"fair_pairs,omitempty"`
	Interleaving          *InterleavingOutcome        `json:"interleaving,omitempty"`
}

type QueryEvidence struct {
	Version        int                      `json:"version"`
	Query          string                   `json:"query"`
	ObservedAtUnix int64                    `json:"observed_at_unix,omitempty"`
	Models         map[string]ModelEvidence `json:"models"`
}

type PreparedImpression struct {
	Token      string
	Candidates []DisplayedCandidate
}

type DraftRanking struct {
	Revision   string
	Candidates []Candidate
}

type evidenceCodec struct{}

type legacyQueryClicks struct {
	Query string `json:"query"`
}

func (evidenceCodec) Encode(evidence QueryEvidence) ([]byte, error) {
	if err := validateQueryEvidence(evidence); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(evidence)

	return encoded, nil
}

func (evidenceCodec) Decode(raw []byte) (QueryEvidence, error) {
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return QueryEvidence{}, fmt.Errorf("decode click evidence header: %w", err)
	}
	if header.Version == 0 {
		var legacy legacyQueryClicks
		if err := json.Unmarshal(raw, &legacy); err != nil {
			return QueryEvidence{}, fmt.Errorf("decode legacy click evidence: %w", err)
		}
		query := normalizeQuery(legacy.Query)
		if query == "" {
			return QueryEvidence{}, fmt.Errorf("legacy click evidence query is empty")
		}

		return newQueryEvidence(query), nil
	}
	if header.Version == legacyClickEvidenceVersion {
		var evidence QueryEvidence
		if err := json.Unmarshal(raw, &evidence); err != nil {
			return QueryEvidence{}, fmt.Errorf("decode legacy click evidence: %w", err)
		}
		evidence.Version = clickEvidenceVersion
		if err := validateQueryEvidence(evidence); err != nil {
			return QueryEvidence{}, err
		}

		return evidence, nil
	}
	if header.Version != clickEvidenceVersion {
		return QueryEvidence{}, fmt.Errorf(
			"click evidence version %d is unsupported",
			header.Version,
		)
	}
	var evidence QueryEvidence
	if err := json.Unmarshal(raw, &evidence); err != nil {
		return QueryEvidence{}, fmt.Errorf("decode click evidence: %w", err)
	}
	if err := validateQueryEvidence(evidence); err != nil {
		return QueryEvidence{}, err
	}

	return evidence, nil
}

type Store struct {
	vault   *vault.Vault
	records *vault.Collection[QueryEvidence]
	issuer  *Issuer
}

func Open(v *vault.Vault) (*Store, error) {
	return OpenWithSources(v, nil, nil)
}

func OpenWithSources(
	v *vault.Vault,
	entropy io.Reader,
	clock func() time.Time,
) (*Store, error) {
	issuer, err := NewIssuer(entropy, clock)
	if err != nil {
		return nil, err
	}
	records, err := vault.Register(v, clickBucket, evidenceCodec{})
	if err != nil {
		return nil, fmt.Errorf("register search click evidence: %w", err)
	}

	return &Store{vault: v, records: records, issuer: issuer}, nil
}

func (s *Store) PrepareImpression(
	ctx context.Context,
	query string,
	candidates []Candidate,
) (PreparedImpression, error) {
	seed, err := s.issuer.experimentSeed()
	if err != nil {
		return PreparedImpression{}, err
	}
	displayed := AdjacentPairRandomization(candidates, seed)
	normalizedQuery := normalizeQuery(query)
	token, claims, err := s.issuer.issue(
		normalizedQuery,
		adjacentPairModelAssignment,
		displayed,
	)
	if err != nil {
		return PreparedImpression{}, err
	}
	if err := s.recordImpression(ctx, claims); err != nil {
		return PreparedImpression{}, err
	}

	return PreparedImpression{Token: token, Candidates: displayed}, nil
}

func (s *Store) PrepareTeamDraft(
	ctx context.Context,
	query string,
	primary DraftRanking,
	secondary DraftRanking,
	limit int,
) (PreparedImpression, error) {
	seed, err := s.issuer.experimentSeed()
	if err != nil {
		return PreparedImpression{}, err
	}
	displayed := TeamDraftInterleave(primary.Candidates, secondary.Candidates, seed, limit)
	assignment, err := teamDraftAssignment(primary.Revision, secondary.Revision)
	if err != nil {
		return PreparedImpression{}, err
	}
	token, claims, err := s.issuer.issue(query, assignment, displayed)
	if err != nil {
		return PreparedImpression{}, err
	}
	comparison := InterleavingOutcome{
		PrimaryRevision:   primary.Revision,
		SecondaryRevision: secondary.Revision,
	}
	if err := s.recordInterleavingImpression(ctx, claims, comparison); err != nil {
		return PreparedImpression{}, err
	}

	return PreparedImpression{Token: token, Candidates: displayed}, nil
}

func (s *Store) RecordClick(
	ctx context.Context,
	token string,
	urlIdentity string,
	position int,
) error {
	click, err := s.issuer.ValidateClick(token, urlIdentity, position)
	if err != nil {
		return err
	}
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		record, ok, readErr := s.records.Get(tx, vault.Key(click.Query))
		if readErr != nil {
			return fmt.Errorf("read click evidence: %w", readErr)
		}
		if !ok {
			return fmt.Errorf("signed impression aggregate is unavailable")
		}
		model, ok := record.Models[click.ModelAssignment]
		if !ok {
			return fmt.Errorf("signed impression model aggregate is unavailable")
		}
		result, ok := model.Results[click.Candidate.ClusterIdentity]
		if !ok || result.Clicks >= result.Impressions {
			return fmt.Errorf("signed impression result aggregate is unavailable")
		}
		result.Clicks = incrementAggregate(result.Clicks)
		if measuredPropensity(click.Candidate.Propensity) {
			result.ClippedClickWeight = addAggregateWeight(
				result.ClippedClickWeight,
				inversePropensity(click.Candidate.Propensity),
			)
		}
		model.Results[click.Candidate.ClusterIdentity] = result
		if click.Pair != nil {
			addFairPairClick(&model, click.Candidate.ClusterIdentity, *click.Pair)
		}
		addInterleavingClick(&model, click.Candidate.Attribution)
		record.Models[click.ModelAssignment] = model

		return s.records.Put(tx, vault.Key(click.Query), record)
	}); err != nil {
		return fmt.Errorf("record validated click: %w", err)
	}

	return nil
}

func (s *Store) recordImpression(ctx context.Context, claims impressionClaims) error {
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		return s.updateImpressionEvidence(tx, claims)
	}); err != nil {
		return fmt.Errorf("record impression: %w", err)
	}

	return nil
}

func (s *Store) recordInterleavingImpression(
	ctx context.Context,
	claims impressionClaims,
	comparison InterleavingOutcome,
) error {
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		return s.updateInterleavingEvidence(tx, claims, comparison)
	}); err != nil {
		return fmt.Errorf("record interleaving impression: %w", err)
	}

	return nil
}

func (s *Store) updateImpressionEvidence(tx *vault.Txn, claims impressionClaims) error {
	record, ok, err := s.records.Get(tx, vault.Key(claims.query))
	if err != nil {
		return fmt.Errorf("read impression evidence: %w", err)
	}
	if !ok {
		if err := s.ensureQueryCapacity(tx); err != nil {
			return err
		}
		record = newQueryEvidence(claims.query)
	}
	if record.Query != claims.query {
		return fmt.Errorf("impression evidence query does not match its key")
	}
	model, err := impressionModel(record, claims.modelAssignment)
	if err != nil {
		return err
	}
	addImpressionResults(&model, claims.results)
	addFairPairImpressions(&model, claims.results)
	record.Models[claims.modelAssignment] = model
	record.ObservedAtUnix = max(record.ObservedAtUnix, claims.issuedAt)

	if err := s.records.Put(tx, vault.Key(claims.query), record); err != nil {
		return fmt.Errorf("store impression evidence: %w", err)
	}

	return nil
}

func (s *Store) updateInterleavingEvidence(
	tx *vault.Txn,
	claims impressionClaims,
	comparison InterleavingOutcome,
) error {
	record, ok, err := s.records.Get(tx, vault.Key(claims.query))
	if err != nil {
		return fmt.Errorf("read interleaving evidence: %w", err)
	}
	if !ok {
		if err := s.ensureQueryCapacity(tx); err != nil {
			return err
		}
		record = newQueryEvidence(claims.query)
	}
	model, err := impressionModel(record, claims.modelAssignment)
	if err != nil {
		return err
	}
	comparison.Impressions = incrementAggregate(comparison.Impressions)
	model.Interleaving = mergeInterleavingOutcome(model.Interleaving, comparison)
	addImpressionResults(&model, claims.results)
	record.Models[claims.modelAssignment] = model
	record.ObservedAtUnix = max(record.ObservedAtUnix, claims.issuedAt)
	if err := s.records.Put(tx, vault.Key(claims.query), record); err != nil {
		return fmt.Errorf("store interleaving evidence: %w", err)
	}

	return nil
}

func impressionModel(record QueryEvidence, assignment string) (ModelEvidence, error) {
	model, exists := record.Models[assignment]
	if !exists {
		if len(record.Models) >= maximumModelsPerQuery {
			return ModelEvidence{}, fmt.Errorf("impression model bound reached")
		}
		model = ModelEvidence{Assignment: assignment, Results: map[string]ResultEvidence{}}
	}
	model.Impressions = incrementAggregate(model.Impressions)

	return model, nil
}

func addImpressionResults(model *ModelEvidence, candidates []DisplayedCandidate) {
	randomized := false
	for _, candidate := range candidates {
		if _, present := model.Results[candidate.ClusterIdentity]; !present &&
			len(model.Results) >= maximumResultsPerModel {
			evictWeakestResult(model.Results)
		}
		result := model.Results[candidate.ClusterIdentity]
		result.URLIdentity = representativeURL(result.URLIdentity, candidate.URLIdentity)
		result.ClusterIdentity = candidate.ClusterIdentity
		result.Impressions = incrementAggregate(result.Impressions)
		if measuredPropensity(candidate.Propensity) {
			randomized = true
			result.RandomizedImpressions = incrementAggregate(result.RandomizedImpressions)
			result.ClippedExposureWeight = addAggregateWeight(
				result.ClippedExposureWeight,
				inversePropensity(candidate.Propensity),
			)
		}
		model.Results[candidate.ClusterIdentity] = result
	}
	if randomized {
		model.RandomizedImpressions = incrementAggregate(model.RandomizedImpressions)
	}
}

func (s *Store) ensureQueryCapacity(tx *vault.Txn) error {
	length, err := s.records.Len(tx)
	if err != nil {
		return fmt.Errorf("read impression query count: %w", err)
	}
	if length < maximumStoredQueries {
		return nil
	}
	weakestKey := vault.Key(nil)
	weakestImpressions := math.MaxInt
	if err := s.records.Scan(tx, nil, func(key vault.Key, evidence QueryEvidence) (bool, error) {
		impressions := queryImpressions(evidence)
		if impressions < weakestImpressions ||
			impressions == weakestImpressions && string(key) > string(weakestKey) {
			weakestKey = append(weakestKey[:0], key...)
			weakestImpressions = impressions
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("scan impression query capacity: %w", err)
	}
	if len(weakestKey) == 0 {
		return fmt.Errorf("impression query capacity has no eviction candidate")
	}
	if _, err := s.records.Delete(tx, weakestKey); err != nil {
		return fmt.Errorf("evict impression query: %w", err)
	}

	return nil
}

func (s *Store) Aggregates(ctx context.Context) ([]QueryEvidence, error) {
	var aggregates []QueryEvidence
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		return s.records.Scan(tx, nil, func(_ vault.Key, evidence QueryEvidence) (bool, error) {
			aggregates = append(aggregates, evidence)

			return true, nil
		})
	}); err != nil {
		return nil, fmt.Errorf("list click evidence: %w", err)
	}
	sort.Slice(aggregates, func(left, right int) bool {
		return aggregates[left].Query < aggregates[right].Query
	})

	return aggregates, nil
}

func newQueryEvidence(query string) QueryEvidence {
	return QueryEvidence{
		Version: clickEvidenceVersion,
		Query:   query,
		Models:  map[string]ModelEvidence{},
	}
}

func measuredPropensity(propensity float64) bool {
	return propensity >= minimumMeasuredPropensity && propensity <= 1 &&
		!math.IsNaN(propensity) && !math.IsInf(propensity, 0)
}

func inversePropensity(propensity float64) float64 {
	return min(1/propensity, maximumInversePropensity)
}

func incrementAggregate(value int) int {
	return min(value+1, maximumAggregateValue)
}

func addAggregateWeight(value, addition float64) float64 {
	return min(value+addition, maximumAggregateWeight)
}

func representativeURL(current, candidate string) string {
	if current == "" || candidate < current {
		return candidate
	}

	return current
}

func evictWeakestResult(results map[string]ResultEvidence) {
	weakest := ""
	for identity, evidence := range results {
		if weakest == "" || weakerResult(evidence, results[weakest]) ||
			sameResultStrength(evidence, results[weakest]) && identity > weakest {
			weakest = identity
		}
	}
	delete(results, weakest)
}

func sameResultStrength(left, right ResultEvidence) bool {
	return left.RandomizedImpressions == right.RandomizedImpressions &&
		left.Clicks == right.Clicks && left.Impressions == right.Impressions
}

func weakerResult(left, right ResultEvidence) bool {
	if left.RandomizedImpressions != right.RandomizedImpressions {
		return left.RandomizedImpressions < right.RandomizedImpressions
	}
	if left.Clicks != right.Clicks {
		return left.Clicks < right.Clicks
	}

	return left.Impressions < right.Impressions
}

func queryImpressions(evidence QueryEvidence) int {
	total := 0
	for _, model := range evidence.Models {
		total = min(total+model.Impressions, maximumAggregateValue)
	}

	return total
}

func validateQueryEvidence(evidence QueryEvidence) error {
	if evidence.ObservedAtUnix < 0 {
		return fmt.Errorf("click evidence observation time is invalid")
	}
	if evidence.Version != clickEvidenceVersion || evidence.Query == "" ||
		evidence.Query != normalizeQuery(evidence.Query) ||
		len(evidence.Query) > maximumNormalizedQueryBytes {
		return fmt.Errorf("click evidence identity is invalid")
	}
	if evidence.Models == nil || len(evidence.Models) > maximumModelsPerQuery {
		return fmt.Errorf("click evidence model set is invalid")
	}
	for assignment, model := range evidence.Models {
		if err := validateModelEvidence(assignment, model); err != nil {
			return err
		}
	}

	return nil
}

func validateModelEvidence(assignment string, model ModelEvidence) error {
	if assignment == "" || assignment != strings.TrimSpace(assignment) ||
		len(assignment) > maximumModelAssignmentBytes || model.Assignment != assignment {
		return fmt.Errorf("click evidence model identity is invalid")
	}
	if !boundedAggregate(model.Impressions) ||
		!boundedAggregate(model.RandomizedImpressions) ||
		model.RandomizedImpressions > model.Impressions || model.Results == nil ||
		len(model.Results) > maximumResultsPerModel {
		return fmt.Errorf("click evidence model aggregate is invalid")
	}
	for identity, result := range model.Results {
		if err := validateResultEvidence(identity, result); err != nil {
			return err
		}
	}
	if err := validateFairPairEvidence(model.FairPairs); err != nil {
		return err
	}
	if err := validateInterleavingEvidence(assignment, model.Interleaving); err != nil {
		return err
	}

	return nil
}

func validateResultEvidence(identity string, result ResultEvidence) error {
	if identity == "" || identity != result.ClusterIdentity ||
		len(identity) > maximumClusterIdentityBytes || result.URLIdentity == "" ||
		len(result.URLIdentity) > maximumURLIdentityBytes {
		return fmt.Errorf("click evidence result identity is invalid")
	}
	if !boundedAggregate(result.Impressions) ||
		!boundedAggregate(result.RandomizedImpressions) ||
		!boundedAggregate(result.Clicks) ||
		result.RandomizedImpressions > result.Impressions ||
		result.Clicks > result.Impressions {
		return fmt.Errorf("click evidence result aggregate is invalid")
	}
	if !boundedWeight(result.ClippedExposureWeight) ||
		!boundedWeight(result.ClippedClickWeight) ||
		result.ClippedClickWeight > result.ClippedExposureWeight {
		return fmt.Errorf("click evidence propensity aggregate is invalid")
	}

	return nil
}

func boundedAggregate(value int) bool {
	return value >= 0 && value <= maximumAggregateValue
}

func boundedWeight(value float64) bool {
	return value >= 0 && value <= maximumAggregateWeight &&
		!math.IsNaN(value) && !math.IsInf(value, 0)
}
