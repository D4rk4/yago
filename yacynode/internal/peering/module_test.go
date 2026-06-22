package peering

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const hashFiller = "AAAAAAAAAAAA"

func hashFor(base string) yacymodel.Hash {
	if len(base) >= yacymodel.HashLength {
		return yacymodel.Hash(base[:yacymodel.HashLength])
	}

	return yacymodel.Hash(base + hashFiller[len(base):])
}

func callerSeed(t testing.TB, hash, ip string, port int) yacymodel.Seed {
	seed := yacymodel.Seed{Hash: hashFor(hash)}
	if ip != "" {
		host, err := yacymodel.ParseHost(ip)
		if err != nil {
			t.Fatalf("parse host: %v", err)
		}
		seed.IP = yacymodel.Some(host)
	}
	if port != 0 {
		seed.Port = yacymodel.Some(yacymodel.Port(port))
	}

	return seed
}

type stubProbe struct {
	reachable bool
}

func (p stubProbe) Reachable(context.Context, yacymodel.Seed, yacymodel.Hash, string) bool {
	return p.reachable
}

func noShuffle(int, func(i, j int)) {}

func reverseShuffle(n int, swap func(i, j int)) {
	for i := 0; i < n/2; i++ {
		swap(i, n-1-i)
	}
}

type stubStatus struct {
	networkName string
	seed        yacymodel.Seed
}

func (s stubStatus) NetworkName(context.Context) string {
	return s.networkName
}

func (s stubStatus) SelfSeed(context.Context) yacymodel.Seed {
	return s.seed
}

type headerStatus struct{}

func (headerStatus) Version(context.Context) string { return "1.0" }

func (headerStatus) Uptime(context.Context) int { return 5 }

func newResponder() httpguard.WireResponder {
	return httpguard.NewWireResponder(headerStatus{})
}

type stubDirectory struct {
	outcome HelloOutcome
	err     error
	called  bool
}

func (d *stubDirectory) Hello(context.Context, yacymodel.Seed, int) (HelloOutcome, error) {
	d.called = true

	return d.outcome, d.err
}

func newGuard() httpguard.RequestGuard {
	ident := httpguard.LocalPeer{Hash: hashFor("self"), NetworkName: "freeworld"}

	return httpguard.NewRequestGuard(ident, httpguard.DefaultMaxBodyBytes, time.Second)
}

func selfStatus(t testing.TB) stubStatus {
	return stubStatus{
		networkName: "freeworld",
		seed:        callerSeed(t, "self", "203.0.113.9", 8090),
	}
}

func newEndpoint(t testing.TB, peers PeerDirectory) helloEndpoint {
	return helloEndpoint{
		guard:   newGuard(),
		respond: newResponder(),
		status:  selfStatus(t),
		peers:   peers,
	}
}

func helloRequest(network string, caller yacymodel.Seed) *http.Request {
	form := yacyproto.HelloRequest{
		NetworkName: network,
		Seed:        caller,
		Iam:         caller.Hash,
	}.Form()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		yacyproto.PathHello,
		nil,
	)
	req.PostForm = form

	return req
}

func parseResponse(t *testing.T, body string) yacyproto.HelloResponse {
	t.Helper()

	message, err := yacymodel.ParseMessage(body)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	resp, err := yacyproto.ParseHelloResponse(context.Background(), message)
	if err != nil {
		t.Fatalf("ParseHelloResponse: %v", err)
	}

	return resp
}

func queryServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := yacyproto.QueryResponse{Response: 3}
		_, _ = io.WriteString(w, resp.Encode().Encode())
	}))
}

func serverSeed(t *testing.T, rawURL string) yacymodel.Seed {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split server host: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}

	return callerSeed(t, "peer", host, port)
}

