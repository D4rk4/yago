package documentsearch

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type recordingPotentialPeerObserver struct {
	observed []yagomodel.Seed
}

func (o *recordingPotentialPeerObserver) ObservePotential(
	_ context.Context,
	potential yagomodel.Seed,
) {
	o.observed = append(o.observed, potential)
}

func potentialSearchSeed(t *testing.T, hash yagomodel.Hash) yagomodel.Seed {
	t.Helper()
	port, err := yagomodel.ParsePort("8090")
	if err != nil {
		t.Fatal(err)
	}
	host, err := yagomodel.ParseHost("advertised.example")
	if err != nil {
		t.Fatal(err)
	}

	return yagomodel.Seed{
		Hash:     hash,
		IP:       yagomodel.Some(host),
		Port:     yagomodel.Some(port),
		PeerType: yagomodel.Some(yagomodel.PeerSenior),
	}
}

func TestPotentialPeerUsesAuthenticatedHashAndTransportAddress(t *testing.T) {
	hash := hashFor("peer")
	ctx := httpguard.WithRemoteAddr(t.Context(), "198.51.100.42")
	potential, ok := potentialPeerFromRequest(ctx, yagoproto.SearchRequest{
		Iam:    hash.String(),
		MySeed: yagomodel.Some(potentialSearchSeed(t, hash)),
	}, hashFor("self"))
	if !ok {
		t.Fatal("potential peer was rejected")
	}
	address, addressable := potential.NetworkAddress()
	classification, classified := potential.PeerType.Get()
	if !addressable || address != "198.51.100.42:8090" ||
		!classified || classification != yagomodel.PeerVirgin {
		t.Fatalf("potential = %#v address = %q", potential, address)
	}
	if _, retained := potential.Name.Get(); retained {
		t.Fatalf("potential retained untrusted seed fields: %#v", potential)
	}
}

func TestPotentialPeerRejectsInvalidIdentityOrAddress(t *testing.T) {
	peer := hashFor("peer")
	valid := potentialSearchSeed(t, peer)
	withoutPort := valid
	withoutPort.Port = yagomodel.None[yagomodel.Port]()
	tests := []struct {
		name   string
		remote string
		iam    string
		seed   yagomodel.Optional[yagomodel.Seed]
		self   yagomodel.Hash
	}{
		{name: "missing seed", remote: "198.51.100.1", iam: peer.String()},
		{name: "invalid iam", remote: "198.51.100.1", iam: "bad", seed: yagomodel.Some(valid)},
		{
			name:   "hash mismatch",
			remote: "198.51.100.1",
			iam:    hashFor("other").String(),
			seed:   yagomodel.Some(valid),
		},
		{
			name:   "self",
			remote: "198.51.100.1",
			iam:    peer.String(),
			seed:   yagomodel.Some(valid),
			self:   peer,
		},
		{
			name:   "missing port",
			remote: "198.51.100.1",
			iam:    peer.String(),
			seed:   yagomodel.Some(withoutPort),
		},
		{
			name:   "invalid remote",
			remote: "host.example",
			iam:    peer.String(),
			seed:   yagomodel.Some(valid),
		},
		{name: "loopback", remote: "127.0.0.1", iam: peer.String(), seed: yagomodel.Some(valid)},
		{
			name:   "link local",
			remote: "169.254.1.1",
			iam:    peer.String(),
			seed:   yagomodel.Some(valid),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := httpguard.WithRemoteAddr(t.Context(), test.remote)
			if _, ok := potentialPeerFromRequest(ctx, yagoproto.SearchRequest{
				Iam: test.iam, MySeed: test.seed,
			}, test.self); ok {
				t.Fatal("invalid potential peer was accepted")
			}
		})
	}
}

func TestEndpointObservesPotentialPeerOnlyAfterAuthentication(t *testing.T) {
	observer := &recordingPotentialPeerObserver{}
	endpoint := searchEndpoint{
		identity:       searchIdentity(),
		searcher:       searcher{index: fakeScanner{}, documents: fakeDirectory{}},
		potentialPeers: observer,
	}
	peer := hashFor("peer")
	req := yagoproto.SearchRequest{
		NetworkName: "freeworld",
		Iam:         peer.String(),
		MySeed:      yagomodel.Some(potentialSearchSeed(t, peer)),
	}
	ctx := httpguard.WithRemoteAddr(t.Context(), "198.51.100.9")
	if _, err := endpoint.Serve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(observer.observed) != 1 {
		t.Fatalf("observations = %d, want 1", len(observer.observed))
	}
	req.NetworkName = "other"
	if _, err := endpoint.Serve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(observer.observed) != 1 {
		t.Fatalf("unauthenticated observations = %d, want 1", len(observer.observed))
	}
}

func TestEndpointPotentialObservationIgnoresInvalidSeed(t *testing.T) {
	observer := &recordingPotentialPeerObserver{}
	endpoint := searchEndpoint{
		identity:       searchIdentity(),
		potentialPeers: observer,
	}
	endpoint.observePotentialPeer(t.Context(), yagoproto.SearchRequest{})
	if len(observer.observed) != 0 {
		t.Fatalf("observations = %#v", observer.observed)
	}
}
