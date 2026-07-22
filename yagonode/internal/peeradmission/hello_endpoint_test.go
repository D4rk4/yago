package peeradmission

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

type stubProbe struct {
	reachable    bool
	called       bool
	caller       yagomodel.Seed
	verifiedHost yagomodel.Optional[yagomodel.Host]
}

type cancelingProbe struct {
	cancel context.CancelFunc
}

func (p cancelingProbe) ReachableCaller(
	_ context.Context,
	caller yagomodel.Seed,
	_ yagomodel.Hash,
	_ string,
) (yagomodel.Seed, bool) {
	p.cancel()

	return caller, false
}

func (p *stubProbe) ReachableCaller(
	_ context.Context,
	caller yagomodel.Seed,
	_ yagomodel.Hash,
	_ string,
) (yagomodel.Seed, bool) {
	p.called = true
	p.caller = caller
	if verifiedHost, ok := p.verifiedHost.Get(); ok && p.reachable {
		caller = caller.WithPrimaryHost(verifiedHost)
	}

	return caller, p.reachable
}

type stubReachability struct {
	seeds        []yagomodel.Seed
	limits       []int
	ignoresLimit bool
	observed     []yagomodel.Seed
	classifying  []yagomodel.PeerType
	contextErrs  []error
}

func (s *stubReachability) FreshestPeers(_ context.Context, limit int) []yagomodel.Seed {
	s.limits = append(s.limits, limit)
	if limit <= 0 {
		return nil
	}
	if s.ignoresLimit {
		return slices.Clone(s.seeds)
	}

	return slices.Clone(s.seeds[:min(limit, len(s.seeds))])
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
		Iam:         caller.Hash.String(),
		Count:       count,
	}
}

