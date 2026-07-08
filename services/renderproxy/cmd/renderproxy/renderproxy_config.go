package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	EnvListenAddr        = "RENDERPROXY_LISTEN_ADDR"
	EnvCDPURL            = "RENDERPROXY_CDP_URL"
	EnvRenderConcurrency = "RENDERPROXY_RENDER_CONCURRENCY"
	EnvRequestDeadline   = "RENDERPROXY_REQUEST_DEADLINE"
	EnvMaxResponseBytes  = "RENDERPROXY_MAX_RESPONSE_BYTES"
	EnvOpsAddr           = "RENDERPROXY_OPS_ADDR"

	DefaultListenAddr        = ":8080"
	DefaultRenderConcurrency = 4
	DefaultRequestDeadline   = 30 * time.Second
	DefaultMaxResponseBytes  = 10 * 1024 * 1024
	DefaultOpsAddr           = ":9090"
)

type ServiceConfig struct {
	ListenAddr        string
	CDPURL            string
	RenderConcurrency int
	RequestDeadline   time.Duration
	MaxResponseBytes  int64
	OpsAddr           string
}

func LoadServiceConfig(getenv func(string) string) (ServiceConfig, error) {
	cdpURL := strings.TrimSpace(getenv(EnvCDPURL))
	if cdpURL == "" {
		return ServiceConfig{}, fmt.Errorf("%s: must be set", EnvCDPURL)
	}

	renderConcurrency, err := envPositiveInt(getenv, EnvRenderConcurrency, DefaultRenderConcurrency)
	if err != nil {
		return ServiceConfig{}, err
	}
	requestDeadline, err := envDuration(getenv, EnvRequestDeadline, DefaultRequestDeadline)
	if err != nil {
		return ServiceConfig{}, err
	}
	maxResponseBytes, err := envPositiveInt64(getenv, EnvMaxResponseBytes, DefaultMaxResponseBytes)
	if err != nil {
		return ServiceConfig{}, err
	}

	return ServiceConfig{
		ListenAddr:        envString(getenv, EnvListenAddr, DefaultListenAddr),
		CDPURL:            cdpURL,
		RenderConcurrency: renderConcurrency,
		RequestDeadline:   requestDeadline,
		MaxResponseBytes:  maxResponseBytes,
		OpsAddr:           envString(getenv, EnvOpsAddr, DefaultOpsAddr),
	}, nil
}

func envString(getenv func(string) string, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envPositiveInt(getenv func(string) string, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s: must be positive", key)
	}
	return value, nil
}

func envPositiveInt64(getenv func(string) string, key string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s: must be positive", key)
	}
	return value, nil
}

func envDuration(
	getenv func(string) string,
	key string,
	fallback time.Duration,
) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s: must be positive", key)
	}
	return value, nil
}
