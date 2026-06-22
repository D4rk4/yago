package bootstrap

import (
	"fmt"
	"strings"
	"time"
)

const (
	EnvSeedlistURLs     = "YACY_SEEDLIST_URLS"
	EnvAnnounceInterval = "YACY_ANNOUNCE_INTERVAL"

	defaultAnnounceInterval = 10 * time.Minute
)

type BootstrapSettings struct {
	seedlistURLs     []string
	announceInterval time.Duration
}

func LoadBootstrapSettings(getenv func(string) string) (BootstrapSettings, error) {
	interval := defaultAnnounceInterval
	if raw := strings.TrimSpace(getenv(EnvAnnounceInterval)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return BootstrapSettings{}, fmt.Errorf("%s: %w", EnvAnnounceInterval, err)
		}
		if parsed <= 0 {
			return BootstrapSettings{}, fmt.Errorf("%s: must be positive", EnvAnnounceInterval)
		}
		interval = parsed
	}

	return BootstrapSettings{
		seedlistURLs:     splitList(getenv(EnvSeedlistURLs)),
		announceInterval: interval,
	}, nil
}

func (s BootstrapSettings) SeedlistURLs() []string          { return s.seedlistURLs }
func (s BootstrapSettings) AnnounceInterval() time.Duration { return s.announceInterval }

func splitList(raw string) []string {
	var out []string
	for item := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}
