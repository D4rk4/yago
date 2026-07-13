package hostrank

import (
	"math"
	"sort"
)

const (
	dampingFactor  = 0.85
	iterationCount = 40
)

type AuthorityEvidence struct {
	Score      float64
	Confidence float64
}

type AuthorityTable map[string]AuthorityEvidence

func (t AuthorityTable) Rank(domain string) float64 {
	return t[domain].Score
}

func (t AuthorityTable) Confidence(domain string) float64 {
	return t[domain].Confidence
}

// Compute derives a host authority table from the incoming citation graph.
// citations[target][source] is the number of links from the source host to the
// target host. It runs damped iterative rank propagation at host granularity (a
// host-level PageRank) and normalizes the result so the top host scores 1.
// Non-positive edge counts are ignored.
func Compute(citations map[string]map[string]int) AuthorityTable {
	graph := newCitationGraph(citations)
	if len(graph.hosts) == 0 {
		return AuthorityTable{}
	}

	rank := make(map[string]float64, len(graph.hosts))
	initial := 1 / float64(len(graph.hosts))
	for _, host := range graph.hosts {
		rank[host] = initial
	}
	for range iterationCount {
		rank = graph.propagate(rank)
	}

	return normalize(rank, graph.incoming)
}

type citationGraph struct {
	hosts     []string
	incoming  map[string]map[string]int
	outWeight map[string]int
}

func newCitationGraph(citations map[string]map[string]int) citationGraph {
	incoming := map[string]map[string]int{}
	outWeight := map[string]int{}
	present := map[string]struct{}{}
	for target, sources := range citations {
		for source, count := range sources {
			if count <= 0 {
				continue
			}
			sourcesForTarget := incoming[target]
			if sourcesForTarget == nil {
				sourcesForTarget = map[string]int{}
				incoming[target] = sourcesForTarget
			}
			sourcesForTarget[source] += count
			outWeight[source] += count
			present[target] = struct{}{}
			present[source] = struct{}{}
		}
	}

	hosts := make([]string, 0, len(present))
	for host := range present {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)

	return citationGraph{hosts: hosts, incoming: incoming, outWeight: outWeight}
}

func (g citationGraph) propagate(rank map[string]float64) map[string]float64 {
	hostCount := float64(len(g.hosts))
	dangling := 0.0
	for _, host := range g.hosts {
		if g.outWeight[host] == 0 {
			dangling += rank[host]
		}
	}
	base := (1-dampingFactor)/hostCount + dampingFactor*dangling/hostCount

	next := make(map[string]float64, len(g.hosts))
	for _, host := range g.hosts {
		inbound := 0.0
		for source, count := range g.incoming[host] {
			inbound += rank[source] * float64(count) / float64(g.outWeight[source])
		}
		next[host] = base + dampingFactor*inbound
	}

	return next
}

func normalize(
	rank map[string]float64,
	incoming map[string]map[string]int,
) AuthorityTable {
	highest := 0.0
	for _, value := range rank {
		if value > highest {
			highest = value
		}
	}

	table := make(AuthorityTable, len(rank))
	for host, value := range rank {
		table[host] = AuthorityEvidence{
			Score:      value / highest,
			Confidence: 1 - math.Exp(-float64(len(incoming[host]))/3),
		}
	}

	return table
}
