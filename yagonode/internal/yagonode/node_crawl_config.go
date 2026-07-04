package yagonode

import "strings"

const envCrawlRPCAddr = "YACY_CRAWL_RPC_ADDR"

type crawlConfig struct {
	ListenAddr string
}

func (c crawlConfig) Enabled() bool {
	return c.ListenAddr != ""
}

func loadCrawlConfig(getenv func(string) string) crawlConfig {
	return crawlConfig{ListenAddr: strings.TrimSpace(getenv(envCrawlRPCAddr))}
}
