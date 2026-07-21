package peeradmission

import (
	"context"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type stubProbe struct {
	reachable bool
	called    bool
	caller    yagomodel.Seed
}

type cancelingProbe struct {
	cancel context.CancelFunc
}

func (p cancelingProbe) Reachable(
	context.Context,
	yagomodel.Seed,
	yagomodel.Hash,
	string,
) bool {
	p.cancel()

	return false
}

func (p *stubProbe) Reachable(
	_ context.Context,
	caller yagomodel.Seed,
	_ yagomodel.Hash,
	_ string,
) bool {
	p.called = true
	p.caller = caller

	return p.reachable
}

type stubReachability struct {
	seeds       []yagomodel.Seed
	observed    []yagomodel.Seed
	classifying []yagomodel.PeerType
	contextErrs []error
}

func (s *stubReachability) ReachablePeers(context.Context) []yagomodel.Seed {
	return s.seeds
}

func (s *stubReachability) ObserveCaller(
	ctx context.Context,
	caller yagomodel.Seed,
	classification yagomodel.PeerType,
) {
	s.observed = append(s.observed, caller)
	s.classifying = append(s.classifying, classification)
	s.contextErrs = append(s.contextErrs, ctx.Err())
}

func newEndpoint(
	t testing.TB,
	probe callerReachabilityProbe,
	reachability *stubReachability,
) helloEndpoint {
	return helloEndpoint{
		identity:     localPeer(),
		status:       selfStatus(t),
		probe:        probe,
		reachability: reachability,
	}
}

func helloRequest(network string, caller yagomodel.Seed, count int) yagoproto.HelloRequest {
	return yagoproto.HelloRequest{
		NetworkName: network,
		Seed:        caller,
		Iam:         caller.Hash,
		Count:       count,
	}
}

func TestHelloClassifiesReachableAddressedCallerAsSenior(t *testing.T) {
	reachability := &stubReachability{
		seeds: []yagomodel.Seed{callerSeed(t, "trusted", "203.0.113.1", 8090)},
	}
	endpoint := newEndpoint(t, &stubProbe{reachable: true}, reachability)

	caller := callerSeed(t, "caller", "10.0.0.1", 8090)
	resp, err := endpoint.Serve(context.Background(), helloRequest("freeworld", caller, 0))
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerSenior {
		t.Fatalf("YourType = %q, want senior", resp.YourType)
	}
	if got := len(resp.Seeds); got != 2 {
		t.Fatalf("Seeds = %d, want 2 (self + trusted)", got)
	}
	if resp.Seeds[0].Hash != hashFor("self") {
		t.Fatalf("first seed = %q, want self", resp.Seeds[0].Hash)
	}
	if len(reachability.observed) != 1 || reachability.observed[0].Hash != caller.Hash ||
		!slices.Equal(reachability.classifying, []yagomodel.PeerType{yagomodel.PeerSenior}) {
		t.Fatalf(
			"observed = %v/%v, want senior caller",
			reachability.observed,
			reachability.classifying,
		)
	}
}

func TestHelloClassifiesUnreachableCallerAsJunior(t *testing.T) {
	reachability := &stubReachability{}
	endpoint := newEndpoint(t, &stubProbe{reachable: false}, reachability)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "caller", "10.0.0.1", 8090), 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerJunior {
		t.Fatalf("YourType = %q, want junior", resp.YourType)
	}
	if len(reachability.observed) != 1 ||
		reachability.classifying[0] != yagomodel.PeerJunior {
		t.Fatalf(
			"observed = %v/%v, want junior caller",
			reachability.observed,
			reachability.classifying,
		)
	}
}

func TestHelloPersistsJuniorAfterRequestCancellationDuringProbe(t *testing.T) {
	requestContext, cancel := context.WithCancel(t.Context())
	reachability := &stubReachability{}
	endpoint := newEndpoint(t, cancelingProbe{cancel: cancel}, reachability)
	resp, err := endpoint.Serve(
		requestContext,
		helloRequest("freeworld", callerSeed(t, "caller", "10.0.0.1", 8090), 0),
	)
	if err != nil {
		t.Fatal(err)
	}
	if resp.YourType != yagomodel.PeerJunior || len(reachability.observed) != 1 ||
		len(reachability.contextErrs) != 1 || reachability.contextErrs[0] != nil ||
		!errors.Is(requestContext.Err(), context.Canceled) {
		t.Fatalf(
			"canceled probe response/observations/context = %q/%d/%v/%v",
			resp.YourType,
			len(reachability.observed),
			reachability.contextErrs,
			requestContext.Err(),
		)
	}
}

