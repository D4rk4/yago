package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	envNodeRPCAddress = "YAGO_CRAWLER_NODE_RPC_ADDR"
	envNodeAPIKey     = "YAGO_CRAWLER_NODE_API_KEY"
	envDataDirectory  = "YAGO_DATA_DIR"
	envWorkerIdentity = "YAGO_CRAWLER_WORKER_ID"
	defaultWorkerID   = "yago-crawler"
	defaultDataDir    = "./data"
)

type serviceConfiguration struct {
	nodeRPCAddress string
	nodeAPIKey     string
	dataDirectory  string
	workerID       string
}

func loadServiceConfiguration(getenv func(string) string) (serviceConfiguration, error) {
	configuration := serviceConfiguration{
		nodeRPCAddress: strings.TrimSpace(getenv(envNodeRPCAddress)),
		nodeAPIKey:     strings.TrimSpace(getenv(envNodeAPIKey)),
		dataDirectory:  strings.TrimSpace(getenv(envDataDirectory)),
		workerID:       strings.TrimSpace(getenv(envWorkerIdentity)),
	}
	if configuration.nodeRPCAddress == "" {
		return serviceConfiguration{}, fmt.Errorf("%s is required", envNodeRPCAddress)
	}
	if configuration.nodeAPIKey == "" {
		return serviceConfiguration{}, fmt.Errorf("%s is required", envNodeAPIKey)
	}
	if configuration.dataDirectory == "" {
		configuration.dataDirectory = defaultDataDir
	}
	if configuration.workerID == "" {
		configuration.workerID = defaultWorkerID
	}
	if !yagocrawlcontract.ValidCrawlerWorkerIdentity(configuration.workerID) {
		return serviceConfiguration{}, errors.New("crawler worker identity is invalid")
	}
	return configuration, nil
}
