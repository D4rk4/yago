package yagonode

import (
	"context"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
)

type greetRecorder struct {
	mu    sync.Mutex
	seeds []yagomodel.Seed
}

func (g *greetRecorder) Run(context.Context) {}

func (g *greetRecorder) GreetDiscovered(_ context.Context, seed yagomodel.Seed) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seeds = append(g.seeds, seed)
}

func TestBuildLANBeaconWiresVerifiedGreet(t *testing.T) {
	recorder := &greetRecorder{}
	beacon := buildLANBeacon(
		nodeConfig{LANDiscovery: true, NetworkName: "freeworld", AdvertisePort: 8090},
		nodeidentity.Identity{Hash: yagomodel.Hash("SelfHash0001")},
		recorder,
	)
	if beacon == nil {
		t.Fatal("enabled discovery must build a beacon")
	}
	beacon.HandlePacket(t.Context(), []byte(
		`{"magic":"yago-lan-v1","network":"freeworld","hash":"PeerHash0002","port":8091}`,
	), "192.168.7.9")

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.seeds) != 1 {
		t.Fatalf("greets = %d, want 1", len(recorder.seeds))
	}
	seed := recorder.seeds[0]
	host, hostOK := seed.IP.Get()
	port, portOK := seed.Port.Get()
	if seed.Hash.String() != "PeerHash0002" || !hostOK || !portOK ||
		host.String() != "192.168.7.9" || int(port) != 8091 {
		t.Fatalf("seed = %+v", seed)
	}
}

func TestBuildLANBeaconDisabledPaths(t *testing.T) {
	recorder := &greetRecorder{}
	if buildLANBeacon(nodeConfig{LANDiscovery: false}, nodeidentity.Identity{}, recorder) != nil {
		t.Fatal("disabled toggle must yield no beacon")
	}
	if buildLANBeacon(nodeConfig{LANDiscovery: true}, nodeidentity.Identity{}, nil) != nil {
		t.Fatal("nil announcer must yield no beacon")
	}
	enabled := buildLANBeacon(
		nodeConfig{LANDiscovery: true, NetworkName: "freeworld", AdvertisePort: 8090},
		nodeidentity.Identity{Hash: yagomodel.Hash("SelfHash0001")},
		recorder,
	)
	// A malformed hash from the wire must not reach the greeter.
	enabled.HandlePacket(t.Context(), []byte(
		`{"magic":"yago-lan-v1","network":"freeworld","hash":"###","port":8091}`,
	), "192.168.7.9")
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.seeds) != 0 {
		t.Fatalf("malformed hash greeted: %+v", recorder.seeds)
	}
}