func TestHelloClassifiesAddresslessCallerAsJunior(t *testing.T) {
	reachability := &stubReachability{}
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(t, probe, reachability)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "caller", "", 0), 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerJunior {
		t.Fatalf("YourType = %q, want junior for addressless caller", resp.YourType)
	}
	if probe.called {
		t.Fatal("probe consulted for addressless caller")
	}
	if len(reachability.observed) != 0 {
		t.Fatalf("observed = %v, want addressless caller rejected", reachability.observed)
	}
}

func TestHelloDerivesCallerEndpointFromTransportAddress(t *testing.T) {
	reachability := &stubReachability{}
	probe := &stubProbe{reachable: false}
	endpoint := newEndpoint(t, probe, reachability)
	caller := callerSeed(t, "caller", "", 8090)
	ctx := httpguard.WithRemoteAddr(t.Context(), "198.51.100.8")

	resp, err := endpoint.Serve(ctx, helloRequest("freeworld", caller, 0))
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerJunior || !probe.called ||
		len(reachability.observed) != 1 ||
		reachability.classifying[0] != yagomodel.PeerJunior {
		t.Fatalf(
			"derived caller response = %q called %v observations %v/%v",
			resp.YourType,
			probe.called,
			reachability.observed,
			reachability.classifying,
		)
	}
	address, addressable := reachability.observed[0].NetworkAddress()
	probeAddress, probeAddressable := probe.caller.NetworkAddress()
	if !addressable || address != "198.51.100.8:8090" || !probeAddressable ||
		probeAddress != address {
		t.Fatalf(
			"derived observed/probed addresses = %q/%v %q/%v",
			address,
			addressable,
			probeAddress,
			probeAddressable,
		)
	}
}

func TestHelloReplacesUnspecifiedAdvertisedEndpointWithTransportAddress(t *testing.T) {
	for _, advertised := range []string{"0.0.0.0", "::", "::ffff:0.0.0.0"} {
		reachability := &stubReachability{}
		probe := &stubProbe{reachable: false}
		endpoint := newEndpoint(t, probe, reachability)
		caller := callerSeed(t, "caller", advertised, 8090)
		ctx := httpguard.WithRemoteAddr(t.Context(), "198.51.100.8")

		resp, err := endpoint.Serve(ctx, helloRequest("freeworld", caller, 0))
		if err != nil {
			t.Fatal(err)
		}
		if len(reachability.observed) != 1 {
			t.Fatalf(
				"unspecified %q caller observations = %d, want 1",
				advertised,
				len(reachability.observed),
			)
		}
		address, addressable := reachability.observed[0].NetworkAddress()
		if resp.YourType != yagomodel.PeerJunior || !probe.called ||
			!addressable || address != "198.51.100.8:8090" {
			t.Fatalf(
				"unspecified %q caller response/called/address = %q/%v/%q/%v",
				advertised,
				resp.YourType,
				probe.called,
				address,
				addressable,
			)
		}
	}
}

func TestHelloRejectsUnspecifiedAdvertisedEndpointWithoutTransportAddress(t *testing.T) {
	for _, advertised := range []string{"0.0.0.0", "::", "::ffff:0.0.0.0"} {
		reachability := &stubReachability{}
		probe := &stubProbe{reachable: true}
		endpoint := newEndpoint(t, probe, reachability)
		resp, err := endpoint.Serve(
			t.Context(),
			helloRequest("freeworld", callerSeed(t, "caller", advertised, 8090), 0),
		)
		if err != nil {
			t.Fatal(err)
		}
		if resp.YourType != yagomodel.PeerJunior || probe.called ||
			len(reachability.observed) != 0 {
			t.Fatalf(
				"unspecified %q caller response/called/observed = %q/%v/%d",
				advertised,
				resp.YourType,
				probe.called,
				len(reachability.observed),
			)
		}
	}
}

