package yagonode

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/remotecrawl"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func openNodeRemoteCrawlBroker(
	config nodeConfig,
	storageVault *vault.Vault,
	storage nodeStorage,
	telemetry nodeTelemetry,
) (*remotecrawl.Broker, error) {
	broker, err := remotecrawl.Open(
		config.RemoteCrawl.brokerConfig(),
		storageVault,
		storage.urlReceiver,
		telemetry.remoteCrawl,
		remoteCrawlEventObserver{recorder: telemetry.recorder},
	)
	if err != nil {
		return nil, fmt.Errorf("open remote crawl delegation: %w", err)
	}

	return broker, nil
}
