package rankingtrain

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

type gradedJudgment struct {
	query          string
	queryCluster   string
	observedAt     time.Time
	relevant       map[string]int
	clusterIntents map[string][]string
	navigational   bool
	sliceNames     []string
}

type rankingEvidenceMapper func(
	searchcore.RankingEvidence,
) (rankfit.FeatureVector, bool, error)

type modelCandidate struct {
	identity string
	slot     int
	features rankfit.FeatureVector
}

type queryDataset struct {
	judgment        searcheval.CanonicalJudgment
	request         searchcore.Request
	results         []searchcore.Result
	modelCandidates []modelCandidate
	group           rankfit.QueryGroup
	hasGroup        bool
}

func retrieveCandidateDatasets(
	ctx context.Context,
	searcher searchcore.Searcher,
	judgments []searcheval.Judgment,
) ([]queryDataset, error) {
	graded, err := canonicalGradedJudgments(judgments)
	if err != nil {
		return nil, err
	}
	if len(graded) > MaximumTrainingQueries {
		return nil, fmt.Errorf(
			"training queries exceed the limit of %d",
			MaximumTrainingQueries,
		)
	}
	datasets := make([]queryDataset, len(graded))
	for index, judgment := range graded {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("build ranking candidates: %w", err)
		}
		request := searchcore.RequestWithParsedQuery(searchcore.Request{
			Query: judgment.query,
			Limit: MaximumCandidatesPerQuery,
		})
		response, err := searcher.Search(ctx, request)
		if err != nil {
			return nil, fmt.Errorf("retrieve candidates for query %q: %w", judgment.query, err)
		}
		datasets[index], err = buildQueryDataset(judgment, response.Results)
		if err != nil {
			return nil, fmt.Errorf("build query %q candidates: %w", judgment.query, err)
		}
	}

	return datasets, nil
}

func canonicalGradedJudgments(
	judgments []searcheval.Judgment,
) ([]gradedJudgment, error) {
	return canonicalGradedJudgmentsWithMapper(judgments, learnedrank.MapRankingEvidence)
}

func canonicalGradedJudgmentsWithMapper(
	judgments []searcheval.Judgment,
	mapEvidence rankingEvidenceMapper,
) ([]gradedJudgment, error) {
	if len(judgments) == 0 || len(judgments) > MaximumJudgments {
		return nil, fmt.Errorf(
			"judgments must contain between 1 and %d queries",
			MaximumJudgments,
		)
	}
	validationVector, _, err := mapEvidence(searchcore.RankingEvidence{})
	if err != nil {
		return nil, fmt.Errorf("build relevance validation vector: %w", err)
	}
	graded := make([]gradedJudgment, len(judgments))
	for index, judgment := range judgments {
		query := strings.TrimSpace(judgment.Query)
		if query == "" {
			return nil, fmt.Errorf("judgment query must not be empty")
		}
		relevant, err := gradedRelevance(query, judgment.Relevant, validationVector)
		if err != nil {
			return nil, err
		}
		graded[index] = gradedJudgment{
			query:          query,
			queryCluster:   canonicalQueryCluster(judgment.QueryCluster),
			observedAt:     judgment.ObservedAt,
			relevant:       relevant,
			clusterIntents: cloneStringLists(judgment.ClusterIntents),
			navigational:   judgment.Navigational,
			sliceNames:     append([]string(nil), judgment.SliceNames...),
		}
		if graded[index].queryCluster == "" {
			graded[index].queryCluster = canonicalQueryCluster(query)
		}
	}
	sort.Slice(graded, func(left, right int) bool {
		return graded[left].query < graded[right].query
	})
	for index := 1; index < len(graded); index++ {
		if graded[index-1].query == graded[index].query {
			return nil, fmt.Errorf("judgment query %q is duplicated", graded[index].query)
		}
	}

	return graded, nil
}

func gradedRelevance(
	query string,
	judgments map[string]int,
	validationVector rankfit.FeatureVector,
) (map[string]int, error) {
	relevant := make(map[string]int, len(judgments))
	urls := make([]string, 0, len(judgments))
	for url := range judgments {
		urls = append(urls, url)
	}
	sort.Strings(urls)
	for _, url := range urls {
		if url == "" {
			return nil, fmt.Errorf("judgment %q contains an empty URL", query)
		}
		grade := judgments[url]
		if _, err := rankfit.NewRankingExample("grade", grade, validationVector); err != nil {
			return nil, fmt.Errorf("judgment %q URL %q grade: %w", query, url, err)
		}
		relevant[url] = grade
	}

	return relevant, nil
}

func buildQueryDataset(
	judgment gradedJudgment,
	results []searchcore.Result,
) (queryDataset, error) {
	return buildQueryDatasetWithMapper(judgment, results, learnedrank.MapRankingEvidence)
}

