package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const (
	EnvNATSURL              = "NATS_URL"
	EnvExtractedTextSubject = "NATS_EXTRACTED_TEXT_SUBJECT"
	EnvExtractedTextMaxMsgs = "NATS_EXTRACTED_TEXT_MAX_MSGS"
	EnvDurable              = "YACYTEXTINDEXER_DURABLE"
	EnvConcurrency          = "YACYTEXTINDEXER_CONCURRENCY"
	EnvElasticsearchURL     = "ELASTICSEARCH_URL"
	EnvElasticsearchIndex   = "ELASTICSEARCH_INDEX"

	DefaultExtractedTextSubject = "yacy.crawl.extracted-text"
	DefaultExtractedTextMaxMsgs = 1024
	DefaultDurable              = "yacytextindexer"
	DefaultConcurrency          = 4
	DefaultElasticsearchIndex   = "yacy-text"
)

type ServiceConfig struct {
	NATSURL              string
	ExtractedTextSubject string
	ExtractedTextMaxMsgs int64
	Durable              string
	Concurrency          int
	ElasticsearchURL     string
	ElasticsearchIndex   string
}

func (c ServiceConfig) StreamSpec() yacycrawlcontract.ExtractedTextStreamSpec {
	return yacycrawlcontract.ExtractedTextStreamSpec{
		Subject: c.ExtractedTextSubject,
		MaxMsgs: c.ExtractedTextMaxMsgs,
	}
}

func LoadServiceConfig(getenv func(string) string) (ServiceConfig, error) {
	natsURL := strings.TrimSpace(getenv(EnvNATSURL))
	if natsURL == "" {
		return ServiceConfig{}, fmt.Errorf("%s: must be set", EnvNATSURL)
	}
	esURL := strings.TrimSpace(getenv(EnvElasticsearchURL))
	if esURL == "" {
		return ServiceConfig{}, fmt.Errorf("%s: must be set", EnvElasticsearchURL)
	}

	maxMsgs, err := envPositiveInt64(getenv, EnvExtractedTextMaxMsgs, DefaultExtractedTextMaxMsgs)
	if err != nil {
		return ServiceConfig{}, err
	}
	concurrency, err := envPositiveInt(getenv, EnvConcurrency, DefaultConcurrency)
	if err != nil {
		return ServiceConfig{}, err
	}

	return ServiceConfig{
		NATSURL:              natsURL,
		ExtractedTextSubject: envString(getenv, EnvExtractedTextSubject, DefaultExtractedTextSubject),
		ExtractedTextMaxMsgs: maxMsgs,
		Durable:              envString(getenv, EnvDurable, DefaultDurable),
		Concurrency:          concurrency,
		ElasticsearchURL:     esURL,
		ElasticsearchIndex:   envString(getenv, EnvElasticsearchIndex, DefaultElasticsearchIndex),
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
