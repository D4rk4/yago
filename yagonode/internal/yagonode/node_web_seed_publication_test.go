package yagonode

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

type webSeedPublicationQueue struct{}

func (webSeedPublicationQueue) PublishOnce(
	_ context.Context,
	identity string,
	_ yagocrawlcontract.CrawlOrder,
) (bool, error) {
	switch {
	case strings.Contains(identity, "coalesced"):
		return true, nil
	case strings.Contains(identity, "failed"):
		return false, fmt.Errorf("publication failed")
	default:
		return false, nil
	}
}

func TestWebSeedOutcomeDistinguishesPublishedCoalescedAndFailed(t *testing.T) {
	previous := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	seeder := newWebCrawlSeeder(
		webSeedPublicationQueue{},
		fakeSeedDocuments{stored: map[string]bool{"https://stored.example/": true}},
		yagomodel.Hash("node"),
		webCrawlSeedProfile{fallback: webFallbackConfig{SeedMaxPages: 1}},
	)
	seeder.Seed(t.Context(), []string{
		"https://published.example/",
		"https://coalesced.example/",
		"https://failed.example/",
		"https://stored.example/",
		"not-a-url",
	})
	logOutput := output.String()
	for _, field := range []string{
		`"urls":5`,
		`"published":1`,
		`"coalesced":1`,
		`"failed":1`,
		`"alreadyStored":1`,
		`"unusableUrl":1`,
	} {
		if !strings.Contains(logOutput, field) {
			t.Fatalf("web seed outcome log %q does not contain %s", logOutput, field)
		}
	}
}
