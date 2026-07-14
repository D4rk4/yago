package yagonode

import "time"

type nodeRankingRuntime struct {
	tuner   rankingTuner
	trainer *rankingModelTrainer
}

func newNodeRankingRuntime(parts nodeParts) nodeRankingRuntime {
	tuner := newRankingTuner(
		parts.storage.searchIndex,
		parts.hostRank.Current,
		parts.ranking,
		parts.judgments,
		parts.clicks,
	)
	trainer := newRankingModelTrainer(
		newRankingTrainingCandidateSource(
			parts.storage.searchIndex,
			parts.ranking.Current,
			parts.hostRank.Current,
		),
		tuner,
		parts.models,
		time.Now,
	)

	return nodeRankingRuntime{tuner: tuner, trainer: trainer}
}
