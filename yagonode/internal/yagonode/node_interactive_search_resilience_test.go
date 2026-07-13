package yagonode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/publicportal"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type resultWithOperationalFailure struct {
	response searchcore.Response
	err      error
}

func (s resultWithOperationalFailure) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	return s.response, s.err
}

func TestInteractiveSearchPreservesUsableOperationalResponse(t *testing.T) {
	want := errors.New("ranking unavailable")
	response, err := interactiveBudgetFixture(
		resultWithOperationalFailure{
			response: searchcore.Response{
				TotalResults: 1,
				Results: []searchcore.Result{{
					Title: "Local", URL: "https://local.example/", Source: searchcore.SourceLocal,
				}},
				PartialFailures: []searchcore.PartialFailure{{
					Source: searchcore.PartialFailureSourceRemoteYaCy,
					Reason: "peer timeout",
				}},
			},
			err: want,
		},
		time.Second,
	).Search(t.Context(), searchcore.Request{Query: "local", Limit: 10})
	if err != nil || response.Request.Query != "local" || response.TotalResults != 1 ||
		len(response.Results) != 1 || response.Results[0].URL != "https://local.example/" ||
		len(response.PartialFailures) != 2 ||
		response.PartialFailures[1] != (searchcore.PartialFailure{
			Source: interactiveSearchFailureSource,
			Reason: interactiveSearchFailed,
		}) {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestInteractiveOperationalFailureKeepsPortalAvailable(t *testing.T) {
	portal := publicportal.New(newPortalSource(interactiveBudgetFixture(
		resultWithOperationalFailure{
			response: searchcore.Response{
				TotalResults: 1,
				Results: []searchcore.Result{{
					Title: "Local result",
					URL:   "https://local.example/",
				}},
			},
			err: errors.New("projection failed"),
		},
		time.Second,
	)), false)
	recorder := httptest.NewRecorder()
	portal.ServeHTTP(recorder, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/?q=local", nil,
	))
	if recorder.Code != http.StatusOK ||
		strings.Contains(recorder.Body.String(), "Search is temporarily unavailable.") ||
		!strings.Contains(recorder.Body.String(), "Local result") {
		t.Fatalf("portal response = %d %q", recorder.Code, recorder.Body.String())
	}
}
