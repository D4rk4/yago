package peeradmission

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
)

const hashFiller = "AAAAAAAAAAAA"

func hashFor(base string) yagomodel.Hash {
	if len(base) >= yagomodel.HashLength {
		return yagomodel.Hash(base[:yagomodel.HashLength])
	}

	return yagomodel.Hash(base + hashFiller[len(base):])
}

func callerSeed(t testing.TB, hash, ip string, port int) yagomodel.Seed {
	seed := yagomodel.Seed{Hash: hashFor(hash)}
	if ip != "" {
		host, err := yagomodel.ParseHost(ip)
		if err != nil {
			t.Fatalf("parse host: %v", err)
		}
		seed.IP = yagomodel.Some(host)
	}
	if port != 0 {
		seed.Port = yagomodel.Some(yagomodel.Port(port))
	}

	return seed
}

type stubStatus struct {
	networkName string
	seed        yagomodel.Seed
}

func (s stubStatus) NetworkName(context.Context) string {
	return s.networkName
}

func (s stubStatus) SelfSeed(context.Context) yagomodel.Seed {
	return s.seed
}

func localPeer() nodeidentity.Identity {
	return nodeidentity.Identity{Hash: hashFor("self"), NetworkName: "freeworld"}
}

func selfStatus(t testing.TB) stubStatus {
	return stubStatus{
		networkName: "freeworld",
		seed:        callerSeed(t, "self", "203.0.113.9", 8090),
	}
}
