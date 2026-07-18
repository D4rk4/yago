package yagonode

import "github.com/D4rk4/yago/yagonode/internal/searchremote"

func publicRemoteSearchConfig(assembly publicSearchAssembly) searchremote.Config {
	return searchremote.Config{
		Client:                 remoteSearchClient(assembly),
		NetworkName:            assembly.identity.NetworkName,
		Peers:                  searchTargetPeers{roster: assembly.roster},
		Redundancy:             assembly.dht.Redundancy,
		MinimumPeerAgeDays:     assembly.dht.MinimumPeerAgeDays,
		PartitionExponent:      assembly.dht.PartitionExponent,
		RandomTargetIndex:      assembly.dhtSearchTargetIndex,
		Weights:                remoteRankingWeights(assembly.rankingWeights),
		PreferHTTPS:            assembly.peerHTTPSPreferred,
		ExpandWord:             swarmMorphologyExpander(assembly),
		PerPeerTimeout:         assembly.remoteTimeouts.perPeer,
		OverallTimeout:         assembly.remoteTimeouts.overall,
		ReputationSnapshots:    assembly.peerReputation,
		ReputationObservations: assembly.peerObservations,
		ReputationNetworkGroup: assembly.peerNetworkGroup,
		SelfSeed:               assembly.selfSeed,
	}
}

func publicDiagnosticRemoteSearchConfig(assembly publicSearchAssembly) searchremote.Config {
	config := publicRemoteSearchConfig(assembly)
	config.ReputationObservations = nil

	return config
}
