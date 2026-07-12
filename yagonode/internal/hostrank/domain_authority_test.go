package hostrank

import (
	"context"
	"math"
	"reflect"
	"testing"
)

type authorityContext struct {
	context.Context
	remaining int
}

func (c *authorityContext) Err() error {
	if c.remaining > 0 {
		c.remaining--

		return nil
	}

	return context.Canceled
}

func TestComputeDomainAuthorityRanksDiverseCitationsWithConfidence(t *testing.T) {
	citations := []Citation{
		{
			SourceURL:  "https://one.source.example/a",
			TargetURL:  "https://docs.target.com/a",
			Confidence: 1,
		},
		{
			SourceURL:  "https://one.source.example/a",
			TargetURL:  "https://docs.target.com/a",
			Confidence: 0.5,
		},
		{
			SourceURL:  "https://one.source.example/b",
			TargetURL:  "https://docs.target.com/b",
			Confidence: 1,
		},
		{SourceURL: "https://two.example/a", TargetURL: "https://target.com/c", Confidence: 0.8},
		{SourceURL: "https://three.example/a", TargetURL: "https://target.com/d", Confidence: 0.6},
		{
			SourceURL:  "https://one.source.example/a",
			TargetURL:  "https://other.example/a",
			Confidence: 1,
		},
		{
			SourceURL:  "https://one.source.example/a",
			TargetURL:  "https://other.example/b",
			Confidence: 0.2,
		},
		{
			SourceURL:  "https://target.com/self",
			TargetURL:  "https://target.com/ignored",
			Confidence: 1,
		},
		{SourceURL: "bad", TargetURL: "https://target.com/ignored", Confidence: 1},
		{
			SourceURL:  "https://zero.example/",
			TargetURL:  "https://target.com/ignored",
			Confidence: 0,
		},
		{
			SourceURL:  "https://nan.example/",
			TargetURL:  "https://target.com/ignored",
			Confidence: math.NaN(),
		},
		{
			SourceURL:  "https://inf.example/",
			TargetURL:  "https://target.com/ignored",
			Confidence: math.Inf(1),
		},
	}
	table, err := ComputeDomainAuthority(t.Context(), citations, DomainOptions{})
	if err != nil {
		t.Fatalf("ComputeDomainAuthority: %v", err)
	}
	if table.Rank("target.com") != 1 ||
		table.Confidence("target.com") <= table.Confidence("other.example") ||
		table.Confidence("target.com") <= 0 || table.Confidence("target.com") > 1 {
		t.Fatalf("authority table = %#v", table)
	}
	reversed := append([]Citation(nil), citations...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	repeated, err := ComputeDomainAuthority(t.Context(), reversed, DomainOptions{})
	if err != nil || !reflect.DeepEqual(table, repeated) {
		t.Fatalf("repeated authority = %#v/%v", repeated, err)
	}
}

func TestComputeDomainAuthoritySupportsBoundedTrustTeleport(t *testing.T) {
	domains := []string{"a.example", "b.example"}
	uniform := domainTeleport(domains, DomainOptions{
		TrustedDomains: []string{"missing.example"}, TrustBlend: 0.5,
	})
	if uniform["a.example"] != 0.5 || uniform["b.example"] != 0.5 {
		t.Fatalf("uniform teleport = %#v", uniform)
	}
	trusted := domainTeleport(domains, DomainOptions{
		TrustedDomains: []string{"https://a.example/path", ""}, TrustBlend: 0.5,
	})
	if trusted["a.example"] != 0.75 || trusted["b.example"] != 0.25 {
		t.Fatalf("trusted teleport = %#v", trusted)
	}
	withoutSeeds := domainTeleport(domains, DomainOptions{TrustBlend: 0.5})
	if withoutSeeds["a.example"] != 0.5 {
		t.Fatalf("seedless teleport = %#v", withoutSeeds)
	}

	citations := []Citation{
		{SourceURL: "https://a.example/", TargetURL: "https://b.example/", Confidence: 1},
		{SourceURL: "https://b.example/", TargetURL: "https://a.example/", Confidence: 1},
	}
	table, err := ComputeDomainAuthority(t.Context(), citations, DomainOptions{
		TrustedDomains: []string{"a.example"}, TrustBlend: 0.5,
	})
	if err != nil || table.Rank("a.example") <= table.Rank("b.example") {
		t.Fatalf("trusted authority = %#v/%v", table, err)
	}
}

func TestComputeDomainAuthorityValidatesAndCancels(t *testing.T) {
	var missingContext context.Context
	if _, err := ComputeDomainAuthority(missingContext, nil, DomainOptions{}); err == nil {
		t.Fatal("nil context was accepted")
	}
	for _, blend := range []float64{-1, 2, math.NaN(), math.Inf(1)} {
		if _, err := ComputeDomainAuthority(t.Context(), nil, DomainOptions{
			TrustBlend: blend,
		}); err == nil {
			t.Fatalf("invalid trust blend %v was accepted", blend)
		}
	}
	tooMany := make([]string, maximumTrustedDomains+1)
	if _, err := ComputeDomainAuthority(t.Context(), nil, DomainOptions{
		TrustedDomains: tooMany,
	}); err == nil {
		t.Fatal("oversize trust seeds were accepted")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ComputeDomainAuthority(canceled, []Citation{{
		SourceURL: "https://a.example/", TargetURL: "https://b.example/", Confidence: 1,
	}}, DomainOptions{}); err == nil {
		t.Fatal("canceled graph build succeeded")
	}
	ctx := &authorityContext{Context: t.Context(), remaining: 1}
	if _, err := ComputeDomainAuthority(ctx, []Citation{{
		SourceURL: "https://a.example/", TargetURL: "https://b.example/", Confidence: 1,
	}}, DomainOptions{}); err == nil {
		t.Fatal("canceled propagation succeeded")
	}
	empty, err := ComputeDomainAuthority(t.Context(), nil, DomainOptions{})
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty authority = %#v/%v", empty, err)
	}
}

func TestWeightedDomainGraphCapsRepeatedSourcePages(t *testing.T) {
	pages := make(map[string]float64)
	for index := 0; index < maximumSourcePagesPerDomain+1; index++ {
		pages[string(rune('a'+index))] = 1
	}
	graph := weightedGraphFromVotes(map[string]map[string]map[string]float64{
		"target.example": {"source.example": pages},
	})
	want := math.Log2(1 + maximumSourcePagesPerDomain)
	if graph.incoming["target.example"]["source.example"] != want ||
		graph.confidence["target.example"] <= 0 {
		t.Fatalf("weighted graph = %#v", graph)
	}
	if maximumDomainCitations*maximumCitationRetainedBytes > maximumCitationSampleBytes {
		t.Fatal("citation retention constants exceed the byte budget")
	}
}

func TestRegistrableDomainHandlesWebHostsAndInvalidValues(t *testing.T) {
	cases := map[string]string{
		"https://www.news.example.co.uk/path": "example.co.uk",
		"http://127.0.0.1/path":               "127.0.0.1",
		"https://LOCALHOST./":                 "localhost",
		"":                                    "",
		"://bad":                              "",
		"relative/path":                       "",
	}
	for rawURL, want := range cases {
		if got := RegistrableDomain(rawURL); got != want {
			t.Errorf("RegistrableDomain(%q) = %q, want %q", rawURL, got, want)
		}
	}
	if got := domainFromValue("example.com"); got != "example.com" {
		t.Fatalf("domain value = %q", got)
	}
	if got := domainFromValue(" "); got != "" {
		t.Fatalf("blank domain value = %q", got)
	}
}
