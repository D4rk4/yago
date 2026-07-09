// Package clickcapture persists the implicit relevance signal mined from result
// clicks on the public search page and derives graded judgments from it, so the
// ranking learner (YagoRank, ADR-0035) can fit weights against real usage on top
// of the operator-curated qrels. Capture is opt-in and stores only aggregates —
// a normalized query paired with per-URL click counts and a position-debiased
// weight — never per-user, per-session, or per-event data.
package clickcapture

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searcheval"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const clickBucket vault.Name = "search_clicks"

// maxURLsPerQuery bounds a single query's record so a spammed query cannot grow
// it without limit; when full, the lowest-weight URL is dropped to admit a new
// one, keeping the URLs that carry the most signal.
const maxURLsPerQuery = 64

// dominanceFraction is the share of a query's strongest position-weighted URL at
// or above which another clicked URL is also graded highly relevant; below it a
// clicked URL is graded relevant. It keeps a clear winner at the top grade while
// still crediting other genuinely-clicked results.
const dominanceFraction = 0.5

// gradeRelevant and gradeHighlyRelevant are the two implicit grades, on the same
// small integer scale the curated judgments and NDCG gain (2^grade-1) use.
const (
	gradeRelevant       = 1
	gradeHighlyRelevant = 2
)

// URLClicks is the accumulated click signal for one result URL under a query:
// the raw click count and a position-debiased weight (see positionWeight).
type URLClicks struct {
	Clicks int     `json:"clicks"`
	Weight float64 `json:"weight"`
}

// QueryClicks is the click signal captured for one normalized query.
type QueryClicks struct {
	Query string               `json:"query"`
	URLs  map[string]URLClicks `json:"urls"`
}

type clickCodec struct{}

func (clickCodec) Encode(q QueryClicks) ([]byte, error) {
	data, _ := json.Marshal(q)

	return data, nil
}

func (clickCodec) Decode(raw []byte) (QueryClicks, error) {
	var q QueryClicks
	if err := json.Unmarshal(raw, &q); err != nil {
		return QueryClicks{}, fmt.Errorf("decode click record: %w", err)
	}

	return q, nil
}

// Store persists captured click signal in the vault.
type Store struct {
	vault   *vault.Vault
	records *vault.Collection[QueryClicks]
}

// Open registers the click-capture collection.
func Open(v *vault.Vault) (*Store, error) {
	records, err := vault.Register(v, clickBucket, clickCodec{})
	if err != nil {
		return nil, fmt.Errorf("register search clicks: %w", err)
	}

	return &Store{vault: v, records: records}, nil
}

// normalizeQuery lowercases and collapses whitespace so clicks under the same
// query accumulate on one record, matching how the curated judgment store keys.
func normalizeQuery(raw string) string {
	return strings.Join(strings.Fields(strings.ToLower(raw)), " ")
}

// positionWeight is the inverse-propensity weight of a click at a 1-based result
// rank: it upweights clicks on lower-ranked results because those positions are
// examined less, correcting the position bias that would otherwise let the top
// slot dominate purely by exposure. The examination proxy is 1/log2(rank+1) —
// the same rank discount NDCG applies — so the weight is log2(rank+1).
func positionWeight(rank int) float64 {
	if rank < 1 {
		rank = 1
	}

	return math.Log2(float64(rank) + 1)
}

// Record adds one result click for a query at a 1-based rank. The query and URL
// must be non-empty. Clicks accumulate per URL and the URL's weight grows by the
// click's position weight; a record holds at most maxURLsPerQuery URLs.
func (s *Store) Record(ctx context.Context, query, url string, rank int) error {
	key := normalizeQuery(query)
	if key == "" {
		return fmt.Errorf("click query must not be empty")
	}
	target := strings.TrimSpace(url)
	if target == "" {
		return fmt.Errorf("click url must not be empty")
	}

	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		record, ok, err := s.records.Get(tx, vault.Key(key))
		if err != nil {
			return fmt.Errorf("read click record: %w", err)
		}
		if !ok || record.URLs == nil {
			record = QueryClicks{Query: key, URLs: map[string]URLClicks{}}
		}
		admitClick(&record, target, positionWeight(rank))

		return s.records.Put(tx, vault.Key(key), record)
	}); err != nil {
		return fmt.Errorf("record click: %w", err)
	}

	return nil
}

