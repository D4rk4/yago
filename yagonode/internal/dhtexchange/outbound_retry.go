package dhtexchange

import (
	"strconv"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	DefaultOutboundRetryBaseDelay          = time.Minute
	DefaultOutboundRetryMaxDelay           = time.Hour
	DefaultOutboundRetryJitterRatio        = 0.5
	DefaultOutboundRetryQuarantineFailures = 3
	DefaultOutboundRetryQuarantineDuration = 30 * time.Minute
)

type OutboundRetryDelayFraction func(peer yagomodel.Hash, failures int) float64

type OutboundRetryConfig struct {
	BaseDelay          time.Duration
	MaxDelay           time.Duration
	JitterRatio        float64
	QuarantineFailures int
	QuarantineDuration time.Duration
	DelayFraction      OutboundRetryDelayFraction
}

type OutboundRetryStatus string

const (
	OutboundRetryIgnored     OutboundRetryStatus = "ignored"
	OutboundRetryDelayed     OutboundRetryStatus = "delayed"
	OutboundRetryQuarantined OutboundRetryStatus = "quarantined"
	OutboundRetryCleared     OutboundRetryStatus = "cleared"
)

type OutboundRetryState struct {
	Failures        int
	RetryAfter      time.Time
	QuarantineUntil time.Time
}

type OutboundRetryDecision struct {
	Status          OutboundRetryStatus
	Peer            yagomodel.Hash
	Failures        int
	Delay           time.Duration
	RetryAfter      time.Time
	QuarantineUntil time.Time
}

type OutboundRetryPolicy struct {
	config     OutboundRetryConfig
	peerStates map[yagomodel.Hash]OutboundRetryState
}

func DefaultOutboundRetryConfig() OutboundRetryConfig {
	return OutboundRetryConfig{
		BaseDelay:          DefaultOutboundRetryBaseDelay,
		MaxDelay:           DefaultOutboundRetryMaxDelay,
		JitterRatio:        DefaultOutboundRetryJitterRatio,
		QuarantineFailures: DefaultOutboundRetryQuarantineFailures,
		QuarantineDuration: DefaultOutboundRetryQuarantineDuration,
		DelayFraction:      defaultOutboundRetryDelayFraction,
	}
}

func NewOutboundRetryPolicy(config OutboundRetryConfig) *OutboundRetryPolicy {
	return &OutboundRetryPolicy{
		config:     normalizeOutboundRetryConfig(config),
		peerStates: make(map[yagomodel.Hash]OutboundRetryState),
	}
}

func (p *OutboundRetryPolicy) Observe(
	receipt DistributionReceipt,
	at time.Time,
) OutboundRetryDecision {
	if receipt.State == DistributionSent {
		delete(p.peerStates, receipt.Peer)

		return OutboundRetryDecision{Status: OutboundRetryCleared, Peer: receipt.Peer}
	}
	if !outboundRetryFailure(receipt.State) {
		return OutboundRetryDecision{Status: OutboundRetryIgnored, Peer: receipt.Peer}
	}

	state := p.peerStates[receipt.Peer]
	state.Failures++
	delay := p.failureDelay(receipt.Peer, state.Failures)
	state.RetryAfter = at.Add(delay)
	decision := OutboundRetryDecision{
		Status:     OutboundRetryDelayed,
		Peer:       receipt.Peer,
		Failures:   state.Failures,
		Delay:      delay,
		RetryAfter: state.RetryAfter,
	}
	if state.Failures >= p.config.QuarantineFailures {
		state.QuarantineUntil = at.Add(p.config.QuarantineDuration)
		decision.Status = OutboundRetryQuarantined
		decision.QuarantineUntil = state.QuarantineUntil
	}
	p.peerStates[receipt.Peer] = state

	return decision
}

func (p *OutboundRetryPolicy) Ready(peer yagomodel.Hash, at time.Time) bool {
	state, ok := p.peerStates[peer]
	if !ok {
		return true
	}

	return !at.Before(state.RetryAfter) && !at.Before(state.QuarantineUntil)
}

func (p *OutboundRetryPolicy) PeerState(peer yagomodel.Hash) (OutboundRetryState, bool) {
	state, ok := p.peerStates[peer]

	return state, ok
}

func (p *OutboundRetryPolicy) failureDelay(peer yagomodel.Hash, failures int) time.Duration {
	delay := p.config.BaseDelay
	for attempt := 1; attempt < failures; attempt++ {
		if delay > p.config.MaxDelay/2 {
			delay = p.config.MaxDelay
			continue
		}
		delay *= 2
	}
	lower := time.Duration(float64(delay) * (1 - p.config.JitterRatio))
	spread := time.Duration(
		float64(delay-lower) *
			outboundRetryFraction(p.config.DelayFraction(peer, failures)),
	)

	return lower + spread
}

func normalizeOutboundRetryConfig(config OutboundRetryConfig) OutboundRetryConfig {
	if config.BaseDelay <= 0 {
		config.BaseDelay = DefaultOutboundRetryBaseDelay
	}
	if config.MaxDelay <= 0 {
		config.MaxDelay = DefaultOutboundRetryMaxDelay
	}
	if config.MaxDelay < config.BaseDelay {
		config.MaxDelay = config.BaseDelay
	}
	if config.JitterRatio < 0 {
		config.JitterRatio = 0
	}
	if config.JitterRatio > 1 {
		config.JitterRatio = 1
	}
	if config.QuarantineFailures <= 0 {
		config.QuarantineFailures = DefaultOutboundRetryQuarantineFailures
	}
	if config.QuarantineDuration <= 0 {
		config.QuarantineDuration = DefaultOutboundRetryQuarantineDuration
	}
	if config.DelayFraction == nil {
		config.DelayFraction = defaultOutboundRetryDelayFraction
	}

	return config
}

func outboundRetryFailure(state DistributionState) bool {
	return state == DistributionCapacityFailed ||
		state == DistributionHandoffFailed ||
		state == DistributionHandoffRejected
}

func outboundRetryFraction(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}

	return value
}

func defaultOutboundRetryDelayFraction(peer yagomodel.Hash, failures int) float64 {
	value := uint64(0)
	for _, letter := range []byte(peer.String() + strconv.Itoa(failures)) {
		value = value*131 + uint64(letter)
	}

	return float64(value%10_000) / 10_000
}
