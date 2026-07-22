package seedlist

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type seedStatus struct {
	seed yagomodel.Seed
}

func (s seedStatus) SelfSeed(context.Context) yagomodel.Seed {
	return s.seed
}

type seedReachability struct {
	seeds  []yagomodel.Seed
	active []yagomodel.Seed
}

func (s seedReachability) ReachablePeers(context.Context) []yagomodel.Seed {
	if s.active != nil {
		return s.active
	}

	return s.seeds
}

func (s seedReachability) SeedlistPeers(_ context.Context, limit int) []yagomodel.Seed {
	if limit < len(s.seeds) {
		return s.seeds[:max(limit, 0)]
	}

	return s.seeds
}

func (s seedReachability) PeerByHash(
	_ context.Context,
	peer yagomodel.Hash,
) (yagomodel.Seed, bool) {
	for _, seed := range s.seeds {
		if seed.Hash == peer {
			return seed, true
		}
	}

	return yagomodel.Seed{}, false
}

func (s seedReachability) PeerByName(
	_ context.Context,
	name string,
) (yagomodel.Seed, bool) {
	for _, seed := range s.seeds {
		seedName, known := seed.Name.Get()
		if known && yagomodel.NormalizeSeedName(seedName) == yagomodel.NormalizeSeedName(name) {
			return seed, true
		}
	}

	return yagomodel.Seed{}, false
}

func TestSelfSeedNameKeepsRawAngleVocabulary(t *testing.T) {
	self := yagomodel.Seed{Name: yagomodel.Some("self_peer")}
	if selfSeedNameMatches(self, "self<peer") {
		t.Fatal("angle-normalized query matched self")
	}
	if !selfSeedNameMatches(self, "SELF_PEER") {
		t.Fatal("case-insensitive self name did not match")
	}
}

func seedEndpoint(tb testing.TB) endpoint {
	tb.Helper()

	return endpoint{
		status: seedStatus{seed: seed(tb, "self", "self-peer", "192.0.2.10")},
		peers: seedReachability{seeds: []yagomodel.Seed{
			seed(tb, "alpha", "alpha-peer", "192.0.2.11"),
			seed(tb, "beta", "beta-peer", "192.0.2.12"),
		}},
	}
}

func seed(tb interface {
	Helper()
	Fatalf(string, ...any)
}, word, name, host string,
) yagomodel.Seed {
	tb.Helper()

	parsedHost, err := yagomodel.ParseHost(host)
	if err != nil {
		tb.Fatalf("ParseHost(%q): %v", host, err)
	}

	return yagomodel.Seed{
		Hash:     yagomodel.WordHash(word),
		Name:     yagomodel.Some(name),
		IP:       yagomodel.Some(parsedHost),
		Port:     yagomodel.Some(yagomodel.Port(8090)),
		PeerType: yagomodel.Some(yagomodel.PeerSenior),
	}
}

func versionedSeed(tb interface {
	Helper()
	Fatalf(string, ...any)
}, word, name, host, version string,
) yagomodel.Seed {
	tb.Helper()

	seed := seed(tb, word, name, host)
	seed.Version = yagomodel.Some(yagomodel.YaCyVersion(version))

	return seed
}

func seedWithIPv6(tb interface {
	Helper()
	Fatalf(string, ...any)
}, word, name, host string,
) yagomodel.Seed {
	tb.Helper()

	parsedHost, err := yagomodel.ParseHost(host)
	if err != nil {
		tb.Fatalf("ParseHost(%q): %v", host, err)
	}

	return yagomodel.Seed{
		Hash: yagomodel.WordHash(word),
		Name: yagomodel.Some(name),
		IP6:  yagomodel.Some([]yagomodel.Host{parsedHost}),
		Port: yagomodel.Some(yagomodel.Port(8090)),
	}
}

func responseLines(t testing.TB, body string) []yagomodel.Seed {
	t.Helper()

	rawLines := strings.Split(strings.TrimSuffix(body, seedlistLineBreak), seedlistLineBreak)
	seeds := make([]yagomodel.Seed, 0, len(rawLines))
	for _, raw := range rawLines {
		seed, err := yagomodel.ParseSeedWireForm(t.Context(), raw)
		if err != nil {
			t.Fatalf("ParseSeedWireForm(%q): %v", raw, err)
		}
		seeds = append(seeds, seed)
	}

	return seeds
}

