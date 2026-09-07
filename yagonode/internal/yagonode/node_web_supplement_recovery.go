package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/spellcheck"
	"github.com/D4rk4/yago/yagonode/internal/websearch"
)

type localResultRecovery struct {
	local     searchcore.Searcher
	corrector func() *spellcheck.Corrector
}

func (r localResultRecovery) recover(
	ctx context.Context,
	request searchcore.Request,
	response searchcore.Response,
) (searchcore.Response, error) {
	exact := localExactRecoverySearcher{local: r.local}
	response, err := exact.recover(ctx, request, response)
	if err != nil {
		return response, fmt.Errorf("recover local results: %w", err)
	}
	fuzzy := recoveringSearcher{retry: r.local, corrector: r.corrector}
	return fuzzy.recover(ctx, request, response)
}

func withPublicRecovery(
	primary, local searchcore.Searcher,
	assembly publicSearchAssembly,
) searchcore.Searcher {
	if webFallbackAvailable(assembly.webFallback) {
		recovery := localResultRecovery{local: local, corrector: assembly.spellCorrector}
		return withWebFallback(primary, assembly, websearch.WithRecovery(recovery.recover))
	}
	return withZeroResultRecovery(
		withLocalExactRecovery(primary, local),
		local,
		assembly.spellCorrector,
	)
}

func webFallbackAvailable(config webFallbackConfig) bool {
	return config.Provider == webFallbackProviderDDGS &&
		config.Privacy != webFallbackPrivacyDisabled
}
