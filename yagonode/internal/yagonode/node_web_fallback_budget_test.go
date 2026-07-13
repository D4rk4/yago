package yagonode

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type webFallbackBudgetProbe struct {
	mu          sync.Mutex
	hadDeadline bool
	err         error
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

	return searchcore.Response{Request: req}, p.err
}

func TestWebFallbackExactStageBudgetWrapsSearchError(t *testing.T) {
	sentinel := errors.New("swarm failed")
	searcher := withWebFallbackExactStageBudget(
		&webFallbackBudgetProbe{err: sentinel},
		webFallbackConfig{
			Provider: webFallbackProviderDDGS,
			Privacy:  webFallbackPrivacyEnabled,
		},
	)
	_, err := searcher.Search(context.Background(), searchcore.Request{Query: "query"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("search error = %v, want wrapped sentinel", err)
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
			budgeted: true,
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
