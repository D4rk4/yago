package yagonode

import (
	"context"
	"fmt"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type liveWebSearch struct {
	mutex    sync.Mutex
	toggles  *runtimeToggles
	config   webFallbackConfig
	mode     webFallbackPrivacy
	current  searchcore.Searcher
	assemble func(webFallbackConfig) searchcore.Searcher
}

func withLiveWebSearch(
	assembly publicSearchAssembly,
	assemble func(webFallbackConfig) searchcore.Searcher,
) searchcore.Searcher {
	if assembly.toggles == nil {
		return assemble(assembly.webFallback)
	}
	return &liveWebSearch{
		toggles:  assembly.toggles,
		config:   assembly.webFallback,
		assemble: assemble,
	}
}

func (s *liveWebSearch) Search(
	ctx context.Context,
	request searchcore.Request,
) (searchcore.Response, error) {
	current := s.searcher()
	response, err := current.Search(ctx, request)
	if err != nil {
		return response, fmt.Errorf("live web search: %w", err)
	}
	return response, nil
}

func (s *liveWebSearch) searcher() searchcore.Searcher {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	mode := effectiveWebFallbackPrivacy(s.config)
	if value, present := s.toggles.webSearchMode.Load().(webFallbackPrivacy); present {
		mode = value
	}
	if s.current == nil || mode != s.mode {
		config := s.config
		config.Privacy = mode
		config.Trigger = webFallbackTriggerMiss
		s.current, s.mode = s.assemble(config), mode
	}
	return s.current
}
