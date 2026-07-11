//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
)

const (
	pathRankingProfile   = "/api/admin/v1/search/ranking"
	pathRankingJudgments = "/api/admin/v1/search/judgments"
	rankingRevision      = "e2e-linear-v1"
	rankingModelKind     = "linear_lambdarank"
)

type rankingPromotionPartition struct {
	Queries       int `json:"queries"`
	Candidates    int `json:"candidates"`
	ModelExamples int `json:"model_examples"`
}

type rankingPromotionDataset struct {
	Train       rankingPromotionPartition `json:"train"`
	Development rankingPromotionPartition `json:"development"`
	Test        rankingPromotionPartition `json:"test"`
}

type rankingPromotionMetrics struct {
	Queries  int     `json:"queries"`
	NDCGAt10 float64 `json:"ndcg_at_10"`
}

type rankingPromotionComparison struct {
	Baseline  rankingPromotionMetrics `json:"baseline"`
	Candidate rankingPromotionMetrics `json:"candidate"`
}

type rankingPromotionTrainingResponse struct {
	Revision  string                  `json:"revision"`
	ModelKind string                  `json:"model_kind"`
	Promoted  bool                    `json:"promoted"`
	Dataset   rankingPromotionDataset `json:"dataset"`
	Training  struct {
		PreferencePairs int `json:"preference_pairs"`
		Iterations      int `json:"iterations"`
	} `json:"training"`
	Development rankingPromotionComparison `json:"development"`
	Test        rankingPromotionComparison `json:"test"`
	Promotion   struct {
		Promote               bool     `json:"promote"`
		LowerRelativeGain     float64  `json:"lower_relative_gain"`
		Confidence            float64  `json:"confidence"`
		Samples               int      `json:"samples"`
		QueryClusters         int      `json:"query_clusters"`
		Reasons               []string `json:"reasons"`
		ComparedWithIncumbent bool     `json:"compared_with_incumbent"`
	} `json:"promotion"`
}

type rankingPromotionModelResponse struct {
	Status struct {
		Current struct {
			Active    bool   `json:"active"`
			Revision  string `json:"revision"`
			ModelKind string `json:"model_kind"`
		} `json:"current"`
	} `json:"status"`
	ActiveSnapshot struct {
		Format    string `json:"format"`
		Revision  string `json:"revision"`
		ModelKind string `json:"model_kind"`
		Model     struct {
			Format   string `json:"format"`
			Features []struct {
				Name string `json:"name"`
			} `json:"features"`
			Weights []float64 `json:"weights"`
		} `json:"model"`
	} `json:"active_snapshot"`
}

type rankingPromotionExplainResponse struct {
	ModelRevision string `json:"modelRevision"`
	ModelKind     string `json:"modelKind"`
	Results       []struct {
		URL          string  `json:"url"`
		Quality      float64 `json:"quality"`
		QualityKnown bool    `json:"qualityKnown"`
		Learned      *struct {
			OriginalRank int `json:"original_rank"`
			ModelRank    int `json:"model_rank"`
			FinalRank    int `json:"final_rank"`
			Signals      []struct {
				Name         string  `json:"name"`
				Used         bool    `json:"used"`
				Weight       float64 `json:"weight"`
				Contribution float64 `json:"contribution"`
			} `json:"signals"`
		} `json:"learned"`
	} `json:"results"`
}

type rankingPromotionPublicResponse struct {
	Channels []struct {
		Items []struct {
			Link string `json:"link"`
		} `json:"items"`
	} `json:"channels"`
}

func createRankingPromotionProfile(
	t *testing.T,
	ctx context.Context,
	opsURL string,
	session adminSession,
	seed string,
) string {
	t.Helper()
	var response struct {
		ProfileHandle string `json:"profileHandle"`
	}
	postRankingAdminJSON(
		t,
		ctx,
		opsURL+pathCrawl,
		session,
		map[string]any{
			"name":            "ranking-promotion-e2e",
			"seeds":           []string{seed},
			"scope":           "domain",
			"maxDepth":        0,
			"maxPagesPerHost": -1,
		},
		http.StatusAccepted,
		&response,
	)
	if response.ProfileHandle == "" {
		t.Fatal("ranking crawl profile has an empty handle")
	}

	return response.ProfileHandle
}

func applyRankingPromotionProfile(
	t *testing.T,
	ctx context.Context,
	opsURL string,
	session adminSession,
) {
	t.Helper()
	postRankingAdminJSON(
		t,
		ctx,
		opsURL+pathRankingProfile,
		session,
		map[string]any{"weights": map[string]float64{
			"title":     12,
			"headings":  4,
			"anchors":   1,
			"body":      0.1,
			"url":       0.1,
			"hostRank":  0,
			"freshness": 0,
			"quality":   0,
			"proximity": 0,
		}},
		http.StatusOK,
		nil,
	)
}

