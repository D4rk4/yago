package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/envconfig"
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

	renderConcurrency, err := envconfig.PositiveInt(
		getenv,
		EnvRenderConcurrency,
		DefaultRenderConcurrency,
	)
	if err != nil {
		return ServiceConfig{}, err
	}
	requestDeadline, err := envconfig.Duration(getenv, EnvRequestDeadline, DefaultRequestDeadline)
	if err != nil {
		return ServiceConfig{}, err
	}
	maxResponseBytes, err := envconfig.PositiveInt64(
		getenv,
		EnvMaxResponseBytes,
		DefaultMaxResponseBytes,
	)
	if err != nil {
		return ServiceConfig{}, err
	}

	return ServiceConfig{
		ListenAddr:        envconfig.String(getenv, EnvListenAddr, DefaultListenAddr),
		CDPURL:            cdpURL,
		RenderConcurrency: renderConcurrency,
		RequestDeadline:   requestDeadline,
		MaxResponseBytes:  maxResponseBytes,
		OpsAddr:           envconfig.String(getenv, EnvOpsAddr, DefaultOpsAddr),
	}, nil
}
