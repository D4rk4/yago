package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/bootstrap"
)

const (
	envSeedlistURLs     = "YACY_SEEDLIST_URLS"
	envAnnounceInterval = "YACY_ANNOUNCE_INTERVAL"

	defaultAnnounceInterval = 10 * time.Minute
)

func loadBootstrapSettings(getenv func(string) string) (bootstrap.BootstrapSettings, error) {
	interval := defaultAnnounceInterval
	if raw := strings.TrimSpace(getenv(envAnnounceInterval)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return bootstrap.BootstrapSettings{}, fmt.Errorf("%s: %w", envAnnounceInterval, err)
		}
		if parsed <= 0 {
			return bootstrap.BootstrapSettings{}, fmt.Errorf(
				"%s: must be positive",
				envAnnounceInterval,
			)
		}
		interval = parsed
	}

	return bootstrap.BootstrapSettings{
		SeedlistURLs:     splitList(getenv(envSeedlistURLs)),
		AnnounceInterval: interval,
	}, nil
}

func splitList(raw string) []string {
	var out []string
	for item := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}
