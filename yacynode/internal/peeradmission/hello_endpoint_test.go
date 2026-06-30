package peeradmission

import (
	"context"
	"errors"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type stubDirectory struct {
	outcome helloOutcome
	err     error
	called  bool
}

func (d *stubDirectory) Hello(context.Context, yacymodel.Seed, int) (helloOutcome, error) {
	d.called = true

	return d.outcome, d.err
}

func newEndpoint(t testing.TB, peers helloDirectory) helloEndpoint {
	return helloEndpoint{
		identity: localPeer(),
		status:   selfStatus(t),
		peers:    peers,
	}
}

func helloRequest(network string, caller yacymodel.Seed) yacyproto.HelloRequest {
	return yacyproto.HelloRequest{
		NetworkName: network,
		Seed:        caller,
		Iam:         caller.Hash,
	}
}

func TestHelloServesSelfAndKnownSeeds(t *testing.T) {
	known := callerSeed(t, "trusted", "203.0.113.1", 8090)
	endpoint := newEndpoint(t, &stubDirectory{
		outcome: helloOutcome{CallerType: yacymodel.PeerSenior, Known: []yacymodel.Seed{known}},
	})

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "caller", "10.0.0.1", 8090)),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yacymodel.PeerSenior {
		t.Fatalf("YourType = %q, want senior", resp.YourType)
	}
	if got := len(resp.Seeds); got != 2 {
		t.Fatalf("Seeds = %d, want 2 (self + known)", got)
	}
	if resp.Seeds[0].Hash != hashFor("self") {
		t.Fatalf("first seed = %q, want self", resp.Seeds[0].Hash)
	}
}

func TestHelloOnForeignNetworkOmitsDirectory(t *testing.T) {
	directory := &stubDirectory{}
	endpoint := newEndpoint(t, directory)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("otherworld", callerSeed(t, "caller", "10.0.0.1", 8090)),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if got := len(resp.Seeds); got != 1 {
		t.Fatalf("Seeds = %d, want 1 (self only)", got)
	}
	if directory.called {
		t.Fatal("directory consulted despite foreign network")
	}
}

func TestHelloReportsDirectoryFailure(t *testing.T) {
	endpoint := newEndpoint(t, &stubDirectory{err: errors.New("directory down")})

	_, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "caller", "10.0.0.1", 8090)),
	)
	if err == nil {
		t.Fatal("Serve returned nil error, want directory failure")
	}
}