func TestHelloServesSelfAndKnownSeeds(t *testing.T) {
	known := callerSeed(t, "trusted", "203.0.113.1", 8090)
	endpoint := newEndpoint(t, &stubDirectory{
		outcome: HelloOutcome{CallerType: yacymodel.PeerSenior, Known: []yacymodel.Seed{known}},
	})

	rec := httptest.NewRecorder()
	endpoint.ServeHTTP(rec, helloRequest("freeworld", callerSeed(t, "caller", "10.0.0.1", 8090)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := parseResponse(t, rec.Body.String())
	if resp.Version != "1.0" {
		t.Fatalf("Version = %q, want 1.0", resp.Version)
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

	rec := httptest.NewRecorder()
	endpoint.ServeHTTP(rec, helloRequest("otherworld", callerSeed(t, "caller", "10.0.0.1", 8090)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := parseResponse(t, rec.Body.String())
	if got := len(resp.Seeds); got != 1 {
		t.Fatalf("Seeds = %d, want 1 (self only)", got)
	}
	if directory.called {
		t.Fatal("directory consulted despite foreign network")
	}
}

func TestHelloRejectsWrongMethod(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodDelete,
		yacyproto.PathHello,
		nil,
	)
	newEndpoint(t, &stubDirectory{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHelloRejectsMissingSeed(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		yacyproto.PathHello,
		nil,
	)
	req.PostForm = yacyproto.HelloRequest{NetworkName: "freeworld"}.Form()
	newEndpoint(t, &stubDirectory{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHelloReportsDirectoryFailure(t *testing.T) {
	endpoint := newEndpoint(t, &stubDirectory{err: errors.New("directory down")})

	rec := httptest.NewRecorder()
	endpoint.ServeHTTP(rec, helloRequest("freeworld", callerSeed(t, "caller", "10.0.0.1", 8090)))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func helloThrough(
	t *testing.T,
	client *http.Client,
	caller yacymodel.Seed,
) yacyproto.HelloResponse {
	t.Helper()

	module := New(
		newGuard(),
		newResponder(),
		selfStatus(t),
		client,
		Config{TrustedSeedCapacity: 10},
	)
	module.Registry.Absorb(context.Background(), callerSeed(t, "trusted", "203.0.113.1", 8090))

	rec := httptest.NewRecorder()
	module.HelloEndpoint.ServeHTTP(rec, helloRequest("freeworld", caller))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	return parseResponse(t, rec.Body.String())
}

func TestHelloClassifiesReachableCallerAsSenior(t *testing.T) {
	srv := queryServer()
	defer srv.Close()

	resp := helloThrough(t, srv.Client(), serverSeed(t, srv.URL))

	if resp.YourType != yacymodel.PeerSenior {
		t.Fatalf("YourType = %q, want senior", resp.YourType)
	}
	if got := len(resp.Seeds); got != 2 {
		t.Fatalf("Seeds = %d, want 2 (self + trusted)", got)
	}
}

func TestHelloClassifiesUnreachableCallerAsJunior(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	resp := helloThrough(t, srv.Client(), serverSeed(t, srv.URL))

	if resp.YourType != yacymodel.PeerJunior {
		t.Fatalf("YourType = %q, want junior", resp.YourType)
	}
}

func TestHelloClassifiesAddresslessCallerAsJunior(t *testing.T) {
	resp := helloThrough(t, http.DefaultClient, callerSeed(t, "caller", "", 0))

	if resp.YourType != yacymodel.PeerJunior {
		t.Fatalf("YourType = %q, want junior", resp.YourType)
	}
}

func TestSampleSeedsLimitsAndShuffles(t *testing.T) {
	seeds := []yacymodel.Seed{
		callerSeed(t, "a", "", 0),
		callerSeed(t, "b", "", 0),
		callerSeed(t, "c", "", 0),
	}
	dir := newPeerDirectory(
		stubProbe{},
		NewTrustedSeedRegistry(10),
		reverseShuffle,
		stubStatus{},
	)

	picked := dir.sampleSeeds(seeds, 2)

	got := []yacymodel.Hash{picked[0].Hash, picked[1].Hash}
	want := []yacymodel.Hash{hashFor("c"), hashFor("b")}
	if !slices.Equal(got, want) {
		t.Fatalf("picked = %v, want %v", got, want)
	}
}

func TestSampleSeedsCountZeroReturnsAll(t *testing.T) {
	seeds := []yacymodel.Seed{callerSeed(t, "a", "", 0), callerSeed(t, "b", "", 0)}
	dir := newPeerDirectory(
		stubProbe{},
		NewTrustedSeedRegistry(10),
		noShuffle,
		stubStatus{},
	)

	if got := len(dir.sampleSeeds(seeds, 0)); got != 2 {
		t.Fatalf("picked = %d, want 2", got)
	}
}

func TestCallerBackPingRejectsUnaddressableSeed(t *testing.T) {
	probe := newCallerBackPing(http.DefaultClient)

	if probe.Reachable(
		context.Background(),
		callerSeed(t, "peer", "", 0),
		hashFor("self"),
		"freeworld",
	) {
		t.Fatal("Reachable = true, want false for a seed without an address")
	}
}

func TestRegistryDiscardsBeyondCapacity(t *testing.T) {
	registry := NewTrustedSeedRegistry(1)
	registry.Absorb(context.Background(), callerSeed(t, "a", "", 0))
	registry.Absorb(context.Background(), callerSeed(t, "b", "", 0))

	trusted := registry.Trusted(context.Background())
	if len(trusted) != 1 {
		t.Fatalf("trusted = %d, want 1 (capacity enforced)", len(trusted))
	}
	if trusted[0].Hash != hashFor("a") {
		t.Fatalf("retained %q, want first absorbed", trusted[0].Hash)
	}
}
