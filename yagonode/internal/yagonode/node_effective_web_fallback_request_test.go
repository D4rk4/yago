package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchsession"
)

func TestEffectiveWebFallbackConsentFollowsOperatorPolicy(t *testing.T) {
	t.Parallel()

	ddgsMode := func(privacy webFallbackPrivacy) webFallbackConfig {
		return webFallbackConfig{Provider: webFallbackProviderDDGS, Privacy: privacy}
	}
	tests := []struct {
		name      string
		config    webFallbackConfig
		requested bool
		want      bool
	}{
		{
			name:      "disabled rejects request consent",
			config:    ddgsMode(webFallbackPrivacyDisabled),
			requested: true,
		},
		{
			name:   "enabled supplies consent",
			config: ddgsMode(webFallbackPrivacyEnabled),
			want:   true,
		},
		{
			name:   "always supplies consent",
			config: ddgsMode(webFallbackPrivacyAlways),
			want:   true,
		},
		{
			name: "legacy parallel mode supplies consent",
			config: webFallbackConfig{
				Provider: webFallbackProviderDDGS,
				Privacy:  webFallbackPrivacyEnabled,
				Trigger:  webFallbackTriggerParallel,
			},
			want: true,
		},
		{
			name:   "explicit preserves rejection",
			config: ddgsMode(webFallbackPrivacyExplicit),
		},
		{
			name:      "explicit preserves consent",
			config:    ddgsMode(webFallbackPrivacyExplicit),
			requested: true,
			want:      true,
		},
		{
			name: "unsupported provider disables consent",
			config: webFallbackConfig{
				Provider: "unsupported",
				Privacy:  webFallbackPrivacyAlways,
			},
			requested: true,
		},
		{
			name:      "unsupported mode disables consent",
			config:    ddgsMode(webFallbackPrivacy("unsupported")),
			requested: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := effectiveWebFallbackConsent(test.config, test.requested); got != test.want {
				t.Fatalf("consent = %v, want %v", got, test.want)
			}
		})
	}
}

type effectiveWebFallbackSessionSource struct {
	calls int
	err   error
}

func (s *effectiveWebFallbackSessionSource) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.calls++
	if s.err != nil {
		return searchcore.Response{}, s.err
	}

	return searchcore.Response{
		Request:      req,
		TotalResults: 3,
		Availability: searchcore.ResultAvailability{Materialized: 3, Exhausted: true},
		Results: []searchcore.Result{
			{URL: "https://one.example/"},
			{URL: "https://two.example/"},
			{URL: "https://three.example/"},
		},
	}, nil
}

func TestEffectiveWebFallbackRequestReusesCanonicalSearchSession(t *testing.T) {
	tests := []struct {
		name      string
		privacy   webFallbackPrivacy
		wantCalls int
	}{
		{name: "disabled", privacy: webFallbackPrivacyDisabled, wantCalls: 1},
		{name: "enabled", privacy: webFallbackPrivacyEnabled, wantCalls: 1},
		{name: "always", privacy: webFallbackPrivacyAlways, wantCalls: 1},
		{name: "explicit", privacy: webFallbackPrivacyExplicit, wantCalls: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := &effectiveWebFallbackSessionSource{}
			search := withEffectiveWebFallbackRequest(
				searchsession.NewStableWindow(source),
				webFallbackConfig{
					Provider: webFallbackProviderDDGS,
					Privacy:  test.privacy,
				},
			)
			request := searchcore.Request{
				Query:            "ranking parity",
				Source:           searchcore.SourceGlobal,
				ContentDomain:    searchcore.ContentDomainText,
				Verify:           searchcore.VerifyIfExist,
				Limit:            1,
				AllowWebFallback: false,
			}
			if _, err := search.Search(t.Context(), request); err != nil {
				t.Fatalf("first search: %v", err)
			}
			request.Offset = 1
			request.AllowWebFallback = true
			response, err := search.Search(t.Context(), request)
			if err != nil {
				t.Fatalf("second search: %v", err)
			}
			if source.calls != test.wantCalls {
				t.Fatalf("source calls = %d, want %d", source.calls, test.wantCalls)
			}
			if len(response.Results) != 1 || response.Results[0].URL != "https://two.example/" {
				t.Fatalf("second page = %+v", response.Results)
			}
		})
	}
}

func TestEffectiveWebFallbackRequestRetainsFailureCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("search failed")
	search := withEffectiveWebFallbackRequest(
		&effectiveWebFallbackSessionSource{err: cause},
		webFallbackConfig{Provider: webFallbackProviderDDGS},
	)
	_, err := search.Search(t.Context(), searchcore.Request{})
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want wrapped cause", err)
	}
}

func TestEffectiveWebFallbackRequestKeepsLocalScopeLocal(t *testing.T) {
	source := &effectiveWebFallbackSessionSource{}
	search := withEffectiveWebFallbackRequest(
		source,
		webFallbackConfig{
			Provider: webFallbackProviderDDGS,
			Privacy:  webFallbackPrivacyAlways,
		},
	)
	response, err := search.Search(t.Context(), searchcore.Request{
		Query: "local", Source: searchcore.SourceLocal, AllowWebFallback: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Request.AllowWebFallback {
		t.Fatal("local scope retained web fallback consent")
	}
}