// admitClick applies one weighted click to the record, evicting the lowest-weight
// URL first when a new URL would exceed maxURLsPerQuery.
func admitClick(record *QueryClicks, url string, weight float64) {
	if _, seen := record.URLs[url]; !seen && len(record.URLs) >= maxURLsPerQuery {
		evictLightest(record.URLs)
	}
	stat := record.URLs[url]
	stat.Clicks++
	stat.Weight += weight
	record.URLs[url] = stat
}

func evictLightest(urls map[string]URLClicks) {
	lightestURL := ""
	lightest := math.Inf(1)
	for url, stat := range urls {
		if stat.Weight < lightest {
			lightest, lightestURL = stat.Weight, url
		}
	}
	delete(urls, lightestURL)
}

// Aggregates returns every query's captured clicks ordered by query.
func (s *Store) Aggregates(ctx context.Context) ([]QueryClicks, error) {
	var all []QueryClicks
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		return s.records.Scan(tx, nil, func(_ vault.Key, q QueryClicks) (bool, error) {
			all = append(all, q)

			return true, nil
		})
	}); err != nil {
		return nil, fmt.Errorf("list click records: %w", err)
	}
	sort.Slice(all, func(i, k int) bool { return all[i].Query < all[k].Query })

	return all, nil
}

// ImplicitJudgments derives graded judgments from the captured clicks, admitting
// only queries with at least minClicks total clicks so single-click noise does
// not steer the learner.
func (s *Store) ImplicitJudgments(
	ctx context.Context,
	minClicks int,
) ([]searcheval.Judgment, error) {
	aggregates, err := s.Aggregates(ctx)
	if err != nil {
		return nil, err
	}

	return DeriveJudgments(aggregates, minClicks), nil
}

// DeriveJudgments converts captured click aggregates into graded judgments. For
// each query with at least minClicks total clicks, every clicked URL is graded
// relevant, and those whose position-debiased weight reaches dominanceFraction of
// the query's strongest URL are graded highly relevant. Queries below the click
// floor, or with no positive signal, are skipped as noise.
func DeriveJudgments(aggregates []QueryClicks, minClicks int) []searcheval.Judgment {
	if minClicks < 1 {
		minClicks = 1
	}
	judgments := make([]searcheval.Judgment, 0, len(aggregates))
	for _, agg := range aggregates {
		total, top := queryTotals(agg.URLs)
		if total < minClicks || top <= 0 {
			continue
		}
		// total >= minClicks >= 1 guarantees a URL with a positive click count,
		// which gradeURLs always grades, so the grade map is never empty here.
		judgments = append(
			judgments,
			searcheval.Judgment{Query: agg.Query, Relevant: gradeURLs(agg.URLs, top)},
		)
	}
	sort.Slice(judgments, func(i, k int) bool {
		return judgments[i].Query < judgments[k].Query
	})

	return judgments
}

// queryTotals returns the total click count and the strongest position-weighted
// score across a query's clicked URLs. Only URLs with a positive click count set
// the dominance reference, so an unclicked entry never suppresses real grades.
func queryTotals(urls map[string]URLClicks) (int, float64) {
	total := 0
	top := 0.0
	for _, stat := range urls {
		if stat.Clicks < 1 {
			continue
		}
		total += stat.Clicks
		if stat.Weight > top {
			top = stat.Weight
		}
	}

	return total, top
}

// gradeURLs grades every clicked URL relative to the query's strongest weight.
func gradeURLs(urls map[string]URLClicks, top float64) map[string]int {
	grades := make(map[string]int, len(urls))
	for url, stat := range urls {
		if stat.Clicks < 1 {
			continue
		}
		grade := gradeRelevant
		if stat.Weight >= dominanceFraction*top {
			grade = gradeHighlyRelevant
		}
		grades[url] = grade
	}

	return grades
}
