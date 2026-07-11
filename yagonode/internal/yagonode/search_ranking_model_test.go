package yagonode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/rankingmodel"
)

type rankingModelCatalogFixture struct {
	status     rankingmodel.Status
	snapshot   []byte
	rolledBack bool
	err        error
}

func (fixture *rankingModelCatalogFixture) Snapshot() rankingmodel.CatalogSnapshot {
	return rankingmodel.CatalogSnapshot{
		Status: fixture.status, ActiveSnapshot: append([]byte(nil), fixture.snapshot...),
	}
}

func (fixture *rankingModelCatalogFixture) Rollback(context.Context) (bool, error) {
	return fixture.rolledBack, fixture.err
}

func TestSearchRankingModelEndpointReturnsStatusAndSnapshot(t *testing.T) {
	fixture := &rankingModelCatalogFixture{
		status: rankingmodel.Status{Current: rankingmodel.Revision{
			Active: true, Revision: "v1", Kind: learnedrank.ModelLinearLambdaRank,
		}},
		snapshot: []byte(`{"format":"snapshot"}`),
	}
	recorder := httptest.NewRecorder()
	newSearchRankingModelEndpoint(fixture).ServeHTTP(
		recorder,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathSearchRankingModel, nil),
	)
	if recorder.Code != http.StatusOK ||
		!strings.Contains(recorder.Body.String(), `"revision":"v1"`) ||
		!strings.Contains(recorder.Body.String(), `"format":"snapshot"`) ||
		recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestSearchRankingModelEndpointRejectsMethodAndUnavailableCatalog(t *testing.T) {
	method := httptest.NewRecorder()
	newSearchRankingModelEndpoint(&rankingModelCatalogFixture{}).ServeHTTP(
		method,
		httptest.NewRequestWithContext(t.Context(), http.MethodPost, pathSearchRankingModel, nil),
	)
	if method.Code != http.StatusMethodNotAllowed ||
		method.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("method response = %d, %q", method.Code, method.Header().Get("Allow"))
	}
	unavailable := httptest.NewRecorder()
	newSearchRankingModelEndpoint(nil).ServeHTTP(
		unavailable,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathSearchRankingModel, nil),
	)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable response = %d", unavailable.Code)
	}
}

func TestSearchRankingRollbackEndpointCoversOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		catalog rankingModelCatalog
		want    int
	}{
		{"method", http.MethodGet, &rankingModelCatalogFixture{}, http.StatusMethodNotAllowed},
		{"unavailable", http.MethodPost, nil, http.StatusServiceUnavailable},
		{
			"failure",
			http.MethodPost,
			&rankingModelCatalogFixture{err: errors.New("disk")},
			http.StatusInternalServerError,
		},
		{"empty", http.MethodPost, &rankingModelCatalogFixture{}, http.StatusConflict},
		{"success", http.MethodPost, &rankingModelCatalogFixture{rolledBack: true}, http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			newSearchRankingRollbackEndpoint(test.catalog).ServeHTTP(
				recorder,
				httptest.NewRequestWithContext(
					t.Context(),
					test.method,
					pathSearchRankingRollback,
					nil,
				),
			)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
			}
			if test.name == "method" && recorder.Header().Get("Allow") != http.MethodPost {
				t.Fatalf("Allow = %q", recorder.Header().Get("Allow"))
			}
		})
	}
}
