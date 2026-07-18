package yagonode

import (
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type nodeNetworkBootstrap struct {
	hash             yagomodel.Hash
	peerAddress      string
	seedlistURLs     []string
	announceInterval time.Duration
	greetsPerCycle   int
	advertisement    peerAdvertisement
}

func loadNodeNetworkBootstrap(getenv func(string) string) (nodeNetworkBootstrap, error) {
	hash, err := optionalPeerHash(getenv)
	if err != nil {
		return nodeNetworkBootstrap{}, err
	}
	peerAddress := envWithDefault(getenv, envPeerAddr, defaultPeerAddr)
	seedlistURLs := splitList(getenv(envSeedlistURLs))
	announceInterval, greetsPerCycle, err := announceCadence(getenv)
	if err != nil {
		return nodeNetworkBootstrap{}, err
	}
	advertisement, err := loadPeerAdvertisement(
		getenv,
		peerAddress,
		len(seedlistURLs) > 0,
	)
	if err != nil {
		return nodeNetworkBootstrap{}, err
	}

	return nodeNetworkBootstrap{
		hash:             hash,
		peerAddress:      peerAddress,
		seedlistURLs:     seedlistURLs,
		announceInterval: announceInterval,
		greetsPerCycle:   greetsPerCycle,
		advertisement:    advertisement,
	}, nil
}
