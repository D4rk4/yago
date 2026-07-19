package yagonode

import (
	"fmt"
	"strconv"
	"time"
)

const (
	envExtractFetchEnabled  = "YAGO_EXTRACT_FETCH_ENABLED"
	envExtractFetchTimeout  = "YAGO_EXTRACT_FETCH_TIMEOUT"
	envExtractFetchMaxBytes = "YAGO_EXTRACT_FETCH_MAX_BYTES"

	defaultExtractFetchTimeout        = 10 * time.Second
	defaultExtractFetchMaxBytes int64 = 2 << 20
)

// extractFetchConfig holds the optional fetch-on-extract settings for the
// Tavily-compatible `POST /extract` endpoint. Fetching is off unless Enabled, so
// by default an uncached URL never triggers an outbound request.
type extractFetchConfig struct {
	Enabled  bool
	Timeout  time.Duration
	MaxBytes int64
}

func loadExtractFetchConfig(getenv func(string) string) (extractFetchConfig, error) {
	enabled, err := boolEnv(getenv, envExtractFetchEnabled, false)
	if err != nil {
		return extractFetchConfig{}, fmt.Errorf("%s: %w", envExtractFetchEnabled, err)
	}
	timeout, err := parseOutboundRequestTimeout(envWithDefault(
		getenv,
		envExtractFetchTimeout,
		defaultExtractFetchTimeout.String(),
	))
	if err != nil {
		return extractFetchConfig{}, fmt.Errorf("%s: %w", envExtractFetchTimeout, err)
	}
	maxBytes, err := parseExtractFetchResponseBytes(envWithDefault(
		getenv,
		envExtractFetchMaxBytes,
		strconv.FormatInt(defaultExtractFetchMaxBytes, 10),
	))
	if err != nil {
		return extractFetchConfig{}, fmt.Errorf("%s: %w", envExtractFetchMaxBytes, err)
	}

	return extractFetchConfig{
		Enabled:  enabled,
		Timeout:  timeout,
		MaxBytes: maxBytes,
	}, nil
}
