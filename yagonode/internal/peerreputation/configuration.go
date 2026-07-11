package peerreputation

import (
	"fmt"
	"math"
	"time"
)

const (
	maximumPeersLimit        = 100_000
	maximumPriorEvidence     = 1_000_000.0
	maximumEvidence          = 1_000_000_000_000.0
	minimumFusionWeight      = 0.25
	maximumFusionWeight      = 1.5
	maximumInfluenceWeight   = 1_000_000_000_000.0
	maximumPeerLabelBytes    = 512
	maximumBatchObservations = 4096
)

type Configuration struct {
	HalfLife     time.Duration `json:"half_life"`
	PriorSuccess float64       `json:"prior_success"`
	PriorFailure float64       `json:"prior_failure"`
	MaximumPeers int           `json:"maximum_peers"`
}

func DefaultConfiguration() Configuration {
	return Configuration{
		HalfLife:     7 * 24 * time.Hour,
		PriorSuccess: 2,
		PriorFailure: 2,
		MaximumPeers: 4096,
	}
}

func validateConfiguration(configuration Configuration) error {
	if configuration.HalfLife <= 0 {
		return fmt.Errorf("peer reputation half-life must be positive")
	}
	if !finitePositive(configuration.PriorSuccess) ||
		configuration.PriorSuccess > maximumPriorEvidence {
		return fmt.Errorf("peer reputation success prior is invalid")
	}
	if !finitePositive(configuration.PriorFailure) ||
		configuration.PriorFailure > maximumPriorEvidence {
		return fmt.Errorf("peer reputation failure prior is invalid")
	}
	if configuration.PriorFailure < configuration.PriorSuccess {
		return fmt.Errorf("peer reputation prior must not favor an unknown peer")
	}
	if configuration.MaximumPeers < 1 || configuration.MaximumPeers > maximumPeersLimit {
		return fmt.Errorf("peer reputation peer bound is invalid")
	}

	return nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finitePositive(value float64) bool {
	return finite(value) && value > 0
}
