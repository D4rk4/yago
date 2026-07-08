package peerannouncement

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const hashFiller = "AAAAAAAAAAAA"

func hashFor(base string) yacymodel.Hash {
	if len(base) >= yacymodel.HashLength {
		return yacymodel.Hash(base[:yacymodel.HashLength])
	}

	return yacymodel.Hash(base + hashFiller[len(base):])
}

const seedPort = 8090

func callerSeed(t testing.TB, hash, ip string) yacymodel.Seed {
	t.Helper()

	host, err := yacymodel.ParseHost(ip)
	if err != nil {
		t.Fatalf("parse host: %v", err)
	}

	return yacymodel.Seed{
		Hash: hashFor(hash),
		IP:   yacymodel.Some(host),
		Port: yacymodel.Some(yacymodel.Port(seedPort)),
	}
}
