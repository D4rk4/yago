package tavilyapi

import (
	"strings"
	"time"
)

func validateTimeout(timeout *float64, minimum, maximum float64) error {
	if timeout == nil {
		return nil
	}
	if *timeout < minimum || *timeout > maximum {
		return badRequest("timeout out of range")
	}

	return nil
}

func requestTimeout(timeout *float64, fallback time.Duration) time.Duration {
	if timeout == nil {
		return fallback
	}

	return time.Duration(*timeout * float64(time.Second))
}

func defaultExtractTimeout(depth string) time.Duration {
	if strings.EqualFold(strings.TrimSpace(depth), "advanced") {
		return 30 * time.Second
	}

	return 10 * time.Second
}
