package yagonode

import (
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	envCrawlRPCAddr                 = "YAGO_CRAWL_RPC_ADDR"
	envCrawlerWorkers               = "YAGOCRAWLER_WORKERS"
	envPrioritizeAutomaticDiscovery = "YAGO_CRAWLER_PRIORITIZE_AUTOMATIC_DISCOVERY"
	defaultCrawlRPCAddr             = "127.0.0.1:9091"
	// crawlRPCDisabled is the explicit value that turns the crawl exchange off
	// for a node that runs no crawler, now that an unset value means "default".
	crawlRPCDisabled = "off"
)

type crawlConfig struct {
	ListenAddr                   string
	FetchWorkers                 int
	PrioritizeAutomaticDiscovery bool
	// QualityGate rejects crawled pages that fail the deterministic Gopher/C4
	// content-quality rules before they are stored or indexed.
	QualityGate bool
}

func (c crawlConfig) Enabled() bool {
	return c.ListenAddr != ""
}

func loadCrawlConfig(getenv func(string) string) (crawlConfig, error) {
	qualityGate, err := boolEnv(getenv, envIngestQualityGate, true)
	if err != nil {
		return crawlConfig{}, fmt.Errorf("%s: %w", envIngestQualityGate, err)
	}
	fetchWorkers, err := intRangeEnv(
		getenv,
		envCrawlerWorkers,
		yagocrawlcontract.DefaultFetchWorkerConcurrency,
		1,
		yagocrawlcontract.MaximumFetchWorkerConcurrency,
	)
	if err != nil {
		return crawlConfig{}, err
	}
	prioritizeAutomaticDiscovery, err := boolEnv(
		getenv,
		envPrioritizeAutomaticDiscovery,
		true,
	)
	if err != nil {
		return crawlConfig{}, err
	}

	return crawlConfig{
		ListenAddr:                   crawlRPCListenAddr(getenv),
		FetchWorkers:                 fetchWorkers,
		PrioritizeAutomaticDiscovery: prioritizeAutomaticDiscovery,
		QualityGate:                  qualityGate,
	}, nil
}

// crawlRPCListenAddr resolves the crawl-exchange listen address: the operator's
// value when set, the loopback default when unset (so the co-located crawler
// connects out of the box), or empty — disabled — for the explicit "off"
// sentinel.
func crawlRPCListenAddr(getenv func(string) string) string {
	addr := strings.TrimSpace(getenv(envCrawlRPCAddr))
	switch {
	case addr == "":
		return defaultCrawlRPCAddr
	case strings.EqualFold(addr, crawlRPCDisabled):
		return ""
	default:
		return addr
	}
}
