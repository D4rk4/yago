package yagonode

import (
	"fmt"
	"strings"
	"time"
)

const (
	minimumInteractiveSearchTimeout = 100 * time.Millisecond
	maximumInteractiveSearchTimeout = 2 * time.Minute
	minimumAnnounceInterval         = 30 * time.Second
	maximumAnnounceInterval         = 7 * 24 * time.Hour
	minimumDHTDistributionInterval  = time.Second
)

func durationRangeEnv(
	getenv func(string) string,
	key string,
	fallback time.Duration,
	minimum time.Duration,
	maximum time.Duration,
) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := parseDurationRange(raw, minimum, maximum)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}

	return value, nil
}

func durationMinimumEnv(
	getenv func(string) string,
	key string,
	fallback time.Duration,
	minimum time.Duration,
) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := parseDurationMinimum(raw, minimum)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}

	return value, nil
}

func parseDurationRange(
	raw string,
	minimum time.Duration,
	maximum time.Duration,
) (time.Duration, error) {
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("parse duration: %w", err)
	}
	if value < minimum || value > maximum {
		return 0, fmt.Errorf("must be between %s and %s", minimum, maximum)
	}

	return value, nil
}

func parseDurationMinimum(raw string, minimum time.Duration) (time.Duration, error) {
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("parse duration: %w", err)
	}
	if value < minimum {
		return 0, fmt.Errorf("must be at least %s", minimum)
	}

	return value, nil
}

func normalizeAnnouncementInterval(raw string) (string, error) {
	value, err := parseDurationRange(raw, minimumAnnounceInterval, maximumAnnounceInterval)
	if err != nil {
		return "", err
	}

	return value.String(), nil
}

func normalizeDHTDistributionInterval(raw string) (string, error) {
	value, err := parseDurationMinimum(raw, minimumDHTDistributionInterval)
	if err != nil {
		return "", err
	}

	return value.String(), nil
}
