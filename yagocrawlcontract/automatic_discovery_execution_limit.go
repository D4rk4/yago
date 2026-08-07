package yagocrawlcontract

import (
	"fmt"
	"strings"
)

const (
	MaximumAutomaticDiscoveryExecutionLimits  = 64
	MaximumAutomaticDiscoveryProfileNameBytes = 256
)

type AutomaticDiscoveryExecutionLimit struct {
	ProfileName         string
	MaximumDepth        int
	MaximumPagesPerHost int
	MaximumPagesPerRun  int
}

func validateAutomaticDiscoveryExecutionLimits(policy CrawlerRuntimePolicy) error {
	limits := policy.AutomaticDiscoveryLimits
	if len(limits) > MaximumAutomaticDiscoveryExecutionLimits {
		return fmt.Errorf(
			"automatic discovery execution limits must contain at most %d profiles",
			MaximumAutomaticDiscoveryExecutionLimits,
		)
	}
	profiles := make(map[string]struct{}, len(limits))
	for _, limit := range limits {
		if limit.ProfileName == "" ||
			len(limit.ProfileName) > MaximumAutomaticDiscoveryProfileNameBytes ||
			strings.ContainsAny(limit.ProfileName, "\r\n\x00") {
			return fmt.Errorf(
				"automatic discovery profile name must be one line between 1 and %d bytes",
				MaximumAutomaticDiscoveryProfileNameBytes,
			)
		}
		if _, duplicate := profiles[limit.ProfileName]; duplicate {
			return fmt.Errorf(
				"automatic discovery profile %q has duplicate execution limits",
				limit.ProfileName,
			)
		}
		profiles[limit.ProfileName] = struct{}{}
		if limit.MaximumDepth < 0 || limit.MaximumDepth > MaxCrawlDepth {
			return fmt.Errorf(
				"automatic discovery maximum depth must be between 0 and %d",
				MaxCrawlDepth,
			)
		}
		if limit.MaximumPagesPerHost < 1 {
			return fmt.Errorf("automatic discovery maximum pages per host must be positive")
		}
		if limit.MaximumPagesPerRun < 1 {
			return fmt.Errorf("automatic discovery maximum pages per run must be positive")
		}
	}

	return nil
}
