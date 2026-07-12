package yagonode

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type webFallbackBudgetProbe struct {
	mu          sync.Mutex
	hadDeadline bool
	err         error
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

func TestWebFallbackSwarmBudgetWrapsSearchError(t *testing.T) {
	sentinel := errors.New("swarm failed")
	searcher := withWebFallbackSwarmBudget(
		&webFallbackBudgetProbe{err: sentinel},
		webFallbackConfig{
			Provider: webFallbackProviderDDGS,
			Privacy:  webFallbackPrivacyEnabled,
		},
	)
	_, err := searcher.Search(context.Background(), searchcore.Request{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("search error = %v, want wrapped sentinel", err)
	}
}

func (p *webFallbackBudgetProbe) deadline() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.hadDeadline
}

func TestWebFallbackSwarmBudgetFollowsOperatorPolicy(t *testing.T) {
	if withWebFallbackSwarmBudget(nil, webFallbackConfig{}) != nil {
		t.Fatal("nil swarm searcher changed")
	}

	for _, test := range []struct {
		name     string
		privacy  webFallbackPrivacy
		request  searchcore.Request
		budgeted bool
	}{
		{name: "disabled", privacy: webFallbackPrivacyDisabled},
		{name: "enabled", privacy: webFallbackPrivacyEnabled, budgeted: true},
		{name: "explicit without consent", privacy: webFallbackPrivacyExplicit},
		{
			name: "explicit with consent", privacy: webFallbackPrivacyExplicit,
			request: searchcore.Request{AllowWebFallback: true}, budgeted: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := &webFallbackBudgetProbe{}
			searcher := withWebFallbackSwarmBudget(probe, webFallbackConfig{
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
	searcher := withWebFallbackSwarmBudget(probe, webFallbackConfig{
		Provider: "other", Privacy: webFallbackPrivacyEnabled,
	})
	if _, err := searcher.Search(context.Background(), searchcore.Request{}); err != nil {
		t.Fatal(err)
	}
	if probe.deadline() {
		t.Fatal("unconfigured provider shortened the swarm deadline")
	}
}
