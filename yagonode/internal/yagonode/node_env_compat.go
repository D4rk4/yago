package yagonode

import (
	"context"
	"log/slog"
	"strings"
)

const (
	legacyEnvPrefix  = "YACY_"
	currentEnvPrefix = "YAGO_"
)

// withLegacyEnvAliases wraps getenv so an unset YAGO_* variable falls back to its
// deprecated YACY_* name, warning once per legacy variable that is actually read.
// It gives the yacy->yago environment rename a one-release grace period: existing
// deployments keep working while operators migrate to the YAGO_ names.
func withLegacyEnvAliases(getenv func(string) string) func(string) string {
	warned := make(map[string]struct{})

	return func(name string) string {
		if value := getenv(name); value != "" {
			return value
		}

		suffix, ok := strings.CutPrefix(name, currentEnvPrefix)
		if !ok {
			return ""
		}

		legacy := legacyEnvPrefix + suffix
		value := getenv(legacy)
		if value == "" {
			return ""
		}
		if _, seen := warned[legacy]; !seen {
			warned[legacy] = struct{}{}
			slog.WarnContext(
				context.Background(),
				"deprecated environment variable; use the YAGO_ name instead",
				slog.String("deprecated", legacy),
				slog.String("replacement", name),
			)
		}

		return value
	}
}