func TestHelloClassifiesReachableAddressedCallerAsSenior(t *testing.T) {
	reachability := &stubReachability{
		seeds: []yagomodel.Seed{callerSeed(t, "trusted", "203.0.113.1", 8090)},
	}
	endpoint := newEndpoint(t, &stubProbe{reachable: true}, reachability)

	caller := callerSeed(t, "caller", "10.0.0.1", 8090)
	resp, err := endpoint.Serve(context.Background(), helloRequest("freeworld", caller, 1))
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

func TestHelloObservesCallerAtSuccessfulAlternateAddress(t *testing.T) {
	caller := callerSeed(t, "caller", "192.0.2.7", 8090)
	alternatives, err := yagomodel.ParseIP6("2001:db8::7")
	if err != nil {
		t.Fatal(err)
	}
	caller.IP6 = yagomodel.Some(alternatives)
	reachability := &stubReachability{}
	endpoint := newEndpoint(t, &stubProbe{
		reachable:    true,
		verifiedHost: yagomodel.Some(alternatives[0]),
	}, reachability)

	response, err := endpoint.Serve(
		t.Context(),
		helloRequest("freeworld", caller, 0),
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.YourType != yagomodel.PeerSenior || len(reachability.observed) != 1 {
		t.Fatalf("response/observations = %q/%v", response.YourType, reachability.observed)
	}
	address, ok := reachability.observed[0].NetworkAddress()
	if !ok || address != "[2001:db8::7]:8090" {
		t.Fatalf("observed winning address = %q, %v", address, ok)
	}
}

func TestHelloPreservesReachablePrincipalCaller(t *testing.T) {
	reachability := &stubReachability{}
	endpoint := newEndpoint(t, &stubProbe{reachable: true}, reachability)
	caller := callerSeed(t, "caller", "10.0.0.1", 8090)
	caller.PeerType = yagomodel.Some(yagomodel.PeerPrincipal)

	resp, err := endpoint.Serve(
		t.Context(),
		helloRequest("freeworld", caller, 0),
	)
	if err != nil {
		t.Fatal(err)
	}
	if resp.YourType != yagomodel.PeerPrincipal || len(reachability.observed) != 1 ||
		!slices.Equal(reachability.classifying, []yagomodel.PeerType{yagomodel.PeerPrincipal}) {
		t.Fatalf(
			"principal response/observations = %q/%v/%v",
			resp.YourType,
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

func TestHelloAcceptsIPv6OnlyAdvertisedEndpoint(t *testing.T) {
	caller := callerSeed(t, "caller", "", 8090)
	hosts, err := yagomodel.ParseIP6("2001:db8::7")
	if err != nil {
		t.Fatal(err)
	}
	caller.IP6 = yagomodel.Some(hosts)
	reachability := &stubReachability{}
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(t, probe, reachability)

	response, err := endpoint.Serve(t.Context(), helloRequest("freeworld", caller, 0))
	if err != nil {
		t.Fatal(err)
	}
	if response.YourType != yagomodel.PeerSenior || !probe.called ||
		len(reachability.observed) != 1 {
		t.Fatalf(
			"IPv6-only response/called/observations = %q/%v/%d",
			response.YourType,
			probe.called,
			len(reachability.observed),
		)
	}
	address, ok := reachability.observed[0].NetworkAddress()
	if !ok || address != "[2001:db8::7]:8090" {
		t.Fatalf("IPv6-only observed address = %q, %v", address, ok)
	}
}

func TestHelloRejectsCallerUsingSelfHash(t *testing.T) {
	reachability := &stubReachability{seeds: []yagomodel.Seed{
		callerSeed(t, "trusted", "203.0.113.1", 8090),
	}}
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(t, probe, reachability)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "self", "10.0.0.1", 8090), 1),
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
	if len(resp.Seeds) != 0 || len(reachability.limits) != 0 {
		t.Fatalf(
			"self response seeds/known reads = %d/%v, want 0/none",
			len(resp.Seeds),
			reachability.limits,
		)
	}
}

func TestHelloAcceptsDifferentHashUsingSelfEndpoint(t *testing.T) {
	reachability := &stubReachability{seeds: []yagomodel.Seed{
		callerSeed(t, "trusted", "203.0.113.1", 8090),
	}}
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(t, probe, reachability)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "caller", "203.0.113.9", 8090), 1),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerSenior {
		t.Fatalf("YourType = %q, want senior for distinct hash", resp.YourType)
	}
	if !probe.called {
		t.Fatal("probe was not consulted for distinct hash")
	}
	if len(reachability.observed) != 1 ||
		reachability.observed[0].Hash != hashFor("caller") ||
		reachability.classifying[0] != yagomodel.PeerSenior {
		t.Fatalf(
			"observed = %v/%v, want distinct senior caller",
			reachability.observed,
			reachability.classifying,
		)
	}
	if len(resp.Seeds) != 2 || len(reachability.limits) != 1 || reachability.limits[0] != 1 {
		t.Fatalf(
			"distinct response seeds/known reads = %d/%v, want 2/[1]",
			len(resp.Seeds),
			reachability.limits,
		)
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
		helloRequest("otherworld", callerSeed(t, "caller", "10.0.0.1", 8090), 1),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerVirgin || len(resp.Seeds) != 0 {
		t.Fatalf(
			"foreign-network response = %q/%d, want virgin/0 seeds",
			resp.YourType,
			len(resp.Seeds),
		)
	}
	if probe.called {
		t.Fatal("probe consulted despite foreign network")
	}
	if len(endpoint.reachability.(*stubReachability).observed) != 0 {
		t.Fatal("foreign-network caller was observed")
	}
	if len(endpoint.reachability.(*stubReachability).limits) != 0 {
		t.Fatal("foreign-network caller read known peers")
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
		helloRequest("freeworld", callerSeed(t, "unknown", "10.0.0.1", 8090), 1),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerVirgin || len(resp.Seeds) != 0 ||
		probe.called || len(reachability.observed) != 0 || len(reachability.limits) != 0 {
		t.Fatalf(
			"unauthenticated response/seeds/observation/reads = %q/%d/%v/%v/%v",
			resp.YourType,
			len(resp.Seeds),
			probe.called,
			reachability.observed,
			reachability.limits,
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
		t.Fatalf("Seeds = %d, want 3 (self + two freshest known)", got)
	}
	if resp.Seeds[1].Hash != hashFor("a") || resp.Seeds[2].Hash != hashFor("b") ||
		!slices.Equal(reachability.limits, []int{2}) {
		t.Fatalf(
			"known peers/limits = %v/%v, want freshest a,b/2",
			resp.Seeds[1:],
			reachability.limits,
		)
	}
}

func TestHelloNonpositiveCountReturnsNoKnownPeers(t *testing.T) {
	for _, count := range []int{-1, 0} {
		t.Run(fmt.Sprintf("count_%d", count), func(t *testing.T) {
			reachability := &stubReachability{seeds: []yagomodel.Seed{
				callerSeed(t, "a", "203.0.113.1", 8090),
				callerSeed(t, "b", "203.0.113.2", 8090),
			}}
			endpoint := newEndpoint(t, &stubProbe{reachable: true}, reachability)

			resp, err := endpoint.Serve(
				t.Context(),
				helloRequest("freeworld", callerSeed(t, "caller", "10.0.0.1", 8090), count),
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(resp.Seeds) != 1 || resp.Seeds[0].Hash != hashFor("self") ||
				len(reachability.limits) != 0 {
				t.Fatalf(
					"response/known reads = %v/%v, want self only/no read",
					resp.Seeds,
					reachability.limits,
				)
			}
		})
	}
}

func TestHelloCapsKnownPeerCountAtOneHundred(t *testing.T) {
	seeds := make([]yagomodel.Seed, 101)
	for index := range seeds {
		seeds[index] = callerSeed(
			t,
			fmt.Sprintf("%012d", index),
			fmt.Sprintf("203.0.113.%d", index%250+1),
			8090,
		)
	}
	reachability := &stubReachability{seeds: seeds, ignoresLimit: true}
	endpoint := newEndpoint(t, &stubProbe{reachable: true}, reachability)

	resp, err := endpoint.Serve(
		t.Context(),
		helloRequest("freeworld", callerSeed(t, "caller", "10.0.0.1", 8090), 101),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Seeds) != 101 || !slices.Equal(reachability.limits, []int{100}) {
		t.Fatalf("seed count/limits = %d/%v, want 101/100", len(resp.Seeds), reachability.limits)
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
		Iam:         hashFor("caller").String(),
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

func TestMountHelloReturnsVirginEnvelopeForMalformedSeed(t *testing.T) {
	malformed := []string{
		"",
		"seed=broken",
		url.Values{
			yagoproto.FieldSeed: {helloSeedAboveStockBoundary(t)},
		}.Encode(),
	}
	for _, query := range malformed {
		t.Run(query, func(t *testing.T) {
			mux := http.NewServeMux()
			MountHello(
				httpguard.NewWireRouter(mux, helloGate()),
				localPeer(),
				HelloExchange{
					Status:       selfStatus(t),
					Reachability: &stubReachability{},
					Client:       http.DefaultClient,
				},
			)
			request := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodGet,
				yagoproto.PathHello+"?"+query,
				nil,
			)
			request.RemoteAddr = "203.0.113.25:5000"
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
			}
			message, err := yagomodel.ParseMessage(recorder.Body.String())
			if err != nil {
				t.Fatalf("parse response: %v", err)
			}
			response, err := yagoproto.ParseHelloResponse(t.Context(), message)
			if err != nil {
				t.Fatalf("parse hello response: %v", err)
			}
			if response.YourType != yagomodel.PeerVirgin || len(response.Seeds) != 0 ||
				response.YourIP != "203.0.113.25" {
				t.Fatalf(
					"response = type:%q seeds:%d ip:%q",
					response.YourType,
					len(response.Seeds),
					response.YourIP,
				)
			}
		})
	}
}

func helloSeedAboveStockBoundary(t *testing.T) string {
	t.Helper()

	prefix := "p|{Hash=" + yagomodel.WordHash("oversized-hello").String() + ",a="
	suffix := ",b=}"
	remaining := yagoproto.HelloSeedMaximumUTF16Units + 1 - len(prefix) - len(suffix)
	first := min(8000, remaining)
	seed := prefix + strings.Repeat("a", first) +
		",b=" + strings.Repeat("b", remaining-first) + "}"
	if len(seed) != yagoproto.HelloSeedMaximumUTF16Units+1 {
		t.Fatalf("oversized seed length = %d", len(seed))
	}

	return seed
}

func TestMountHelloAcceptsOpaqueIamAndStockCountFallback(t *testing.T) {
	for _, test := range []struct {
		name      string
		count     string
		configure func(*nodeidentity.Identity, *yagoproto.HelloRequest)
	}{
		{
			name:  "malformed count on open network",
			count: "broken",
			configure: func(_ *nodeidentity.Identity, request *yagoproto.HelloRequest) {
				request.Iam = "opaque-open-identity"
			},
		},
		{
			name:  "overflow count on salted network",
			count: "2147483648",
			configure: func(identity *nodeidentity.Identity, request *yagoproto.HelloRequest) {
				identity.AuthenticationMode = yagoproto.NetworkAuthenticationSaltedMagic
				identity.AuthenticationEssentials = "shared-secret"
				request.Key = "salt"
				request.Iam = "opaque-salted-identity"
				request.MagicMD5 = yagoproto.MagicMD5(
					request.Key,
					request.Iam,
					identity.AuthenticationEssentials,
				)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			mux := http.NewServeMux()
			identity := localPeer()
			request := yagoproto.HelloRequest{
				NetworkName: "freeworld",
				Seed:        callerSeed(t, "caller", "", 0),
			}
			test.configure(&identity, &request)
			MountHello(
				httpguard.NewWireRouter(mux, helloGate()),
				identity,
				HelloExchange{
					Status:       selfStatus(t),
					Reachability: &stubReachability{},
					Client:       http.DefaultClient,
				},
			)
			form := request.Form()
			form.Set(yagoproto.FieldCount, test.count)
			httpRequest := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodGet,
				yagoproto.PathHello+"?"+form.Encode(),
				nil,
			)
			httpRequest.RemoteAddr = "203.0.113.25:5000"
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httpRequest)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
			}
			message, err := yagomodel.ParseMessage(recorder.Body.String())
			if err != nil {
				t.Fatalf("parse response: %v", err)
			}
			response, err := yagoproto.ParseHelloResponse(t.Context(), message)
			if err != nil {
				t.Fatalf("parse hello response: %v", err)
			}
			if response.YourType != yagomodel.PeerJunior || len(response.Seeds) != 1 {
				t.Fatalf(
					"response = type:%q seeds:%d, want junior/1",
					response.YourType,
					len(response.Seeds),
				)
			}
		})
	}
}

func TestMountHelloDistinguishesAbsentAndEmptyNetwork(t *testing.T) {
	for _, mode := range []yagoproto.NetworkAuthenticationMode{
		yagoproto.NetworkAuthenticationUncontrolled,
		yagoproto.NetworkAuthenticationSaltedMagic,
	} {
		for _, networkPresent := range []bool{false, true} {
			name := string(mode) + "/absent"
			if networkPresent {
				name = string(mode) + "/present-empty"
			}
			t.Run(name, func(t *testing.T) {
				response := mountedHelloNetworkResponse(t, mode, networkPresent)
				wantType := yagomodel.PeerJunior
				wantSeeds := 1
				if networkPresent {
					wantType = yagomodel.PeerVirgin
					wantSeeds = 0
				}
				if response.YourType != wantType || len(response.Seeds) != wantSeeds {
					t.Fatalf(
						"response = type:%q seeds:%d, want %q/%d",
						response.YourType,
						len(response.Seeds),
						wantType,
						wantSeeds,
					)
				}
			})
		}
	}
}

func mountedHelloNetworkResponse(
	t *testing.T,
	mode yagoproto.NetworkAuthenticationMode,
	networkPresent bool,
) yagoproto.HelloResponse {
	t.Helper()

	mux := http.NewServeMux()
	identity := localPeer()
	identity.NetworkName = ""
	identity.AuthenticationMode = mode
	identity.AuthenticationEssentials = "shared-secret"
	request := yagoproto.HelloRequest{
		NetworkNamePresent: networkPresent,
		Seed:               callerSeed(t, "caller", "", 0),
		Key:                "salt",
		Iam:                "opaque-caller",
	}
	if mode == yagoproto.NetworkAuthenticationSaltedMagic {
		request.MagicMD5 = yagoproto.MagicMD5(
			request.Key,
			request.Iam,
			identity.AuthenticationEssentials,
		)
	}
	MountHello(
		httpguard.NewWireRouter(mux, helloGate()),
		identity,
		HelloExchange{
			Status:       selfStatus(t),
			Reachability: &stubReachability{},
			Client:       http.DefaultClient,
		},
	)
	httpRequest := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathHello+"?"+request.Form().Encode(),
		nil,
	)
	httpRequest.RemoteAddr = "203.0.113.25:5000"
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httpRequest)
	message, err := yagomodel.ParseMessage(recorder.Body.String())
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	response, err := yagoproto.ParseHelloResponse(t.Context(), message)
	if err != nil {
		t.Fatalf("parse hello response: %v", err)
	}

	return response
}

func TestParseHelloRequestEnvelopePreservesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := parseHelloRequestEnvelope(ctx, yagoproto.HelloRequest{
		Seed: callerSeed(t, "caller", "203.0.113.25", 8090),
	}.Form())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}
