package yagonode

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	minimumWebFallbackCacheTTL = 30 * time.Second
	maximumWebFallbackCacheTTL = 168 * time.Hour
)

func parseWebFallbackPrivacy(raw string) (webFallbackPrivacy, error) {
	value := webFallbackPrivacy(strings.ToLower(strings.TrimSpace(raw)))
	switch value {
	case webFallbackPrivacyDisabled,
		webFallbackPrivacyExplicit,
		webFallbackPrivacyEnabled,
		webFallbackPrivacyAlways:
		return value, nil
	default:
		return "", fmt.Errorf("unknown privacy mode %q", value)
	}
}

func normalizeWebFallbackPrivacy(raw string) (string, error) {
	value, err := parseWebFallbackPrivacy(raw)
	if err != nil {
		return "", err
	}

	return string(value), nil
}

func validateLegacyWebFallbackProvider(raw string) error {
	if raw == "" || raw == webFallbackProviderDDGS {
		return nil
	}

	return fmt.Errorf("legacy provider must be exactly %q", webFallbackProviderDDGS)
}

func parseWebFallbackBackend(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "auto", "ddg", "brave", "mojeek", "bing":
		return value, nil
	case "duckduckgo":
		return "ddg", nil
	default:
		return "", fmt.Errorf("unknown web fallback backend %q", value)
	}
}

func normalizeWebFallbackBackend(raw string) (string, error) {
	return parseWebFallbackBackend(raw)
}

func parseWebFallbackSafeSearch(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "off", "moderate", "strict":
		return value, nil
	default:
		return "", fmt.Errorf("unknown web fallback safe-search mode %q", value)
	}
}

func normalizeWebFallbackSafeSearch(raw string) (string, error) {
	return parseWebFallbackSafeSearch(raw)
}

func parseWebFallbackMaxResults(raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < minWebFallbackResults || value > maxWebFallbackResults {
		return 0, fmt.Errorf(
			"web fallback results must be between %d and %d",
			minWebFallbackResults,
			maxWebFallbackResults,
		)
	}

	return value, nil
}

func normalizeWebFallbackMaxResults(raw string) (string, error) {
	value, err := parseWebFallbackMaxResults(raw)
	if err != nil {
		return "", err
	}

	return strconv.Itoa(value), nil
}

func parseOutboundRequestTimeout(raw string) (time.Duration, error) {
	value, err := parseDurationRange(
		raw,
		minimumInteractiveSearchTimeout,
		maximumInteractiveSearchTimeout,
	)
	if err != nil {
		return 0, fmt.Errorf("request timeout: %w", err)
	}

	return value, nil
}

func normalizeOutboundRequestTimeout(raw string) (string, error) {
	value, err := parseOutboundRequestTimeout(raw)
	if err != nil {
		return "", err
	}

	return value.String(), nil
}

func parseWebFallbackCacheTTL(raw string) (time.Duration, error) {
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || value < minimumWebFallbackCacheTTL || value > maximumWebFallbackCacheTTL {
		return 0, fmt.Errorf("web fallback cache TTL must be between 30s and 168h")
	}

	return value, nil
}

func normalizeWebFallbackCacheTTL(raw string) (string, error) {
	value, err := parseWebFallbackCacheTTL(raw)
	if err != nil {
		return "", err
	}

	return value.String(), nil
}
