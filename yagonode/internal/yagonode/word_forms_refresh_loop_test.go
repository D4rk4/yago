package yagonode

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/wordforms"
)

func TestServeStartsCorpusSignalRefreshWithWordFormsEnabled(t *testing.T) {
	config := testConfig(t)
	config.SwarmMorphology = true
	assembled := assembleTestNode(t, config, openTestVault(t))
	if !assembled.swarmMorph {
		t.Fatal("swarm morphology flag not threaded to the node")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := serve(
		ctx,
		assembled,
		metrics.NewEvictionMetrics(prometheus.NewRegistry()),
		namedServer{"peer protocol", buildServer("127.0.0.1:0", assembled.peerMux)},
	); err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestSwarmMorphologyExpander(t *testing.T) {
	holder := wordforms.NewHolder()
	holder.Store(wordforms.New(
		map[string]int{"черногория": 5, "черногории": 3},
		func(word string) string { return string([]rune(word)[:6]) },
	))

	// Enabled with a provider: returns a working expansion function.
	expand := swarmMorphologyExpander(publicSearchAssembly{
		swarmMorphology: true,
		wordForms:       holder.Current,
	})
	if expand == nil {
		t.Fatal("expected an expander when swarm morphology is on")
	}
	if got := expand("черногория"); len(got) < 2 {
		t.Fatalf("expander did not expand: %v", got)
	}

	// Disabled, or wired without a provider: no expansion function.
	if swarmMorphologyExpander(publicSearchAssembly{wordForms: holder.Current}) != nil {
		t.Fatal("expander built while swarm morphology is off")
	}
	if swarmMorphologyExpander(publicSearchAssembly{swarmMorphology: true}) != nil {
		t.Fatal("expander built without a provider")
	}
}
