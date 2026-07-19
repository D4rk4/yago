// Package remotecrawl owns opt-in YaCy remote crawl delegation.
package remotecrawl

import (
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	DefaultRequestsPerMinute     = 60
	DefaultOutstandingPerPeer    = 10
	DefaultLeaseTTL              = 10 * time.Minute
	DefaultQueueCapacity         = 1000
	MaximumRequestsPerMinute     = 10000
	MaximumOutstandingPerPeer    = 100
	MaximumLeaseTTL              = 24 * time.Hour
	MaximumQueueCapacity         = 100000
	MaximumTrustedPeers          = 256
	MaximumAllowedDestinations   = 256
	MaximumReceiptMetadataBytes  = 256 << 10
	MaximumReceiptURLBytes       = 8 << 10
	MaximumRemoteCrawlBatch      = 100
	destinationValidationTimeout = 2 * time.Second
)

type Config struct {
	Enabled             bool
	TrustedPeers        []yagomodel.Hash
	AllowedDestinations []string
	RequestsPerMinute   int
	OutstandingPerPeer  int
	LeaseTTL            time.Duration
	QueueCapacity       int
	Resolver            HostResolver
	Now                 func() time.Time
}

func (c Config) normalized() (Config, error) {
	if c.RequestsPerMinute == 0 {
		c.RequestsPerMinute = DefaultRequestsPerMinute
	}
	if c.OutstandingPerPeer == 0 {
		c.OutstandingPerPeer = DefaultOutstandingPerPeer
	}
	if c.LeaseTTL == 0 {
		c.LeaseTTL = DefaultLeaseTTL
	}
	if c.QueueCapacity == 0 {
		c.QueueCapacity = DefaultQueueCapacity
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.RequestsPerMinute < 1 || c.RequestsPerMinute > MaximumRequestsPerMinute {
		return Config{}, fmt.Errorf(
			"remote crawl requests per minute must be between 1 and %d",
			MaximumRequestsPerMinute,
		)
	}
	if c.OutstandingPerPeer < 1 || c.OutstandingPerPeer > MaximumOutstandingPerPeer {
		return Config{}, fmt.Errorf(
			"remote crawl outstanding leases must be between 1 and %d",
			MaximumOutstandingPerPeer,
		)
	}
	if c.LeaseTTL < time.Second || c.LeaseTTL > MaximumLeaseTTL {
		return Config{}, fmt.Errorf(
			"remote crawl lease TTL must be between 1s and %s",
			MaximumLeaseTTL,
		)
	}
	if c.QueueCapacity < 1 || c.QueueCapacity > MaximumQueueCapacity {
		return Config{}, fmt.Errorf(
			"remote crawl queue capacity must be between 1 and %d",
			MaximumQueueCapacity,
		)
	}
	if len(c.TrustedPeers) > MaximumTrustedPeers {
		return Config{}, fmt.Errorf(
			"remote crawl trusted peers must not exceed %d",
			MaximumTrustedPeers,
		)
	}
	if len(c.AllowedDestinations) > MaximumAllowedDestinations {
		return Config{}, fmt.Errorf(
			"remote crawl destinations must not exceed %d",
			MaximumAllowedDestinations,
		)
	}
	if !c.Enabled {
		return c, nil
	}
	if len(c.TrustedPeers) == 0 {
		return Config{}, fmt.Errorf("remote crawl requires at least one trusted peer")
	}
	if len(c.AllowedDestinations) == 0 {
		return Config{}, fmt.Errorf("remote crawl requires at least one allowed destination")
	}

	return c, nil
}

func Open(
	config Config,
	storage *vault.Vault,
	receiver URLMetadataReceiver,
	observers ...Observer,
) (*Broker, error) {
	config, err := config.normalized()
	if err != nil {
		return nil, err
	}
	if !config.Enabled {
		return nil, nil
	}
	if storage == nil {
		return nil, fmt.Errorf("remote crawl storage is required")
	}
	if receiver == nil {
		return nil, fmt.Errorf("remote crawl URL metadata receiver is required")
	}
	policy, err := newDestinationPolicy(config.AllowedDestinations, config.Resolver)
	if err != nil {
		return nil, err
	}
	collections, err := registerCollections(storage)
	if err != nil {
		return nil, err
	}
	if err := reconcileQueueState(storage, collections); err != nil {
		return nil, err
	}
	trusted := make(map[yagomodel.Hash]struct{}, len(config.TrustedPeers))
	for _, peer := range config.TrustedPeers {
		parsed, err := yagomodel.ParseHash(peer.String())
		if err != nil {
			return nil, fmt.Errorf("remote crawl trusted peer: %w", err)
		}
		trusted[parsed] = struct{}{}
	}

	return &Broker{
		storage:       storage,
		orders:        collections.orders,
		urlSequences:  collections.urlSequences,
		sequence:      collections.sequence,
		requestRates:  collections.requestRates,
		leaseCounts:   collections.leaseCounts,
		leaseExpiries: collections.leaseExpiries,
		pending:       collections.pending,
		receiver:      receiver,
		policy:        policy,
		trusted:       trusted,
		config:        config,
		observers:     observers,
	}, nil
}
