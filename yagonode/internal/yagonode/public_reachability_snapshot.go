package yagonode

import (
	"context"
	"time"
)

type publicReachabilityState uint8

const (
	publicReachabilityUnknown publicReachabilityState = iota
	publicReachabilityUnreachable
	publicReachabilityReachable
)

type publicReachabilitySource string

const (
	publicReachabilitySourcePeerBackPing publicReachabilitySource = "peer-back-ping"
	publicReachabilitySourcePinnedProbe  publicReachabilitySource = "pinned-direct-probe"
	publicReachabilitySourceDerivedProbe publicReachabilitySource = "derived-local-probe"
	publicReachabilitySourceUnspecified  publicReachabilitySource = ""
)

type publicReachabilitySnapshot struct {
	state      publicReachabilityState
	source     publicReachabilitySource
	observedAt time.Time
}

type publicReachability interface {
	Snapshot(context.Context) publicReachabilitySnapshot
}
