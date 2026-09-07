package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestWebSearchModeAppliesLiveAndRetainsUnchangedPipeline(t *testing.T) {
	toggles := newRuntimeToggles(
		nodeConfig{WebFallback: webFallbackConfig{Privacy: webFallbackPrivacyDisabled}},
	)
	var modes []webFallbackPrivacy
	search := withLiveWebSearch(
		publicSearchAssembly{toggles: toggles},
		func(config webFallbackConfig) searchcore.Searcher {
			modes = append(modes, config.Privacy)
			return staticSearcher{}
		},
	)
	definition := indexSettingDefinitions()[settingKeyWebFallbackPrivacy]
	for _, mode := range []webFallbackPrivacy{webFallbackPrivacyDisabled, webFallbackPrivacyEnabled, webFallbackPrivacyAlways, webFallbackPrivacyExplicit, webFallbackPrivacyDisabled} {
		definition.applyLive(toggles, string(mode))
		for range 2 {
			if _, err := search.Search(t.Context(), searchcore.Request{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(modes) != 5 || definition.restartRequired() {
		t.Fatalf("assemblies=%v restart=%t", modes, definition.restartRequired())
	}
	definition.applyLive(nil, string(webFallbackPrivacyDisabled))
}

func TestLiveWebSearchUsesBootstrapAndPreservesErrors(t *testing.T) {
	want := errors.New("search failed")
	search := withLiveWebSearch(
		publicSearchAssembly{
			toggles:     &runtimeToggles{},
			webFallback: webFallbackConfig{Privacy: webFallbackPrivacyEnabled},
		},
		func(config webFallbackConfig) searchcore.Searcher {
			if config.Privacy != webFallbackPrivacyEnabled {
				t.Errorf("mode=%s", config.Privacy)
			}
			return errorInteractiveSearch{err: want}
		},
	)
	if _, err := search.Search(context.Background(), searchcore.Request{}); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
}

func TestLiveWebModeChangesActualSearchEgress(t *testing.T) {
	assembly, calls := fallbackPipelineAssembly(t, &fallbackPipelineEvents{})
	assembly.webFallback.Privacy = webFallbackPrivacyDisabled
	assembly.toggles = newRuntimeToggles(nodeConfig{WebFallback: assembly.webFallback})
	search := assemblePublicSearcher(
		productionShapeLocalHit{},
		productionShapeSwarmMiss{},
		assembly,
	)
	definition := indexSettingDefinitions()[settingKeyWebFallbackPrivacy]
	for _, step := range []struct {
		mode webFallbackPrivacy
		want int32
	}{
		{webFallbackPrivacyDisabled, 0}, {webFallbackPrivacyEnabled, 1}, {webFallbackPrivacyDisabled, 1},
	} {
		definition.applyLive(assembly.toggles, string(step.mode))
		response, err := search.Search(
			t.Context(),
			searchcore.Request{Query: "gap", Source: searchcore.SourceGlobal, Limit: 10},
		)
		if err != nil || len(response.Results) == 0 || calls.Load() != step.want {
			t.Fatalf("mode=%s calls=%d error=%v", step.mode, calls.Load(), err)
		}
	}
}