func storeRankingPromotionJudgments(
	t *testing.T,
	ctx context.Context,
	opsURL string,
	session adminSession,
	corpus rankingPromotionCorpus,
) {
	t.Helper()
	for _, query := range corpus.queries {
		postRankingAdminJSON(
			t,
			ctx,
			opsURL+pathRankingJudgments,
			session,
			map[string]any{
				"query":         query.query,
				"query_cluster": query.cluster,
				"grades": map[string]int{
					query.badURL:    0,
					query.middleURL: 1,
					query.goodURL:   3,
				},
			},
			http.StatusOK,
			nil,
		)
	}
}

func trainRankingPromotionModel(
	t *testing.T,
	ctx context.Context,
	opsURL string,
	session adminSession,
) rankingPromotionTrainingResponse {
	t.Helper()
	var response rankingPromotionTrainingResponse
	postRankingAdminJSON(
		t,
		ctx,
		opsURL+pathTrain,
		session,
		map[string]string{
			"revision":   rankingRevision,
			"model_kind": rankingModelKind,
		},
		http.StatusOK,
		&response,
	)

	return response
}

func rankingPromotionModel(
	t *testing.T,
	ctx context.Context,
	opsURL string,
	session adminSession,
) rankingPromotionModelResponse {
	t.Helper()
	var response rankingPromotionModelResponse
	getRankingJSON(t, ctx, opsURL+pathModel, session.cookie, &response)

	return response
}

func rankingPromotionExplain(
	t *testing.T,
	ctx context.Context,
	opsURL string,
	session adminSession,
	query string,
) rankingPromotionExplainResponse {
	t.Helper()
	var response rankingPromotionExplainResponse
	postRankingAdminJSON(
		t,
		ctx,
		opsURL+pathExplain,
		session,
		map[string]string{"query": query},
		http.StatusOK,
		&response,
	)

	return response
}

func rankingPromotionPublicSearch(
	t *testing.T,
	ctx context.Context,
	publicURL string,
	query string,
) rankingPromotionPublicResponse {
	t.Helper()
	endpoint := publicURL + pathSearchJSON + "?query=" + url.QueryEscape(query) +
		"&resource=local&maximumRecords=3"
	var response rankingPromotionPublicResponse
	getRankingJSON(t, ctx, endpoint, "", &response)

	return response
}

func postRankingAdminJSON(
	t *testing.T,
	ctx context.Context,
	endpoint string,
	session adminSession,
	payload any,
	expectedStatus int,
	destination any,
) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode request for %s: %v", endpoint, err)
	}
	response, ok := authenticatedPost(ctx, endpoint, session, string(body))
	if !ok {
		t.Fatalf("request %s failed", endpoint)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response from %s: %v", endpoint, err)
	}
	if response.StatusCode != expectedStatus {
		t.Fatalf(
			"request %s status = %d, want %d, body = %q",
			endpoint,
			response.StatusCode,
			expectedStatus,
			raw,
		)
	}
	if destination != nil {
		if err := json.Unmarshal(raw, destination); err != nil {
			t.Fatalf("decode response from %s: %v, body = %q", endpoint, err, raw)
		}
	}
}

func getRankingJSON(
	t *testing.T,
	ctx context.Context,
	endpoint string,
	cookie string,
	destination any,
) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatalf("build request for %s: %v", endpoint, err)
	}
	if cookie != "" {
		request.Header.Set("Cookie", sessionCookie+"="+cookie)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request %s: %v", endpoint, err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response from %s: %v", endpoint, err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("request %s status = %d, body = %q", endpoint, response.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		t.Fatalf("decode response from %s: %v, body = %q", endpoint, err, raw)
	}
}

func rankingModelQualityWeight(model rankingPromotionModelResponse) (float64, bool) {
	for index, feature := range model.ActiveSnapshot.Model.Features {
		if feature.Name == "quality" && index < len(model.ActiveSnapshot.Model.Weights) {
			return model.ActiveSnapshot.Model.Weights[index], true
		}
	}

	return 0, false
}

func rankingResponseTopURL(response rankingPromotionPublicResponse) (string, error) {
	if len(response.Channels) == 0 || len(response.Channels[0].Items) == 0 {
		return "", fmt.Errorf("public search returned no items")
	}

	return response.Channels[0].Items[0].Link, nil
}
