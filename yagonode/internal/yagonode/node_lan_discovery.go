package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/landiscovery"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
)

// buildLANBeacon wires the Syncthing-style LAN discovery beacon: announcements
// carry this node's hash and peer port, and a heard announcement hands the
// packet's source address to the ordinary verified hello exchange. Disabled
// deployments get nil, which Run treats as a no-op.
func buildLANBeacon(
	config nodeConfig,
	identity nodeidentity.Identity,
	announcer peerannouncement.Announcer,
) *landiscovery.Beacon {
	if !config.LANDiscovery || announcer == nil {
		return nil
	}

	return landiscovery.New(
		config.NetworkName,
		identity.Hash.String(),
		config.AdvertisePort,
		func(ctx context.Context, host string, port int, hash string) {
			seed, ok := discoveredSeed(host, port, hash)
			if !ok {
				return
			}
			announcer.GreetDiscovered(ctx, seed)
		},
	)
}

// discoveredSeed builds the minimal seed the hello exchange needs from a
// beacon's verified-source address and claimed hash and port.
func discoveredSeed(host string, port int, hash string) (yagomodel.Seed, bool) {
	parsedHash, err := yagomodel.ParseHash(hash)
	if err != nil {
		return yagomodel.Seed{}, false
	}
	parsedHost, err := yagomodel.ParseHost(host)
	if err != nil {
		return yagomodel.Seed{}, false
	}
	seed := yagomodel.Seed{Hash: parsedHash}
	seed.IP = yagomodel.Some(parsedHost)
	seed.Port = yagomodel.Some(yagomodel.Port(port))

	return seed, true
}
