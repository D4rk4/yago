package yagonode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

func TestWebFallbackExactFailureLogUsesBoundedCause(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	ctx, _ := tracectx.StartServerSpan(
		t.Context(),
		"00-11111111111111112222222222222222-3333333333333333-01",
	)
	serverSpanID, ok := tracectx.ServerSpanIDFromContext(ctx)
	if !ok {
		t.Fatal("server span ID missing")
	}
	secret := "PRIVATE_ERROR_CANARY https://PRIVATE_URL_CANARY/"
	response, err := webFallbackExactStageResult(
		ctx,
		searchcore.Request{Query: "PRIVATE_QUERY_CANARY"},
		searchcore.Response{},
		errors.New(secret),
	)
	if err != nil || len(response.PartialFailures) != 1 {
		t.Fatalf("response/error = %#v/%v", response, err)
	}
	if !strings.Contains(output.String(), `"cause":"backend"`) ||
		!strings.Contains(output.String(), `"serverSpanId":"`+serverSpanID+`"`) {
		t.Fatalf("bounded cause missing: %s", output.String())
	}
	for _, canary := range []string{
		"PRIVATE_ERROR_CANARY",
		"PRIVATE_URL_CANARY",
		"PRIVATE_QUERY_CANARY",
		"11111111111111112222222222222222",
		"3333333333333333",
	} {
		if strings.Contains(output.String(), canary) {
			t.Fatalf("exact-stage warning exposed %q: %s", canary, output.String())
		}
	}
}

type webFallbackBudgetProbe struct {
	mu          sync.Mutex
	hadDeadline bool
	err         error
	response    searchcore.Response
}

type webFallbackDeadlineProbe struct{}

func (webFallbackDeadlineProbe) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	<-ctx.Done()

	return searchcore.Response{Request: req}, fmt.Errorf("exact work: %w", ctx.Err())
}

func (p *webFallbackBudgetProbe) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	_, deadline := ctx.Deadline()
	p.mu.Lock()
	p.hadDeadline = deadline
	p.mu.Unlock()

	response := p.response
	response.Request = req

	return response, p.err
}

func TestWebFallbackExactStageBudgetPreservesAndClassifiesSearchError(t *testing.T) {
	sentinel := errors.New("swarm failed")
	searcher := withWebFallbackExactStageBudget(
		&webFallbackBudgetProbe{
			err: sentinel,
			response: searchcore.Response{Results: []searchcore.Result{{
				URL: "https://local.example/", Source: searchcore.SourceLocal,
			}}},
		},
		webFallbackConfig{
			Provider: webFallbackProviderDDGS,
			Privacy:  webFallbackPrivacyEnabled,
		},
	)
	response, err := searcher.Search(
		context.Background(),
		searchcore.Request{Query: "query"},
	)
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].URL != "https://local.example/" ||
		len(response.PartialFailures) != 1 ||
		response.PartialFailures[0] != (searchcore.PartialFailure{
			Source: webFallbackExactStageFailureSource,
			Reason: webFallbackExactStageFailed,
			Diagnostic: searchcore.PartialFailureDiagnostic{
				Stage: searchcore.FailureStageExactSearch,
				Cause: searchcore.FailureCauseBackend,
			},
		}) {
		t.Fatalf("search response = %#v, error = %v", response, err)
	}
}

func (p *webFallbackBudgetProbe) deadline() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.hadDeadline
}

func TestWebFallbackExactStageBudgetFollowsOperatorPolicy(t *testing.T) {
	if withWebFallbackExactStageBudget(nil, webFallbackConfig{}) != nil {
		t.Fatal("nil swarm searcher changed")
	}

	for _, test := range []struct {
		name     string
		privacy  webFallbackPrivacy
		request  searchcore.Request
		budgeted bool
	}{
		{name: "disabled", privacy: webFallbackPrivacyDisabled},
		{
			name: "enabled", privacy: webFallbackPrivacyEnabled,
			request: searchcore.Request{Query: "query"}, budgeted: true,
		},
		{
			name: "enabled local scope", privacy: webFallbackPrivacyEnabled,
			request: searchcore.Request{Source: searchcore.SourceLocal},
		},
		{
			name: "enabled local fallback", privacy: webFallbackPrivacyEnabled,
			request: searchcore.Request{
				Query: "query", Source: searchcore.SourceLocal, AllowWebFallback: true,
			},
		},
		{
			name: "explicit without consent", privacy: webFallbackPrivacyExplicit,
			request: searchcore.Request{Query: "query"},
		},
		{
			name: "explicit with consent", privacy: webFallbackPrivacyExplicit,
			request: searchcore.Request{Query: "query", AllowWebFallback: true}, budgeted: true,
		},
		{
			name: "non-text", privacy: webFallbackPrivacyEnabled,
			request: searchcore.Request{Query: "query", ContentDomain: searchcore.ContentDomainImage},
		},
		{
			name: "first-seen lower bound", privacy: webFallbackPrivacyEnabled,
			request: searchcore.Request{Query: "query", MinFirstSeen: time.Unix(1, 0)},
		},
		{
			name: "first-seen upper bound", privacy: webFallbackPrivacyEnabled,
			request: searchcore.Request{Query: "query", MaxFirstSeen: time.Unix(2, 0)},
		},
		{name: "blank", privacy: webFallbackPrivacyEnabled},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := &webFallbackBudgetProbe{}
			searcher := withWebFallbackExactStageBudget(probe, webFallbackConfig{
				Provider: webFallbackProviderDDGS,
				Privacy:  test.privacy,
			})
			if _, err := searcher.Search(context.Background(), test.request); err != nil {
				t.Fatal(err)
			}
			if probe.deadline() != test.budgeted {
				t.Fatalf("deadline = %t, want %t", probe.deadline(), test.budgeted)
			}
		})
	}

	probe := &webFallbackBudgetProbe{}
	searcher := withWebFallbackExactStageBudget(probe, webFallbackConfig{
		Provider: "other", Privacy: webFallbackPrivacyEnabled,
	})
	if _, err := searcher.Search(context.Background(), searchcore.Request{}); err != nil {
		t.Fatal(err)
	}
	if probe.deadline() {
		t.Fatal("unconfigured provider shortened the swarm deadline")
	}
}

func TestWebFallbackExactStageDeadlineContinuesTheMissCascade(t *testing.T) {
	previous := webFallbackExactStageBudget
	webFallbackExactStageBudget = 10 * time.Millisecond
	t.Cleanup(func() { webFallbackExactStageBudget = previous })

	searcher := withWebFallbackExactStageBudget(
		webFallbackDeadlineProbe{},
		webFallbackConfig{
			Provider: webFallbackProviderDDGS,
			Privacy:  webFallbackPrivacyEnabled,
		},
	)
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "missing", Source: searchcore.SourceGlobal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 0 || len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Source != searchcore.PartialFailureSourceExactStage {
		t.Fatalf("response = %#v", response)
	}
}

func TestWebFallbackExactStageDeadlineKeepsQueuedCompletion(t *testing.T) {
	hardContext, cancel := context.WithCancel(t.Context())
	cancel()
	want := searchcore.Response{Results: []searchcore.Result{{
		URL: "https://local.example/", Source: searchcore.SourceLocal,
	}}}
	outcomes := make(chan webFallbackExactStageOutcome, 1)
	outcomes <- webFallbackExactStageOutcome{response: want}
	response, err := completedOrExpiredWebFallbackExactStage(
		t.Context(), hardContext, searchcore.Request{Query: "query"}, outcomes,
	)
	if err != nil || len(response.Results) != 1 || response.Results[0].URL != want.Results[0].URL {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}
