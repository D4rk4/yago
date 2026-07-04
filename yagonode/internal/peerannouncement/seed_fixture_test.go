package peerannouncement

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

const hashFiller = "AAAAAAAAAAAA"

func hashFor(base string) yagomodel.Hash {
	if len(base) >= yagomodel.HashLength {
		return yagomodel.Hash(base[:yagomodel.HashLength])
	}

	return yagomodel.Hash(base + hashFiller[len(base):])
}

const seedPort = 8090

func callerSeed(t testing.TB, hash, ip string) yagomodel.Seed {
	t.Helper()

	host, err := yagomodel.ParseHost(ip)
	if err != nil {
		t.Fatalf("parse host: %v", err)
	}

	return yagomodel.Seed{
		Hash: hashFor(hash),
		IP:   yagomodel.Some(host),
		Port: yagomodel.Some(yagomodel.Port(seedPort)),
	}
}