func TestHelloRejectsUnusableTransportDerivedCaller(t *testing.T) {
	for _, remote := range []string{"", "not-an-ip", "0.0.0.0", "::", "::ffff:0.0.0.0"} {
		reachability := &stubReachability{}
		probe := &stubProbe{reachable: true}
		endpoint := newEndpoint(t, probe, reachability)
		ctx := httpguard.WithRemoteAddr(t.Context(), remote)

		resp, err := endpoint.Serve(
			ctx,
			helloRequest("freeworld", callerSeed(t, "caller", "", 8090), 0),
		)
		if err != nil {
			t.Fatalf("Serve(%q): %v", remote, err)
		}
		if resp.YourType != yagomodel.PeerJunior || probe.called ||
			len(reachability.observed) != 0 {
			t.Fatalf(
				"remote %q response/called/observed = %q/%v/%v",
				remote,
				resp.YourType,
				probe.called,
				reachability.observed,
			)
		}
	}
}

func TestHelloAcceptsAdvertisedHostnameEndpoint(t *testing.T) {
	reachability := &stubReachability{}
	probe := &stubProbe{reachable: false}
	endpoint := newEndpoint(t, probe, reachability)
	caller := callerSeed(t, "caller", "peer.example", 8090)
	resp, err := endpoint.Serve(t.Context(), helloRequest("freeworld", caller, 0))
	if err != nil {
		t.Fatal(err)
	}
	if resp.YourType != yagomodel.PeerJunior || !probe.called ||
		len(reachability.observed) != 1 {
		t.Fatalf(
			"hostname caller response/called/observed = %q/%v/%d",
			resp.YourType,
			probe.called,
			len(reachability.observed),
		)
	}
}

func TestHelloRejectsCallerUsingSelfHash(t *testing.T) {
	reachability := &stubReachability{}
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(t, probe, reachability)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "self", "10.0.0.1", 8090), 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerVirgin {
		t.Fatalf("YourType = %q, want virgin for self hash", resp.YourType)
	}
	if probe.called {
		t.Fatal("probe consulted for caller using self hash")
	}
	if len(reachability.observed) != 0 {
		t.Fatalf("observed = %v, want no self observation", reachability.observed)
	}
}

func TestHelloRejectsCallerUsingSelfEndpoint(t *testing.T) {
	reachability := &stubReachability{}
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(t, probe, reachability)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "caller", "203.0.113.9", 8090), 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerVirgin {
		t.Fatalf("YourType = %q, want virgin for self endpoint", resp.YourType)
	}
	if probe.called {
		t.Fatal("probe consulted for caller using self endpoint")
	}
	if len(reachability.observed) != 0 {
		t.Fatalf("observed = %v, want no self observation", reachability.observed)
	}
}

func TestHelloAcceptsSameHostOnDifferentPort(t *testing.T) {
	reachability := &stubReachability{}
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(t, probe, reachability)
	caller := callerSeed(t, "caller", "203.0.113.9", 8091)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", caller, 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerSenior {
		t.Fatalf("YourType = %q, want senior for different port", resp.YourType)
	}
	if !probe.called {
		t.Fatal("probe was not consulted for different-port caller")
	}
	if len(reachability.observed) != 1 || reachability.observed[0].Hash != caller.Hash ||
		reachability.classifying[0] != yagomodel.PeerSenior {
		t.Fatalf(
			"observed = %v/%v, want senior caller",
			reachability.observed,
			reachability.classifying,
		)
	}
}

