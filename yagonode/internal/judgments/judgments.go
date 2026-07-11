// Package judgments persists operator-curated relevance judgments — a query
// paired with graded result URLs — as the human-labelled training and
// regression set the ranking learner (YagoRank, ADR-0035) fits weights against.
// The corpus can also yield pseudo-judgments (searcheval.PseudoJudgments); this
// store holds the curated ones, which outrank pseudo-labels in quality, keyed by
// a normalized query so re-curating the same query updates one record.
package judgments

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const judgmentBucket = "search_judgments"

// Judgment is one query with the graded relevance of result URLs: grade 0 marks
// an explicitly non-relevant URL, higher grades are more relevant.
type Judgment struct {
	Query          string              `json:"query"`
	QueryCluster   string              `json:"query_cluster,omitempty"`
	ObservedAt     time.Time           `json:"observed_at,omitempty"`
	Grades         map[string]int      `json:"grades"`
	ClusterIntents map[string][]string `json:"cluster_intents,omitempty"`
	Navigational   bool                `json:"navigational,omitempty"`
	SliceNames     []string            `json:"slice_names,omitempty"`
}

type judgmentCodec struct{}

func (judgmentCodec) Encode(j Judgment) ([]byte, error) {
	data, _ := json.Marshal(j)

	return data, nil
}

func (judgmentCodec) Decode(raw []byte) (Judgment, error) {
	var j Judgment
	if err := json.Unmarshal(raw, &j); err != nil {
		return Judgment{}, fmt.Errorf("decode judgment: %w", err)
	}

	return j, nil
}

// Store persists curated judgments in the vault.
type Store struct {
	vault   *vault.Vault
	records *vault.Collection[Judgment]
}

// Open registers the judgment collection.
func Open(v *vault.Vault) (*Store, error) {
	records, err := vault.Register(v, judgmentBucket, judgmentCodec{})
	if err != nil {
		return nil, fmt.Errorf("register search judgments: %w", err)
	}

	return &Store{vault: v, records: records}, nil
}

// normalizeQuery lowercases and collapses whitespace so the same query curated
// twice updates one record rather than accumulating near-duplicates.
func normalizeQuery(raw string) string {
	return strings.Join(strings.Fields(strings.ToLower(raw)), " ")
}

// Put upserts a judgment. The query must be non-empty, at least one URL must be
// graded (a judgment with no graded URL cannot score a ranking), every URL must
// be non-empty, and grades must not be negative.
func (s *Store) Put(ctx context.Context, judgment Judgment) error {
	query := normalizeQuery(judgment.Query)
	if query == "" {
		return fmt.Errorf("judgment query must not be empty")
	}
	if len(judgment.Grades) == 0 {
		return fmt.Errorf("judgment %q must grade at least one url", query)
	}
	grades := make(map[string]int, len(judgment.Grades))
	for url, grade := range judgment.Grades {
		trimmed := strings.TrimSpace(url)
		if trimmed == "" {
			return fmt.Errorf("judgment %q has an empty url", query)
		}
		if grade < 0 {
			return fmt.Errorf("judgment %q grade for %q must not be negative", query, trimmed)
		}
		grades[trimmed] = grade
	}

	persisted := Judgment{
		Query:          query,
		QueryCluster:   normalizeQuery(judgment.QueryCluster),
		ObservedAt:     judgment.ObservedAt.UTC(),
		Grades:         grades,
		ClusterIntents: cloneClusterIntents(judgment.ClusterIntents),
		Navigational:   judgment.Navigational,
		SliceNames:     append([]string(nil), judgment.SliceNames...),
	}
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		return s.records.Put(tx, vault.Key(query), persisted)
	}); err != nil {
		return fmt.Errorf("put judgment: %w", err)
	}

	return nil
}

func cloneClusterIntents(intents map[string][]string) map[string][]string {
	cloned := make(map[string][]string, len(intents))
	for identity, values := range intents {
		cloned[identity] = append([]string(nil), values...)
	}

	return cloned
}

// Delete removes a curated judgment, reporting whether one existed.
func (s *Store) Delete(ctx context.Context, query string) (bool, error) {
	key := normalizeQuery(query)
	if key == "" {
		return false, fmt.Errorf("judgment query must not be empty")
	}
	removed := false
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		deleted, err := s.records.Delete(tx, vault.Key(key))
		removed = deleted

		return err //nolint:wrapcheck // wrapped by the outer "delete judgment" handler below
	}); err != nil {
		return false, fmt.Errorf("delete judgment: %w", err)
	}

	return removed, nil
}

// List returns every curated judgment ordered by query, for stable display and
// deterministic training.
func (s *Store) List(ctx context.Context) ([]Judgment, error) {
	var judgments []Judgment
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		return s.records.Scan(tx, nil, func(_ vault.Key, judgment Judgment) (bool, error) {
			judgments = append(judgments, judgment)

			return true, nil
		})
	}); err != nil {
		return nil, fmt.Errorf("list judgments: %w", err)
	}
	sort.Slice(judgments, func(i, k int) bool { return judgments[i].Query < judgments[k].Query })

	return judgments, nil
}
