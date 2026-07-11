package searchcore

import "sort"

type resultCluster struct {
	identity string
	members  []Result
}

func ConsolidateClusters(results []Result) []Result {
	if len(results) < 2 {
		return results
	}
	clusters := make([]resultCluster, 0, len(results))
	positions := make(map[string]int, len(results))
	for _, result := range results {
		if result.ClusterID == "" {
			clusters = append(clusters, resultCluster{members: []Result{result}})

			continue
		}
		position, found := positions[result.ClusterID]
		if !found {
			positions[result.ClusterID] = len(clusters)
			clusters = append(clusters, resultCluster{
				identity: result.ClusterID,
				members:  []Result{result},
			})

			continue
		}
		clusters[position].members = append(clusters[position].members, result)
	}
	consolidated := make([]Result, 0, len(clusters))
	for _, cluster := range clusters {
		consolidated = append(consolidated, consolidateResultCluster(cluster))
	}

	return consolidated
}

func consolidateResultCluster(cluster resultCluster) Result {
	best := cluster.members[0]
	if cluster.identity == "" || len(cluster.members) == 1 {
		return best
	}
	representativeURL := declaredRepresentativeURL(cluster.members)
	selected := best
	for _, member := range cluster.members {
		if member.URL == representativeURL {
			selected = member

			break
		}
	}
	selected.ClusterID = cluster.identity
	selected.RepresentativeURL = representativeURL
	selected.Score = best.Score
	selected.diversityRelevance = best.diversityRelevance
	selected.diversityRelevanceSet = best.diversityRelevanceSet
	selected.Evidence = aggregateClusterEvidence(cluster.members)

	return selected
}

func declaredRepresentativeURL(members []Result) string {
	candidates := make([]string, 0, len(members))
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		if member.RepresentativeURL == "" {
			continue
		}
		if _, exists := seen[member.RepresentativeURL]; exists {
			continue
		}
		seen[member.RepresentativeURL] = struct{}{}
		candidates = append(candidates, member.RepresentativeURL)
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Strings(candidates)

	return candidates[0]
}

func aggregateClusterEvidence(members []Result) RankingEvidence {
	aggregate := members[0].Evidence
	sourceCount := 0.0
	peerSupport := 0.0
	sourceKnown := false
	peerKnown := false
	for _, member := range members {
		aggregate = aggregate.Overlay(member.Evidence)
		if value, known := member.Evidence.Value(SignalSourceCount); known {
			sourceCount += value
			sourceKnown = true
		}
		if value, known := member.Evidence.Value(SignalPeerSupport); known {
			peerSupport += value
			peerKnown = true
		}
	}
	if sourceKnown {
		aggregate = aggregate.With(SignalSourceCount, sourceCount)
	}
	if peerKnown {
		aggregate = aggregate.With(SignalPeerSupport, peerSupport)
	}

	return aggregate
}
