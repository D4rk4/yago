package hostrank

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/publicsuffix"
)

const (
	maximumTrustedDomains       = 256
	maximumSourcePagesPerDomain = 8
)

type Citation struct {
	SourceURL  string
	TargetURL  string
	Confidence float64
}

type DomainOptions struct {
	TrustedDomains []string
	TrustBlend     float64
}

type weightedDomainGraph struct {
	domains    []string
	incoming   map[string]map[string]float64
	outWeight  map[string]float64
	confidence map[string]float64
}

func ComputeDomainAuthority(
	ctx context.Context,
	citations []Citation,
	options DomainOptions,
) (AuthorityTable, error) {
	if ctx == nil {
		return nil, fmt.Errorf("domain authority context must not be nil")
	}
	if math.IsNaN(options.TrustBlend) || math.IsInf(options.TrustBlend, 0) ||
		options.TrustBlend < 0 || options.TrustBlend > 1 {
		return nil, fmt.Errorf("domain authority trust blend must be within zero and one")
	}
	if len(options.TrustedDomains) > maximumTrustedDomains {
		return nil, fmt.Errorf("domain authority trusted domains exceed %d", maximumTrustedDomains)
	}
	graph, err := newWeightedDomainGraph(ctx, citations)
	if err != nil {
		return nil, err
	}
	if len(graph.domains) == 0 {
		return AuthorityTable{}, nil
	}
	teleport := domainTeleport(graph.domains, options)
	rank := make(map[string]float64, len(graph.domains))
	for _, domain := range graph.domains {
		rank[domain] = teleport[domain]
	}
	for iteration := 0; iteration < iterationCount; iteration++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("compute domain authority: %w", err)
		}
		rank = graph.propagate(rank, teleport)
	}

	return normalizeDomainAuthority(rank, graph.confidence), nil
}

func newWeightedDomainGraph(
	ctx context.Context,
	citations []Citation,
) (weightedDomainGraph, error) {
	sample := NewCitationSample()
	for index, citation := range citations {
		if index%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return weightedDomainGraph{}, fmt.Errorf(
					"sample domain authority citations: %w",
					err,
				)
			}
		}
		sample.Add(citation)
	}
	ordered := sample.Citations()
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].SourceURL != ordered[right].SourceURL {
			return ordered[left].SourceURL < ordered[right].SourceURL
		}

		return ordered[left].TargetURL < ordered[right].TargetURL
	})
	votes := make(map[string]map[string]map[string]float64)
	for index, citation := range ordered {
		if index%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return weightedDomainGraph{}, fmt.Errorf("build domain authority graph: %w", err)
			}
		}
		sourceDomain := RegistrableDomain(citation.SourceURL)
		targetDomain := RegistrableDomain(citation.TargetURL)
		confidence := citation.Confidence
		sources := votes[targetDomain]
		if sources == nil {
			sources = make(map[string]map[string]float64)
			votes[targetDomain] = sources
		}
		pages := sources[sourceDomain]
		if pages == nil {
			pages = make(map[string]float64)
			sources[sourceDomain] = pages
		}
		page := strings.TrimSpace(citation.SourceURL)
		pages[page] = max(pages[page], confidence)
	}

	return weightedGraphFromVotes(votes), nil
}

func weightedGraphFromVotes(
	votes map[string]map[string]map[string]float64,
) weightedDomainGraph {
	incoming := make(map[string]map[string]float64)
	outWeight := make(map[string]float64)
	confidence := make(map[string]float64)
	present := make(map[string]struct{})
	for _, target := range sortedDomainKeys(votes) {
		sources := votes[target]
		incoming[target] = make(map[string]float64, len(sources))
		quality := 0.0
		for _, source := range sortedDomainKeys(sources) {
			pages := sources[source]
			values := make([]float64, 0, len(pages))
			for _, value := range pages {
				values = append(values, value)
			}
			sort.Slice(values, func(left, right int) bool { return values[left] > values[right] })
			if len(values) > maximumSourcePagesPerDomain {
				values = values[:maximumSourcePagesPerDomain]
			}
			sum := 0.0
			for _, value := range values {
				sum += value
			}
			weight := math.Log2(1 + sum)
			incoming[target][source] = weight
			outWeight[source] += weight
			quality += sum / float64(len(values))
			present[source] = struct{}{}
		}
		diversity := 1 - math.Exp(-float64(len(sources))/3)
		confidence[target] = diversity * quality / float64(len(sources))
		present[target] = struct{}{}
	}
	domains := make([]string, 0, len(present))
	for domain := range present {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	return weightedDomainGraph{
		domains: domains, incoming: incoming, outWeight: outWeight, confidence: confidence,
	}
}

func domainTeleport(domains []string, options DomainOptions) map[string]float64 {
	trusted := make(map[string]struct{})
	for _, value := range options.TrustedDomains {
		domain := domainFromValue(value)
		if domain != "" {
			trusted[domain] = struct{}{}
		}
	}
	blend := options.TrustBlend
	if len(trusted) == 0 {
		blend = 0
	}
	teleport := make(map[string]float64, len(domains))
	uniform := (1 - blend) / float64(len(domains))
	trustedShare := 0.0
	trustedInGraph := 0
	for _, domain := range domains {
		if _, found := trusted[domain]; found {
			trustedInGraph++
		}
	}
	if trustedInGraph > 0 {
		trustedShare = blend / float64(trustedInGraph)
	} else {
		uniform = 1 / float64(len(domains))
	}
	for _, domain := range domains {
		teleport[domain] = uniform
		if _, found := trusted[domain]; found && trustedInGraph > 0 {
			teleport[domain] += trustedShare
		}
	}

	return teleport
}

func (g weightedDomainGraph) propagate(
	rank map[string]float64,
	teleport map[string]float64,
) map[string]float64 {
	dangling := 0.0
	for _, domain := range g.domains {
		if g.outWeight[domain] == 0 {
			dangling += rank[domain]
		}
	}
	next := make(map[string]float64, len(g.domains))
	for _, domain := range g.domains {
		inbound := 0.0
		for _, source := range sortedDomainKeys(g.incoming[domain]) {
			weight := g.incoming[domain][source]
			inbound += rank[source] * weight / g.outWeight[source]
		}
		next[domain] = (1-dampingFactor)*teleport[domain] +
			dampingFactor*dangling*teleport[domain] + dampingFactor*inbound
	}

	return next
}

func sortedDomainKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys
}

func normalizeDomainAuthority(
	rank map[string]float64,
	confidence map[string]float64,
) AuthorityTable {
	highest := 0.0
	for _, value := range rank {
		highest = max(highest, value)
	}
	table := make(AuthorityTable, len(rank))
	for domain, value := range rank {
		table[domain] = AuthorityEvidence{
			Score:      value / highest,
			Confidence: min(1, max(0, confidence[domain])),
		}
	}

	return table
}

func RegistrableDomain(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" {
		return ""
	}
	if net.ParseIP(host) != nil {
		return strings.Clone(host)
	}
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return strings.Clone(host)
	}

	return strings.Clone(domain)
}

func domainFromValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}

	return RegistrableDomain(value)
}
