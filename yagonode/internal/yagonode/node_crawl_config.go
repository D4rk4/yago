package yagonode

import (
	"fmt"
	"strings"
)

const envCrawlRPCAddr = "YAGO_CRAWL_RPC_ADDR"

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
		ListenAddr:  strings.TrimSpace(getenv(envCrawlRPCAddr)),
		QualityGate: qualityGate,
	}, nil
}