func seedlistGate() httpguard.WireGate {
	return httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(4096, time.Second),
		Address: httpguard.NewClientAddressResolver(nil),
	}
}

func TestMountServesSeedlistRoutes(t *testing.T) {
	mux := http.NewServeMux()
	Mount(
		httpguard.NewWireRouter(mux, seedlistGate()),
		seedEndpoint(t).status,
		seedEndpoint(t).peers,
	)

	for _, route := range []struct {
		path        string
		contentType string
	}{
		{path: yagoproto.PathSeedlist, contentType: "text/html; charset=UTF-8"},
		{path: yagoproto.PathSeedlistJSON, contentType: "application/json; charset=UTF-8"},
		{path: yagoproto.PathSeedlistXML, contentType: "application/xml; charset=UTF-8"},
		{path: yagoproto.PathP2PSeeds, contentType: "text/html; charset=UTF-8"},
		{path: yagoproto.PathP2PSeedsJSON, contentType: "application/json; charset=UTF-8"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, route.path, nil)

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", route.path, rec.Code)
		}
		if rec.Header().Get("Content-Type") != route.contentType {
			t.Fatalf("%s content type = %q", route.path, rec.Header().Get("Content-Type"))
		}
	}
}

func TestSeedlistIncludesSelfAndReachablePeersByDefault(t *testing.T) {
	resp, err := seedEndpoint(
		t,
	).ServeHTML(t.Context(), yagoproto.SeedlistRequest{IncludeSelf: true})
	if err != nil {
		t.Fatal(err)
	}

	if resp.ContentType != "text/html; charset=UTF-8" {
		t.Fatalf("ContentType = %q", resp.ContentType)
	}
	if !strings.HasSuffix(resp.Body, seedlistLineBreak) {
		t.Fatalf("response does not end with CRLF: %q", resp.Body)
	}
	if !strings.HasPrefix(resp.Body, "b|") && !strings.HasPrefix(resp.Body, "z|") {
		t.Fatalf("response does not use a compact seed wire form: %q", resp.Body)
	}

	seeds := responseLines(t, resp.Body)
	if got := len(seeds); got != 3 {
		t.Fatalf("seed count = %d, want 3", got)
	}
	if seeds[0].Hash != yagomodel.WordHash("self") {
		t.Fatalf("first seed = %q, want self", seeds[0].Hash)
	}
}

func TestSeedlistCanExcludeSelf(t *testing.T) {
	resp, err := seedEndpoint(
		t,
	).ServeHTML(t.Context(), yagoproto.SeedlistRequest{IncludeSelf: false})
	if err != nil {
		t.Fatal(err)
	}

	seeds := responseLines(t, resp.Body)
	if got := len(seeds); got != 2 {
		t.Fatalf("seed count = %d, want 2", got)
	}
	for _, seed := range seeds {
		if seed.Hash == yagomodel.WordHash("self") {
			t.Fatal("self seed present")
		}
	}
}

func TestSeedlistCanReturnOnlySelf(t *testing.T) {
	resp, err := seedEndpoint(t).ServeHTML(
		t.Context(),
		yagoproto.SeedlistRequest{IncludeSelf: true, OwnSeedOnly: true},
	)
	if err != nil {
		t.Fatal(err)
	}

	seeds := responseLines(t, resp.Body)
	if got := len(seeds); got != 1 {
		t.Fatalf("seed count = %d, want 1", got)
	}
	if seeds[0].Hash != yagomodel.WordHash("self") {
		t.Fatalf("seed = %q, want self", seeds[0].Hash)
	}
}