func buildQueryDatasetWithMapper(
	judgment gradedJudgment,
	results []searchcore.Result,
	mapEvidence rankingEvidenceMapper,
) (queryDataset, error) {
	limit := min(len(results), MaximumCandidatesPerQuery)
	results = results[:limit]
	urlIdentity := make(map[string]string, limit*2)
	for _, result := range results {
		identity := rankingCandidateIdentity(result)
		if identity == "" {
			return queryDataset{}, fmt.Errorf("search result has no URL or cluster identity")
		}
		urlIdentity[result.URL] = identity
		if result.RepresentativeURL != "" {
			urlIdentity[result.RepresentativeURL] = identity
		}
	}
	queryCluster := judgment.queryCluster
	if queryCluster == "" {
		queryCluster = canonicalQueryCluster(judgment.query)
	}
	canonical := searcheval.CanonicalJudgment{
		Query:            judgment.query,
		QueryCluster:     queryCluster,
		ObservedAt:       judgment.observedAt,
		RelevantClusters: canonicalRelevantClusters(judgment.relevant, urlIdentity),
		ClusterIntents:   canonicalClusterIntents(judgment.clusterIntents, urlIdentity),
		Navigational:     judgment.navigational,
		SliceNames:       append([]string(nil), judgment.sliceNames...),
	}
	dataset := queryDataset{
		judgment: canonical,
		request: searchcore.RequestWithParsedQuery(searchcore.Request{
			Query: judgment.query,
			Limit: MaximumCandidatesPerQuery,
		}),
	}
	seen := make(map[string]struct{}, limit)
	for _, result := range results {
		identity := rankingCandidateIdentity(result)
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		slot := len(dataset.results)
		dataset.results = append(dataset.results, result)
		if slot >= ModelCandidateWindow {
			continue
		}
		features, known, err := mapEvidence(result.Evidence)
		if err != nil {
			return queryDataset{}, fmt.Errorf("candidate %q evidence: %w", identity, err)
		}
		if !known {
			continue
		}
		dataset.modelCandidates = append(dataset.modelCandidates, modelCandidate{
			identity: identity,
			slot:     slot,
			features: features,
		})
	}
	if len(dataset.modelCandidates) == 0 {
		return dataset, nil
	}
	group, err := gradedQueryGroup(
		judgment.query,
		canonical.RelevantClusters,
		dataset.modelCandidates,
	)
	if err != nil {
		return queryDataset{}, err
	}
	dataset.group = group
	dataset.hasGroup = true

	return dataset, nil
}

func gradedQueryGroup(
	query string,
	relevant map[string]int,
	candidates []modelCandidate,
) (rankfit.QueryGroup, error) {
	examples := make([]rankfit.RankingExample, len(candidates))
	for index, candidate := range candidates {
		example, err := rankfit.NewRankingExample(
			candidate.identity,
			relevant[candidate.identity],
			candidate.features,
		)
		if err != nil {
			return rankfit.QueryGroup{}, fmt.Errorf(
				"candidate %q training example: %w",
				candidate.identity,
				err,
			)
		}
		examples[index] = example
	}
	group, err := rankfit.NewQueryGroup(query, examples)
	if err != nil {
		return rankfit.QueryGroup{}, fmt.Errorf("query group: %w", err)
	}

	return group, nil
}

func canonicalRelevantClusters(
	relevant map[string]int,
	urlIdentity map[string]string,
) map[string]int {
	clusters := make(map[string]int, len(relevant))
	for url, grade := range relevant {
		identity := urlIdentity[url]
		if identity == "" {
			identity = "url:" + url
		}
		clusters[identity] = max(clusters[identity], grade)
	}

	return clusters
}

func canonicalClusterIntents(
	intents map[string][]string,
	urlIdentity map[string]string,
) map[string][]string {
	canonical := make(map[string][]string, len(intents))
	for identity, values := range intents {
		if mapped := urlIdentity[identity]; mapped != "" {
			identity = mapped
		} else if !strings.HasPrefix(identity, "cluster:") &&
			!strings.HasPrefix(identity, "url:") {
			identity = "url:" + identity
		}
		canonical[identity] = append([]string(nil), values...)
	}

	return canonical
}

func cloneStringLists(values map[string][]string) map[string][]string {
	cloned := make(map[string][]string, len(values))
	for key, items := range values {
		cloned[key] = append([]string(nil), items...)
	}

	return cloned
}

func rankingCandidateIdentity(result searchcore.Result) string {
	if result.ClusterID != "" {
		return "cluster:" + result.ClusterID
	}
	if result.URL != "" {
		return "url:" + result.URL
	}

	return ""
}

func canonicalRankedCandidate(
	result searchcore.Result,
	identity string,
) searcheval.RankedCandidate {
	return searcheval.RankedCandidate{
		URL:               result.URL,
		CanonicalCluster:  identity,
		RegistrableDomain: result.Host,
		Score:             result.Score,
		Unsafe:            result.SafetyRating == searchcore.SafetyExplicit,
		Spam:              result.SpamRisk >= 1,
	}
}

func canonicalQueryCluster(query string) string {
	return strings.Join(strings.Fields(strings.ToLower(query)), " ")
}
