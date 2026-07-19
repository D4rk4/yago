package main

import (
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func loadCrawlerWorkerIdentityPrefix(getenv func(string) string) (string, error) {
	raw := getenv(EnvWorkerID)
	if strings.Trim(raw, " ") == "" {
		return DefaultWorkerID, nil
	}
	identity, err := yagocrawlcontract.ParseCrawlerWorkerIdentityPrefix(raw)
	if err != nil {
		return "", fmt.Errorf("%s: %w", EnvWorkerID, err)
	}

	return identity, nil
}
