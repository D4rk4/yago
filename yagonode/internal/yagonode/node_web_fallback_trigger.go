package yagonode

import (
	"fmt"
	"strings"
)

type webFallbackTrigger string

const (
	webFallbackTriggerMiss     webFallbackTrigger = "miss"
	webFallbackTriggerParallel webFallbackTrigger = "parallel"
	envWebFallbackTrigger                         = "YAGO_WEB_FALLBACK_TRIGGER"
)

func loadWebFallbackTrigger(getenv func(string) string) (webFallbackTrigger, error) {
	value := strings.TrimSpace(strings.ToLower(getenv(envWebFallbackTrigger)))
	if value == "" {
		return webFallbackTriggerMiss, nil
	}

	switch webFallbackTrigger(value) {
	case webFallbackTriggerMiss, webFallbackTriggerParallel:
		return webFallbackTrigger(value), nil
	default:
		return "", fmt.Errorf("%s: unknown trigger %q", envWebFallbackTrigger, value)
	}
}

func effectiveWebFallbackTrigger(value webFallbackTrigger) webFallbackTrigger {
	if value == webFallbackTriggerParallel {
		return value
	}

	return webFallbackTriggerMiss
}