func TestSeedlistFiltersByIDAndName(t *testing.T) {
	resp, err := seedEndpoint(t).ServeHTML(
		t.Context(),
		yagoproto.SeedlistRequest{
			IncludeSelf: true,
			ID:          yagomodel.Some(yagomodel.WordHash("alpha")),
			Name:        "alpha-peer",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	seeds := responseLines(t, resp.Body)
	if got := len(seeds); got != 1 {
		t.Fatalf("seed count = %d, want 1", got)
	}
	if seeds[0].Hash != yagomodel.WordHash("alpha") {
		t.Fatalf("seed = %q, want alpha", seeds[0].Hash)
	}
}

func TestSeedlistMalformedIDReturnsAnEmptyLookup(t *testing.T) {
	request, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistID: {"short"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := seedEndpoint(t).ServeHTML(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Body != "" {
		t.Fatalf("body = %q, want empty lookup", response.Body)
	}
}

func TestSeedlistBareNameReturnsAnEmptyLookup(t *testing.T) {
	request, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistName: {""}},
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := seedEndpoint(t).ServeHTML(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Body != "" {
		t.Fatalf("body = %q, want empty lookup", response.Body)
	}
}

func TestSeedlistMaxCountCapsOutput(t *testing.T) {
	resp, err := seedEndpoint(t).ServeHTML(
		t.Context(),
		yagoproto.SeedlistRequest{
			IncludeSelf: true,
			MaxCount:    yagomodel.Some(2),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	seeds := responseLines(t, resp.Body)
	if got := len(seeds); got != 2 {
		t.Fatalf("seed count = %d, want 2", got)
	}
}

func TestSeedlistMaxCountAboveProtocolLimitKeepsAvailableSeeds(t *testing.T) {
	resp, err := seedEndpoint(t).ServeHTML(
		t.Context(),
		yagoproto.SeedlistRequest{
			IncludeSelf: true,
			MaxCount:    yagomodel.Some(seedlistMaxEntries + 1),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	seeds := responseLines(t, resp.Body)
	if got := len(seeds); got != 3 {
		t.Fatalf("seed count = %d, want 3", got)
	}
}

func TestSeedlistNegativeMaxCountStillReturnsSelf(t *testing.T) {
	resp, err := seedEndpoint(t).ServeHTML(
		t.Context(),
		yagoproto.SeedlistRequest{
			IncludeSelf: true,
			MaxCount:    yagomodel.Some(-1),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	seeds := responseLines(t, resp.Body)
	if len(seeds) != 1 || seeds[0].Hash != yagomodel.WordHash("self") {
		t.Fatalf("seeds = %#v, want self", seeds)
	}
}

func TestSeedlistFiltersByMinimumVersion(t *testing.T) {
	malformed := seed(t, "badversion", "badversion-peer", "192.0.2.14")
	malformed.Version = yagomodel.Some(yagomodel.YaCyVersion("current"))
	endpoint := endpoint{
		status: seedStatus{
			seed: versionedSeed(t, "self", "self-peer", "192.0.2.10", "1.82"),
		},
		peers: seedReachability{seeds: []yagomodel.Seed{
			versionedSeed(t, "alpha", "alpha-peer", "192.0.2.11", "1.83"),
			versionedSeed(t, "beta", "beta-peer", "192.0.2.12", "1.91"),
			seed(t, "missing", "missing-peer", "192.0.2.13"),
			malformed,
		}},
	}

	resp, err := endpoint.ServeHTML(
		t.Context(),
		yagoproto.SeedlistRequest{
			IncludeSelf: true,
			MinVersion:  yagomodel.Some(1.85),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	seeds := responseLines(t, resp.Body)
	if got := len(seeds); got != 4 {
		t.Fatalf("seed count = %d, want 4", got)
	}
	if seeds[0].Hash != yagomodel.WordHash("self") ||
		seeds[1].Hash != yagomodel.WordHash("beta") ||
		seeds[2].Hash != yagomodel.WordHash("missing") ||
		seeds[3].Hash != yagomodel.WordHash("badversion") {
		t.Fatalf("seeds = %#v, want self/beta/missing/badversion", seeds)
	}
}

func TestSeedlistNodeOnlyFiltersPeersWithoutRootFlag(t *testing.T) {
	endpoint := seedEndpoint(t)
	root := seed(t, "alpha", "alpha-peer", "192.0.2.11")
	root.Flags = yagomodel.Some(yagomodel.ZeroFlags().Set(yagomodel.FlagRootNode, true))
	endpoint.peers = seedReachability{seeds: []yagomodel.Seed{
		seed(t, "nonroot", "nonroot-peer", "192.0.2.12"),
		root,
	}}

	resp, err := endpoint.ServeHTML(
		t.Context(),
		yagoproto.SeedlistRequest{IncludeSelf: false, NodeOnly: true},
	)
	if err != nil {
		t.Fatal(err)
	}

	seeds := responseLines(t, resp.Body)
	if got := len(seeds); got != 1 {
		t.Fatalf("seed count = %d, want 1", got)
	}
	if seeds[0].Hash != yagomodel.WordHash("alpha") {
		t.Fatalf("seed = %q, want alpha", seeds[0].Hash)
	}
}

func TestSeedlistClearTextIgnoresPeerName(t *testing.T) {
	resp, err := seedEndpoint(t).ServeHTML(
		t.Context(),
		yagoproto.SeedlistRequest{
			IncludeSelf: true,
			PeerName:    "beta-peer",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	seeds := responseLines(t, resp.Body)
	if got := len(seeds); got != 3 {
		t.Fatalf("seed count = %d, want 3", got)
	}
}

func TestSeedlistStructuredFormatsFilterByPeerName(t *testing.T) {
	req := yagoproto.SeedlistRequest{IncludeSelf: true, PeerName: "beta-peer"}
	jsonResponse, err := seedEndpoint(t).ServeJSON(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonResponse.Body, yagomodel.WordHash("beta").String()) ||
		strings.Contains(jsonResponse.Body, yagomodel.WordHash("self").String()) {
		t.Fatalf("JSON peername response = %s", jsonResponse.Body)
	}
	xmlResponse, err := seedEndpoint(t).ServeXML(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(xmlResponse.Body, yagomodel.WordHash("beta").String()) ||
		strings.Contains(xmlResponse.Body, yagomodel.WordHash("self").String()) {
		t.Fatalf("XML peername response = %s", xmlResponse.Body)
	}
}

func TestYaCySeedlistRequestMatrixFixtures(t *testing.T) {
	self := yagomodel.WordHash("self")
	alpha := yagomodel.WordHash("alpha")
	beta := yagomodel.WordHash("beta")
	tests := []struct {
		name string
		req  yagoproto.SeedlistRequest
		want []yagomodel.Hash
	}{
		{
			name: "my precedes every selector",
			req: yagoproto.SeedlistRequest{
				OwnSeedOnly: true, ID: yagomodel.Some(alpha), Name: "beta-peer",
				NodeOnly: true, MaxCount: yagomodel.Some(0), MinVersion: yagomodel.Some(999.0),
			},
			want: []yagomodel.Hash{self},
		},
		{
			name: "id precedes name and regular filters",
			req: yagoproto.SeedlistRequest{
				ID: yagomodel.Some(alpha), Name: "beta-peer", NodeOnly: true,
				IncludeSelf: false, MaxCount: yagomodel.Some(0), MinVersion: yagomodel.Some(999.0),
			},
			want: []yagomodel.Hash{alpha},
		},
		{
			name: "name precedes regular filters",
			req: yagoproto.SeedlistRequest{
				Name: "beta-peer", NodeOnly: true, IncludeSelf: false,
				MaxCount: yagomodel.Some(0), MinVersion: yagomodel.Some(999.0),
			},
			want: []yagomodel.Hash{beta},
		},
		{
			name: "zero maximum preserves self",
			req:  yagoproto.SeedlistRequest{IncludeSelf: true, MaxCount: yagomodel.Some(0)},
			want: []yagomodel.Hash{self},
		},
		{
			name: "zero maximum without self is empty",
			req:  yagoproto.SeedlistRequest{IncludeSelf: false, MaxCount: yagomodel.Some(0)},
		},
		{
			name: "node filter preserves self",
			req:  yagoproto.SeedlistRequest{IncludeSelf: true, NodeOnly: true},
			want: []yagomodel.Hash{self},
		},
		{
			name: "self id lookup",
			req:  yagoproto.SeedlistRequest{ID: yagomodel.Some(self), IncludeSelf: false},
			want: []yagomodel.Hash{self},
		},
		{
			name: "self name lookup is case insensitive",
			req:  yagoproto.SeedlistRequest{Name: "SELF-PEER", IncludeSelf: false},
			want: []yagomodel.Hash{self},
		},
		{
			name: "self name lookup preserves whitespace",
			req:  yagoproto.SeedlistRequest{Name: " SELF-PEER ", IncludeSelf: false},
		},
		{
			name: "localpeer alias resolves self",
			req:  yagoproto.SeedlistRequest{Name: "localpeer", IncludeSelf: false},
			want: []yagomodel.Hash{self},
		},
		{
			name: "yacy suffix resolves remote name",
			req:  yagoproto.SeedlistRequest{Name: "alpha-peer.yacy", IncludeSelf: false},
			want: []yagomodel.Hash{alpha},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := seedEndpoint(t).selectSeeds(t.Context(), test.req)
			if !slices.Equal(seedHashes(got), test.want) {
				t.Fatalf("hashes = %v, want %v", seedHashes(got), test.want)
			}
		})
	}
}

func TestSeedlistUnknownIDReturnsNoSeeds(t *testing.T) {
	got := seedEndpoint(t).selectSeeds(t.Context(), yagoproto.SeedlistRequest{
		ID: yagomodel.Some(yagomodel.WordHash("unknown")),
	})
	if got != nil {
		t.Fatalf("seeds = %v, want nil", got)
	}
}

func TestSeedlistMinimumVersionAppliesOnlyToActivePeers(t *testing.T) {
	activeOld := versionedSeed(t, "active-old", "active-old", "192.0.2.20", "1.0")
	activeNew := versionedSeed(t, "active-new", "active-new", "192.0.2.21", "2.0")
	passiveOld := versionedSeed(t, "passive-old", "passive-old", "192.0.2.22", "1.0")
	directory := seedReachability{
		seeds:  []yagomodel.Seed{activeOld, activeNew, passiveOld},
		active: []yagomodel.Seed{activeOld, activeNew},
	}
	endpoint := endpoint{
		status: seedStatus{seed: seed(t, "self", "self-peer", "192.0.2.10")},
		peers:  directory,
	}
	got := endpoint.selectSeeds(t.Context(), yagoproto.SeedlistRequest{
		IncludeSelf: false, MinVersion: yagomodel.Some(1.5),
	})
	want := []yagomodel.Hash{activeNew.Hash, passiveOld.Hash}
	if !slices.Equal(seedHashes(got), want) {
		t.Fatalf("hashes = %v, want %v", seedHashes(got), want)
	}
}

func TestSeedlistDefaultVersionFloorExcludesNegativeActivePeer(t *testing.T) {
	activeNegative := versionedSeed(
		t,
		"active-negative",
		"active-negative",
		"192.0.2.20",
		"-1.0",
	)
	activeZero := versionedSeed(t, "active-zero", "active-zero", "192.0.2.21", "0")
	passiveNegative := versionedSeed(
		t,
		"passive-negative",
		"passive-negative",
		"192.0.2.22",
		"-1.0",
	)
	directory := seedReachability{
		seeds:  []yagomodel.Seed{activeNegative, activeZero, passiveNegative},
		active: []yagomodel.Seed{activeNegative, activeZero},
	}
	endpoint := endpoint{
		status: seedStatus{seed: seed(t, "self", "self-peer", "192.0.2.10")},
		peers:  directory,
	}
	got := endpoint.selectSeeds(t.Context(), yagoproto.SeedlistRequest{IncludeSelf: false})
	want := []yagomodel.Hash{activeZero.Hash, passiveNegative.Hash}
	if !slices.Equal(seedHashes(got), want) {
		t.Fatalf("hashes = %v, want %v", seedHashes(got), want)
	}
}

func TestSeedlistOwnSeedStructuredOutputIgnoresPeerName(t *testing.T) {
	req := yagoproto.SeedlistRequest{
		OwnSeedOnly: true, PeerName: "different-peer", MaxCount: yagomodel.Some(0),
	}
	response, err := seedEndpoint(t).ServeJSON(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Body, yagomodel.WordHash("self").String()) {
		t.Fatalf("own seed missing: %s", response.Body)
	}
	response, err = seedEndpoint(t).ServeXML(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Body, yagomodel.WordHash("self").String()) {
		t.Fatalf("own seed missing: %s", response.Body)
	}
}

func seedHashes(seeds []yagomodel.Seed) []yagomodel.Hash {
	hashes := make([]yagomodel.Hash, 0, len(seeds))
	for _, seed := range seeds {
		hashes = append(hashes, seed.Hash)
	}

	return hashes
}

func TestSeedlistJSONReturnsClearTextPeerMaps(t *testing.T) {
	resp, err := seedEndpoint(t).ServeJSON(
		t.Context(),
		yagoproto.SeedlistRequest{IncludeSelf: true, MaxCount: yagomodel.Some(1)},
	)
	if err != nil {
		t.Fatal(err)
	}

	if resp.ContentType != seedlistJSONContentType {
		t.Fatalf("ContentType = %q", resp.ContentType)
	}

	var payload struct {
		Peers []map[string]any `json:"peers"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &payload); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, resp.Body)
	}
	if got := len(payload.Peers); got != 1 {
		t.Fatalf("peer count = %d, want 1", got)
	}
	peer := payload.Peers[0]
	if peer[yagomodel.SeedHash] != yagomodel.WordHash("self").String() {
		t.Fatalf("Hash = %v", peer[yagomodel.SeedHash])
	}
	if peer[yagomodel.SeedName] != "self-peer" {
		t.Fatalf("Name = %v", peer[yagomodel.SeedName])
	}
	addresses, ok := peer["Address"].([]any)
	if !ok || len(addresses) != 1 || addresses[0] != "192.0.2.10:8090" {
		t.Fatalf("Address = %#v", peer["Address"])
	}
}

func TestSeedlistJSONReturnsEncodingError(t *testing.T) {
	saved := marshalSeedlistJSON
	t.Cleanup(func() { marshalSeedlistJSON = saved })
	sentinel := errors.New("json failed")
	marshalSeedlistJSON = func(any, string, string) ([]byte, error) {
		return nil, sentinel
	}

	_, err := seedEndpoint(t).ServeJSON(
		t.Context(),
		yagoproto.SeedlistRequest{IncludeSelf: true},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("ServeJSON error = %v, want %v", err, sentinel)
	}
}

func TestSeedlistPresentEmptyPeerNameAndCallbackKeepUpstreamSemantics(t *testing.T) {
	request, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{
			yagoproto.FieldSeedlistPeerName: {""},
			yagoproto.FieldSeedlistCallback: {""},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := seedEndpoint(t).ServeJSON(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.ContentType != "application/json; charset=UTF-8" {
		t.Fatalf("ContentType = %q", response.ContentType)
	}
	if !strings.HasPrefix(response.Body, "([") || !strings.HasSuffix(response.Body, "]);") {
		t.Fatalf("JSONP body = %q", response.Body)
	}
	if strings.Contains(response.Body, yagomodel.WordHash("alpha").String()) ||
		strings.Contains(response.Body, yagomodel.WordHash("beta").String()) {
		t.Fatalf("present-empty peername did not filter peers: %s", response.Body)
	}
}

func TestSeedlistJSONAddressOnlyOmitsSeedProperties(t *testing.T) {
	resp, err := seedEndpoint(t).ServeJSON(
		t.Context(),
		yagoproto.SeedlistRequest{
			IncludeSelf: true,
			MaxCount:    yagomodel.Some(1),
			AddressOnly: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	var payload struct {
		Peers []map[string]any `json:"peers"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := payload.Peers[0][yagomodel.SeedName]; ok {
		t.Fatalf("Name present in address-only response: %#v", payload.Peers[0])
	}
	if _, ok := payload.Peers[0][yagomodel.SeedHash]; !ok {
		t.Fatalf("Hash absent: %#v", payload.Peers[0])
	}
	if _, ok := payload.Peers[0]["Address"]; !ok {
		t.Fatalf("Address absent: %#v", payload.Peers[0])
	}
}

func TestSeedlistJSONPSupportsUpstreamCallbackShape(t *testing.T) {
	resp, err := seedEndpoint(t).ServeJSON(
		t.Context(),
		yagoproto.SeedlistRequest{IncludeSelf: true, Callback: "seedlist"},
	)
	if err != nil {
		t.Fatal(err)
	}

	if resp.ContentType != "application/json; charset=UTF-8" {
		t.Fatalf("ContentType = %q", resp.ContentType)
	}
	if !strings.HasPrefix(resp.Body, "seedlist([{") {
		t.Fatalf("JSONP prefix = %q", resp.Body[:min(len(resp.Body), 16)])
	}
	if !strings.HasSuffix(resp.Body, "}]);") {
		t.Fatalf("JSONP suffix missing: %q", resp.Body)
	}
}

func TestSeedlistXMLReturnsSeedElements(t *testing.T) {
	resp, err := seedEndpoint(t).ServeXML(
		t.Context(),
		yagoproto.SeedlistRequest{IncludeSelf: true, MaxCount: yagomodel.Some(1)},
	)
	if err != nil {
		t.Fatal(err)
	}

	if resp.ContentType != seedlistXMLContentType {
		t.Fatalf("ContentType = %q", resp.ContentType)
	}

	var payload struct {
		Seeds []struct {
			Hash    string   `xml:"Hash"`
			Name    string   `xml:"Name"`
			Address []string `xml:"Address"`
		} `xml:"seed"`
	}
	if err := xml.Unmarshal([]byte(resp.Body), &payload); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, resp.Body)
	}
	if got := len(payload.Seeds); got != 1 {
		t.Fatalf("seed count = %d, want 1", got)
	}
	if payload.Seeds[0].Hash != yagomodel.WordHash("self").String() {
		t.Fatalf("Hash = %q", payload.Seeds[0].Hash)
	}
	if payload.Seeds[0].Name != "self-peer" {
		t.Fatalf("Name = %q", payload.Seeds[0].Name)
	}
	if len(payload.Seeds[0].Address) != 1 || payload.Seeds[0].Address[0] != "192.0.2.10:8090" {
		t.Fatalf("Address = %#v", payload.Seeds[0].Address)
	}
}

func TestSeedlistXMLAddressOnlyOmitsSeedProperties(t *testing.T) {
	resp, err := seedEndpoint(t).ServeXML(
		t.Context(),
		yagoproto.SeedlistRequest{
			IncludeSelf: true,
			MaxCount:    yagomodel.Some(1),
			AddressOnly: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(resp.Body, "<Name>") {
		t.Fatalf("Name present in address-only XML: %s", resp.Body)
	}
	if !strings.Contains(resp.Body, "<Hash>") || !strings.Contains(resp.Body, "<Address>") {
		t.Fatalf("required XML fields absent: %s", resp.Body)
	}
}

func TestSeedlistXMLSkipsAddresslessSeeds(t *testing.T) {
	endpoint := seedEndpoint(t)
	endpoint.peers = seedReachability{seeds: []yagomodel.Seed{
		{Hash: yagomodel.WordHash("addressless"), Name: yagomodel.Some("addressless")},
	}}

	resp, err := endpoint.ServeXML(
		t.Context(),
		yagoproto.SeedlistRequest{IncludeSelf: false},
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(resp.Body, "<seed>") {
		t.Fatalf("addressless seed encoded: %s", resp.Body)
	}
}

func TestSeedlistIncludesIPv6Addresses(t *testing.T) {
	endpoint := seedEndpoint(t)
	endpoint.peers = seedReachability{seeds: []yagomodel.Seed{
		seedWithIPv6(t, "ipv6", "ipv6-peer", "2001:db8::1"),
	}}

	resp, err := endpoint.ServeJSON(
		t.Context(),
		yagoproto.SeedlistRequest{IncludeSelf: false},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Body, "[2001:db8::1]:8090") {
		t.Fatalf("IPv6 address absent: %s", resp.Body)
	}
}

func TestSeedlistAddressesPreserveOrderWithoutDuplicates(t *testing.T) {
	seed := seed(t, "deduplicated", "deduplicated-peer", "192.0.2.10")
	primary, _ := seed.IP.Get()
	ipv6, err := yagomodel.ParseHost("2001:db8::1")
	if err != nil {
		t.Fatal(err)
	}
	seed.IP6 = yagomodel.Some([]yagomodel.Host{primary, ipv6, ipv6})

	addresses := seedAddresses(seed)
	want := []string{"192.0.2.10:8090", "[2001:db8::1]:8090"}
	if !slices.Equal(addresses, want) {
		t.Fatalf("addresses = %#v, want %#v", addresses, want)
	}
}

func TestSeedlistClearTextSkipsAddresslessSeeds(t *testing.T) {
	endpoint := seedEndpoint(t)
	endpoint.peers = seedReachability{seeds: []yagomodel.Seed{
		{Hash: yagomodel.WordHash("addressless"), Name: yagomodel.Some("addressless")},
	}}

	resp, err := endpoint.ServeJSON(
		t.Context(),
		yagoproto.SeedlistRequest{IncludeSelf: false},
	)
	if err != nil {
		t.Fatal(err)
	}

	var payload struct {
		Peers []map[string]any `json:"peers"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(payload.Peers) != 0 {
		t.Fatalf("Peers = %#v, want empty", payload.Peers)
	}
}
