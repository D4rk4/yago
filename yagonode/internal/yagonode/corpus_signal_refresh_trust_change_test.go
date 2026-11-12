package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/hosttrust"
)

func TestCorpusSignalRefreshAfterHostTrustChange(t *testing.T) {
	t.Run("open source publishes authority and remains active", func(t *testing.T) {
		refresh, holder := corpusSignalRefreshForTrustChange()
		changes := make(chan struct{})

		if next := refresh.afterHostTrustChange(t.Context(), changes, true); next != changes {
			t.Fatal("open trust source was not retained")
		}
		if holder.Current().Rank("a.example") <= holder.Current().Rank("b.example") {
			t.Fatal("open trust source did not publish updated authority")
		}
	})

	t.Run("closed source is disabled without publishing authority", func(t *testing.T) {
		refresh, holder := corpusSignalRefreshForTrustChange()
		closedChanges := make(chan struct{})
		close(closedChanges)

		if next := refresh.afterHostTrustChange(t.Context(), closedChanges, false); next != nil {
			t.Fatal("closed trust source remained active")
		}
		if holder.Current().Rank("a.example") != holder.Current().Rank("b.example") {
			t.Fatal("closed trust source published authority")
		}
	})
}

func corpusSignalRefreshForTrustChange() (*corpusSignalRefresh, *hostrank.Holder) {
	holder := hostrank.NewHolder()

	return &corpusSignalRefresh{
		hostRank: holder,
		trust: &mutableHostTrustPolicy{
			policy: hosttrust.Policy{Blend: 0.5, Domains: []string{"a.example"}},
		},
		citations: []hostrank.Citation{
			{SourceURL: "https://a.example/", TargetURL: "https://b.example/", Confidence: 1},
			{SourceURL: "https://b.example/", TargetURL: "https://a.example/", Confidence: 1},
		},
		citationsReady: true,
	}, holder
}