func TestHelloOnForeignNetworkOmitsAdmission(t *testing.T) {
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(
		t,
		probe,
		&stubReachability{seeds: []yagomodel.Seed{callerSeed(t, "trusted", "203.0.113.1", 8090)}},
	)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("otherworld", callerSeed(t, "caller", "10.0.0.1", 8090), 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if got := len(resp.Seeds); got != 1 {
		t.Fatalf("Seeds = %d, want 1 (self only)", got)
	}
	if probe.called {
		t.Fatal("probe consulted despite foreign network")
	}
	if len(endpoint.reachability.(*stubReachability).observed) != 0 {
		t.Fatal("foreign-network caller was observed")
	}
}

func TestHelloDoesNotObserveUnauthenticatedCaller(t *testing.T) {
	reachability := &stubReachability{}
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(t, probe, reachability)
	endpoint.identity.AuthenticationMode = yagoproto.NetworkAuthenticationSaltedMagic
	endpoint.identity.AuthenticationEssentials = "shared-secret"

	resp, err := endpoint.Serve(
		t.Context(),
		helloRequest("freeworld", callerSeed(t, "unknown", "10.0.0.1", 8090), 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != "" || probe.called || len(reachability.observed) != 0 {
		t.Fatalf(
			"unauthenticated response/observation = %q/%v/%v",
			resp.YourType,
			probe.called,
			reachability.observed,
		)
	}
}

func TestHelloLimitsKnownPeersToCount(t *testing.T) {
	reachability := &stubReachability{seeds: []yagomodel.Seed{
		callerSeed(t, "a", "203.0.113.1", 8090),
		callerSeed(t, "b", "203.0.113.2", 8090),
		callerSeed(t, "c", "203.0.113.3", 8090),
	}}
	endpoint := newEndpoint(t, &stubProbe{reachable: true}, reachability)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "caller", "10.0.0.1", 8090), 2),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}

	if got := len(resp.Seeds); got != 3 {
		t.Fatalf("Seeds = %d, want 3 (self + two of three known)", got)
	}
	known := []yagomodel.Hash{hashFor("a"), hashFor("b"), hashFor("c")}
	for _, seed := range resp.Seeds[1:] {
		if !slices.Contains(known, seed.Hash) {
			t.Fatalf("known peer %q not from roster %v", seed.Hash, known)
		}
	}
}

func TestHelloCountZeroReturnsAllKnownPeers(t *testing.T) {
	reachability := &stubReachability{seeds: []yagomodel.Seed{
		callerSeed(t, "a", "203.0.113.1", 8090),
		callerSeed(t, "b", "203.0.113.2", 8090),
	}}
	endpoint := newEndpoint(t, &stubProbe{reachable: true}, reachability)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "caller", "10.0.0.1", 8090), 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if got := len(resp.Seeds); got != 3 {
		t.Fatalf("Seeds = %d, want 3 (self + two known)", got)
	}
}

func TestShuffleKnownPeersReturnsOnRandomError(t *testing.T) {
	saved := randomPeerIndex
	t.Cleanup(func() { randomPeerIndex = saved })
	randomPeerIndex = func(io.Reader, *big.Int) (*big.Int, error) {
		return nil, errors.New("entropy failed")
	}
	peers := []yagomodel.Seed{
		callerSeed(t, "a", "203.0.113.1", 8090),
		callerSeed(t, "b", "203.0.113.2", 8090),
	}

	shuffleKnownPeers(peers)

	if peers[0].Hash != hashFor("a") || peers[1].Hash != hashFor("b") {
		t.Fatalf("peers were shuffled despite entropy error: %#v", peers)
	}
}

type helloWireStatus struct{}

func (helloWireStatus) Version(context.Context) string { return "1.940" }
func (helloWireStatus) Uptime(context.Context) int     { return 42 }

func helloGate() httpguard.WireGate {
	return httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(4096, time.Second),
		Respond: httpguard.NewWireResponder(helloWireStatus{}),
		Address: httpguard.NewClientAddressResolver(nil),
	}
}

func TestMountHelloServesRoute(t *testing.T) {
	mux := http.NewServeMux()
	identity := localPeer()
	status := selfStatus(t)
	MountHello(
		httpguard.NewWireRouter(mux, helloGate()),
		identity,
		HelloExchange{
			Status:       status,
			Reachability: &stubReachability{},
			Client:       http.DefaultClient,
		},
	)
	req := yagoproto.HelloRequest{
		NetworkName: "otherworld",
		Seed:        callerSeed(t, "caller", "10.0.0.1", 8090),
		Iam:         hashFor("caller"),
	}

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathHello+"?"+req.Form().Encode(),
		nil,
	)
	mux.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}
