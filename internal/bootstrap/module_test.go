package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const hashFiller = "AAAAAAAAAAAA"

func hashFor(base string) yacymodel.Hash {
	if len(base) >= yacymodel.HashLength {
		return yacymodel.Hash(base[:yacymodel.HashLength])
	}

	return yacymodel.Hash(base + hashFiller[len(base):])
}

func callerSeed(hash, ip string, port int) yacymodel.Seed {
	return yacymodel.Seed{
		Hash: hashFor(hash),
		IP:   yacymodel.Some(yacymodel.Host(ip)),
		Port: yacymodel.Some(yacymodel.Port(port)),
	}
}

func envFrom(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

type stubStatus struct {
	seed yacymodel.Seed
}

func (s stubStatus) Snapshot(context.Context) StatusSnapshot {
	return StatusSnapshot{Seed: s.seed}
}

type recordingSink struct {
	seeds []yacymodel.Seed
}

func (r *recordingSink) Absorb(_ context.Context, seeds ...yacymodel.Seed) {
	r.seeds = append(r.seeds, seeds...)
}

type fakeConfig struct {
	seedlists []string
}

func (c fakeConfig) SeedlistURLs() []string          { return c.seedlists }
func (c fakeConfig) AnnounceInterval() time.Duration { return time.Hour }

type fakeFetcher struct {
	seeds map[string][]yacymodel.Seed
	err   error
}

func (f fakeFetcher) Fetch(_ context.Context, url string) ([]yacymodel.Seed, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.seeds[url], nil
}

type fakeGreeter struct {
	endpoints []string
	known     []yacymodel.Seed
	err       error
}

func (g *fakeGreeter) Greet(
	_ context.Context,
	endpoint string,
	_ yacymodel.Seed,
	_ int,
) (GreetResult, error) {
	g.endpoints = append(g.endpoints, endpoint)
	if g.err != nil {
		return GreetResult{}, g.err
	}

	return GreetResult{Known: g.known}, nil
}

func TestLoadBootstrapSettingsParsesValues(t *testing.T) {
	settings, err := LoadBootstrapSettings(envFrom(map[string]string{
		EnvSeedlistURLs:     " http://a , http://b ,",
		EnvAnnounceInterval: "30s",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := settings.SeedlistURLs(); len(got) != 2 || got[0] != "http://a" ||
		got[1] != "http://b" {
		t.Fatalf("urls = %v, want trimmed pair", got)
	}
	if settings.AnnounceInterval() != 30*time.Second {
		t.Fatalf("interval = %v, want 30s", settings.AnnounceInterval())
	}
}

func TestLoadBootstrapSettingsDefaultsInterval(t *testing.T) {
	settings, err := LoadBootstrapSettings(envFrom(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settings.AnnounceInterval() != defaultAnnounceInterval {
		t.Fatalf("interval = %v, want default", settings.AnnounceInterval())
	}
	if got := settings.SeedlistURLs(); got != nil {
		t.Fatalf("urls = %v, want nil", got)
	}
}

func TestLoadBootstrapSettingsRejectsBadInterval(t *testing.T) {
	if _, err := LoadBootstrapSettings(envFrom(map[string]string{
		EnvAnnounceInterval: "nope",
	})); err == nil {
		t.Fatal("expected error for unparseable interval")
	}
	if _, err := LoadBootstrapSettings(envFrom(map[string]string{
		EnvAnnounceInterval: "-1s",
	})); err == nil {
		t.Fatal("expected error for non-positive interval")
	}
}

func seedlistLine(t *testing.T, hash, ip string) string {
	t.Helper()

	host, err := yacymodel.ParseHost(ip)
	if err != nil {
		t.Fatalf("parse host: %v", err)
	}
	seed := yacymodel.Seed{
		Hash: yacymodel.Hash(hash),
		IP:   yacymodel.Some(host),
		Port: yacymodel.Some(yacymodel.Port(8090)),
	}

	return yacymodel.EncodeCompactWireForm(seed.String())
}

func TestSeedlistFetcherDecodesLines(t *testing.T) {
	body := strings.Join([]string{
		seedlistLine(t, "AAAAAAAAAAAA", "203.0.113.1"),
		"",
		"!!! not a seed line",
		seedlistLine(t, "BBBBBBBBBBBB", "203.0.113.2"),
	}, "\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	fetcher := newHTTPSeedlistFetcher(server.Client())
	seeds, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(seeds) != 2 {
		t.Fatalf("got %d seeds, want 2 (bad line skipped)", len(seeds))
	}
}

func TestSeedlistFetcherRejectsNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer server.Close()

	fetcher := newHTTPSeedlistFetcher(server.Client())
	if _, err := fetcher.Fetch(context.Background(), server.URL); err == nil {
		t.Fatal("expected error on non-200")
	}
}

func endpointOf(t *testing.T, server *httptest.Server) string {
	t.Helper()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	return parsed.Host
}

func TestPeerGreeterLearnsTypeAndKnownSeeds(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		resp := yacyproto.HelloResponse{
			YourIP:   "203.0.113.9",
			YourType: yacymodel.PeerSenior,
			Seeds: []yacymodel.Seed{
				callerSeed("self", "203.0.113.9", 8090),
				callerSeed("known", "198.51.100.7", 8090),
			},
		}
		_, _ = w.Write([]byte(resp.Encode().Encode()))
	}))
	defer server.Close()

	greeter := newHTTPPeerGreeter(server.Client(), "freeworld")
	result, err := greeter.Greet(
		context.Background(),
		endpointOf(t, server),
		callerSeed("self", "203.0.113.9", 8090),
		0,
	)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if gotPath != yacyproto.PathHello {
		t.Errorf("path = %q, want %q", gotPath, yacyproto.PathHello)
	}
	if result.YourType != yacymodel.PeerSenior {
		t.Errorf("type = %v, want senior", result.YourType)
	}
	if len(result.Known) != 1 {
		t.Fatalf("known = %d, want 1 (own seed excluded)", len(result.Known))
	}
}

func TestPeerGreeterRejectsNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	greeter := newHTTPPeerGreeter(server.Client(), "freeworld")
	if _, err := greeter.Greet(
		context.Background(),
		endpointOf(t, server),
		callerSeed("self", "203.0.113.9", 8090),
		0,
	); err == nil {
		t.Fatal("expected error on non-200")
	}
}

func TestPeerGreeterRejectsEmptyEndpoint(t *testing.T) {
	greeter := newHTTPPeerGreeter(http.DefaultClient, "freeworld")
	if _, err := greeter.Greet(
		context.Background(),
		"  ",
		callerSeed("self", "203.0.113.9", 8090),
		0,
	); err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}

func TestAnnounceGreetsDiscoveredEndpoints(t *testing.T) {
	greeter := &fakeGreeter{known: []yacymodel.Seed{callerSeed("known", "198.51.100.1", 8090)}}
	sink := &recordingSink{}
	announcement := newPeerAnnouncement(
		fakeConfig{seedlists: []string{"http://list"}},
		fakeFetcher{seeds: map[string][]yacymodel.Seed{
			"http://list": {callerSeed("disc", "203.0.113.6", 8090)},
		}},
		greeter,
		stubStatus{seed: callerSeed("self", "203.0.113.9", 8090)},
		sink,
	)

	announcement.Announce(context.Background())

	if want := []string{"203.0.113.6:8090"}; len(greeter.endpoints) != len(want) ||
		greeter.endpoints[0] != want[0] {
		t.Fatalf("greeted %v, want %v", greeter.endpoints, want)
	}
	if len(sink.seeds) != 2 {
		t.Fatalf("absorbed %d, want 2 (discovered + greet-known)", len(sink.seeds))
	}
}

func TestAnnounceContinuesWhenSeedlistFails(t *testing.T) {
	greeter := &fakeGreeter{}
	announcement := newPeerAnnouncement(
		fakeConfig{seedlists: []string{"http://list"}},
		fakeFetcher{err: errors.New("offline")},
		greeter,
		stubStatus{seed: callerSeed("self", "203.0.113.9", 8090)},
		&recordingSink{},
	)

	announcement.Announce(context.Background())

	if len(greeter.endpoints) != 0 {
		t.Fatalf("greeted %v, want none", greeter.endpoints)
	}
}

func TestAnnounceCapsGreetCount(t *testing.T) {
	seeds := make([]yacymodel.Seed, announceMaxGreets+5)
	for i := range seeds {
		seeds[i] = callerSeed(string(rune('a'+i)), "203.0.113.6", 8090+i)
	}
	greeter := &fakeGreeter{}
	announcement := newPeerAnnouncement(
		fakeConfig{seedlists: []string{"http://list"}},
		fakeFetcher{seeds: map[string][]yacymodel.Seed{"http://list": seeds}},
		greeter,
		stubStatus{seed: callerSeed("self", "203.0.113.9", 8090)},
		&recordingSink{},
	)

	announcement.Announce(context.Background())

	if len(greeter.endpoints) != announceMaxGreets {
		t.Fatalf("greeted %d, want cap of %d", len(greeter.endpoints), announceMaxGreets)
	}
}

func TestModuleRunStopsOnContextCancel(t *testing.T) {
	module := New(
		http.DefaultClient,
		"freeworld",
		BootstrapSettings{announceInterval: time.Hour},
		stubStatus{seed: callerSeed("self", "203.0.113.9", 8090)},
		&recordingSink{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		module.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
