package yagonode

import (
	"fmt"
	"strings"
)

const (
	envCrawlRPCAddr = "YAGO_CRAWL_RPC_ADDR"
	// defaultCrawlRPCAddr is the loopback endpoint the co-located crawler dials
	// when YAGO_CRAWL_RPC_ADDR is unset. It matches the crawler's own default
	// (YAGOCRAWLER_NODE_RPC_ADDR=127.0.0.1:9091), so a package install that
	// enables both the node and the crawler works with no configuration. It
	// binds loopback rather than a public interface because the crawl exchange
	// is unauthenticated and the crawler is normally on the same host; a split
	// deployment (the container compose file) sets YAGO_CRAWL_RPC_ADDR=:9091.
	defaultCrawlRPCAddr = "127.0.0.1:9091"
	// crawlRPCDisabled is the explicit value that turns the crawl exchange off
	// for a node that runs no crawler, now that an unset value means "default".
	crawlRPCDisabled = "off"
)

type crawlConfig struct {
	ListenAddr string
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

	return crawlConfig{
		ListenAddr:  crawlRPCListenAddr(getenv),
		QualityGate: qualityGate,
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
