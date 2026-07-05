// Package hostrank computes a per-host authority score from the incoming
// host-citation graph — a local approximation of YaCy Block Rank (YBR): hosts
// that are linked to by many well-linked hosts score higher. The score is a
// bounded, opt-in ranking signal derived from this node's own crawl graph;
// exchanging and aggregating rank tables across peers is deliberately left as
// future work, so a node ranks with the authority it can observe locally.
package hostrank

import "sort"

const (
	dampingFactor  = 0.85
	iterationCount = 40
)

// Table maps a host hash to its normalized authority rank in [0,1]. The most
// authoritative host scores 1; a host absent from the table scores 0, so an
// unknown host receives no ranking boost rather than a penalty.
type Table map[string]float64

// Rank returns the normalized authority of hostHash, or 0 when it is unknown or
// the table is empty.
func (t Table) Rank(hostHash string) float64 {
	return t[hostHash]
}

// Compute derives a host authority table from the incoming citation graph.
// citations[target][source] is the number of links from the source host to the
// target host. It runs damped iterative rank propagation at host granularity (a
// host-level PageRank) and normalizes the result so the top host scores 1.
// Non-positive edge counts are ignored.
func Compute(citations map[string]map[string]int) Table {
	graph := newCitationGraph(citations)
	if len(graph.hosts) == 0 {
		return Table{}
	}

	rank := make(map[string]float64, len(graph.hosts))
	initial := 1 / float64(len(graph.hosts))
	for _, host := range graph.hosts {
		rank[host] = initial
	}
	for range iterationCount {
		rank = graph.propagate(rank)
	}

	return normalize(rank)
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

func normalize(rank map[string]float64) Table {
	highest := 0.0
	for _, value := range rank {
		if value > highest {
			highest = value
		}
	}

	table := make(Table, len(rank))
	for host, value := range rank {
		table[host] = value / highest
	}

	return table
}
