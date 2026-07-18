package yagonode

import (
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchvisible"
	"github.com/D4rk4/yago/yagonode/internal/snippetfetch"
)

func assemblePublicRetrievalSearcher(
	local searchcore.Searcher,
	remote searchcore.Searcher,
	assembly publicSearchAssembly,
) searchcore.Searcher {
	localWithFeedback := searchcore.NewPseudoRelevanceSearcher(local)
	merged := searchcore.NewSafeSearchSearcher(
		searchcore.NewFederatedSearcher(
			localWithFeedback,
			withRemoteSearchRetention(remote),
		),
	)
	enriched := snippetfetch.WithSnippetEnrichment(
		merged,
		assembly.snippetEnricher,
		remoteTextEvidence,
	)
	admitted := withDenylistFilter(enriched, assembly.denylist)
	budgetedExact := withWebFallbackExactStageBudget(admitted, assembly.webFallback)
	localRetry := withDenylistFilter(
		searchcore.NewSafeSearchSearcher(local),
		assembly.denylist,
	)
	locallyRecovered := withLocalExactRecovery(budgetedExact, localRetry)
	recovering := withZeroResultRecovery(
		locallyRecovered,
		localRetry,
		assembly.spellCorrector,
	)
	fallback := searchcore.NewSafeSearchSearcher(withWebFallback(recovering, assembly))

	return withDenylistFilter(fallback, assembly.denylist)
}

func assembleRankingEvidenceStages(
	inner searchcore.Searcher,
	assembly publicSearchAssembly,
) searchcore.Searcher {
	return searchcore.NewLexicalEvidenceSearcherWithWeights(
		searchvisible.NewVisibleEvidenceSearcher(inner),
		lexicalRankingWeights(assembly.rankingWeights),
	)
}

func assemblePublicExplanationSearcher(
	local searchcore.Searcher,
	remote searchcore.Searcher,
	assembly publicSearchAssembly,
) searchcore.Searcher {
	assembly.seedQueue = nil

	return newPublicSearchExplanationSource(local, remote, assembly)
}
