package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
)

func TestCrawlBlocksDHTDistribution(t *testing.T) {
	active := []dhtGateResultResponse{{
		Name:   string(dhtexchange.GateCrawlIdle),
		Reason: dhtexchange.GateCrawlActiveReason,
	}}
	if !crawlBlocksDHTDistribution(active) {
		t.Fatal("active crawl was not identified as the distribution blocker")
	}

	for name, results := range map[string][]dhtGateResultResponse{
		"open": {{
			Name:   string(dhtexchange.GateCrawlIdle),
			Open:   true,
			Reason: dhtexchange.GateOpenReason,
		}},
		"unavailable": {{
			Name:   string(dhtexchange.GateCrawlIdle),
			Reason: dhtexchange.GateCrawlQueueUnavailableReason,
		}},
		"other": {{
			Name:   string(dhtexchange.GateIndexIdle),
			Reason: dhtexchange.GateCrawlActiveReason,
		}},
	} {
		t.Run(name, func(t *testing.T) {
			if crawlBlocksDHTDistribution(results) {
				t.Fatal("non-active-crawl gate was identified as the blocker")
			}
		})
	}
}
