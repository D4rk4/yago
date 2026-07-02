package seedlist

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

type seedStatus struct {
	seed yacymodel.Seed
}

func (s seedStatus) SelfSeed(context.Context) yacymodel.Seed {
	return s.seed
}

type seedReachability struct {
	seeds []yacymodel.Seed
}

func (s seedReachability) ReachablePeers(context.Context) []yacymodel.Seed {
	return s.seeds
}

func seedEndpoint(tb testing.TB) endpoint {
	tb.Helper()

	return endpoint{
		status: seedStatus{seed: seed(tb, "self", "self-peer", "192.0.2.10")},
		reachability: seedReachability{seeds: []yacymodel.Seed{
			seed(tb, "alpha", "alpha-peer", "192.0.2.11"),
			seed(tb, "beta", "beta-peer", "192.0.2.12"),
		}},
	}
}

func seed(tb interface {
	Helper()
	Fatalf(string, ...any)
}, word, name, host string,
) yacymodel.Seed {
	tb.Helper()

	parsedHost, err := yacymodel.ParseHost(host)
	if err != nil {
		tb.Fatalf("ParseHost(%q): %v", host, err)
	}

	return yacymodel.Seed{
		Hash:     yacymodel.WordHash(word),
		Name:     yacymodel.Some(name),
		IP:       yacymodel.Some(parsedHost),
		Port:     yacymodel.Some(yacymodel.Port(8090)),
		PeerType: yacymodel.Some(yacymodel.PeerSenior),
	}
}

func seedWithIPv6(tb interface {
	Helper()
	Fatalf(string, ...any)
}, word, name, host string,
) yacymodel.Seed {
	tb.Helper()

	parsedHost, err := yacymodel.ParseHost(host)
	if err != nil {
		tb.Fatalf("ParseHost(%q): %v", host, err)
	}

	return yacymodel.Seed{
		Hash: yacymodel.WordHash(word),
		Name: yacymodel.Some(name),
		IP6:  yacymodel.Some([]yacymodel.Host{parsedHost}),
		Port: yacymodel.Some(yacymodel.Port(8090)),
	}
}

func responseLines(t testing.TB, body string) []yacymodel.Seed {
	t.Helper()

	rawLines := strings.Split(strings.TrimSuffix(body, seedlistLineBreak), seedlistLineBreak)
	seeds := make([]yacymodel.Seed, 0, len(rawLines))
	for _, raw := range rawLines {
		seed, err := yacymodel.ParseSeed(t.Context(), raw)
		if err != nil {
			t.Fatalf("ParseSeed(%q): %v", raw, err)
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
		seedEndpoint(t).reachability,
	)

	for _, path := range []string{
		yacyproto.PathSeedlist,
		yacyproto.PathSeedlistJSON,
		yacyproto.PathSeedlistXML,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rec.Code)
		}
	}
}

func TestSeedlistIncludesSelfAndReachablePeersByDefault(t *testing.T) {
	resp, err := seedEndpoint(
		t,
	).ServeHTML(t.Context(), yacyproto.SeedlistRequest{IncludeSelf: true})
	if err != nil {
		t.Fatal(err)
	}

	if resp.ContentType != seedlistHTMLContentType {
		t.Fatalf("ContentType = %q", resp.ContentType)
	}
	if !strings.HasSuffix(resp.Body, seedlistLineBreak) {
		t.Fatalf("response does not end with CRLF: %q", resp.Body)
	}

	seeds := responseLines(t, resp.Body)
	if got := len(seeds); got != 3 {
		t.Fatalf("seed count = %d, want 3", got)
	}
	if seeds[0].Hash != yacymodel.WordHash("self") {
		t.Fatalf("first seed = %q, want self", seeds[0].Hash)
	}
}

func TestSeedlistCanExcludeSelf(t *testing.T) {
	resp, err := seedEndpoint(
		t,
	).ServeHTML(t.Context(), yacyproto.SeedlistRequest{IncludeSelf: false})
	if err != nil {
		t.Fatal(err)
	}

	seeds := responseLines(t, resp.Body)
	if got := len(seeds); got != 2 {
		t.Fatalf("seed count = %d, want 2", got)
	}
	for _, seed := range seeds {
		if seed.Hash == yacymodel.WordHash("self") {
			t.Fatal("self seed present")
		}
	}
}

func TestSeedlistCanReturnOnlySelf(t *testing.T) {
	resp, err := seedEndpoint(t).ServeHTML(
		t.Context(),
		yacyproto.SeedlistRequest{IncludeSelf: true, OwnSeedOnly: true},
	)
	if err != nil {
		t.Fatal(err)
	}

	seeds := responseLines(t, resp.Body)
	if got := len(seeds); got != 1 {
		t.Fatalf("seed count = %d, want 1", got)
	}
	if seeds[0].Hash != yacymodel.WordHash("self") {
		t.Fatalf("seed = %q, want self", seeds[0].Hash)
	}
}

func TestSeedlistFiltersByIDAndName(t *testing.T) {
	resp, err := seedEndpoint(t).ServeHTML(
		t.Context(),
		yacyproto.SeedlistRequest{
			IncludeSelf: true,
			ID:          yacymodel.Some(yacymodel.WordHash("alpha")),
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
	if seeds[0].Hash != yacymodel.WordHash("alpha") {
		t.Fatalf("seed = %q, want alpha", seeds[0].Hash)
	}
}

func TestSeedlistMaxCountCapsOutput(t *testing.T) {
	resp, err := seedEndpoint(t).ServeHTML(
		t.Context(),
		yacyproto.SeedlistRequest{
			IncludeSelf: true,
			MaxCount:    yacymodel.Some(2),
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
		yacyproto.SeedlistRequest{
			IncludeSelf: true,
			MaxCount:    yacymodel.Some(seedlistMaxEntries + 1),
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

func TestSeedlistNegativeMaxCountReturnsNoSeeds(t *testing.T) {
	resp, err := seedEndpoint(t).ServeHTML(
		t.Context(),
		yacyproto.SeedlistRequest{
			IncludeSelf: true,
			MaxCount:    yacymodel.Some(-1),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if resp.Body != "" {
		t.Fatalf("body = %q, want empty", resp.Body)
	}
}

func TestSeedlistNodeOnlyFiltersAddresslessSeeds(t *testing.T) {
	endpoint := seedEndpoint(t)
	endpoint.reachability = seedReachability{seeds: []yacymodel.Seed{
		{Hash: yacymodel.WordHash("addressless"), Name: yacymodel.Some("addressless")},
		seed(t, "alpha", "alpha-peer", "192.0.2.11"),
	}}

	resp, err := endpoint.ServeHTML(
		t.Context(),
		yacyproto.SeedlistRequest{IncludeSelf: false, NodeOnly: true},
	)
	if err != nil {
		t.Fatal(err)
	}

	seeds := responseLines(t, resp.Body)
	if got := len(seeds); got != 1 {
		t.Fatalf("seed count = %d, want 1", got)
	}
	if seeds[0].Hash != yacymodel.WordHash("alpha") {
		t.Fatalf("seed = %q, want alpha", seeds[0].Hash)
	}
}

func TestSeedlistFiltersByPeerName(t *testing.T) {
	resp, err := seedEndpoint(t).ServeHTML(
		t.Context(),
		yacyproto.SeedlistRequest{
			IncludeSelf: true,
			PeerName:    "beta-peer",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	seeds := responseLines(t, resp.Body)
	if got := len(seeds); got != 1 {
		t.Fatalf("seed count = %d, want 1", got)
	}
	if seeds[0].Hash != yacymodel.WordHash("beta") {
		t.Fatalf("seed = %q, want beta", seeds[0].Hash)
	}
}

func TestSeedlistJSONReturnsClearTextPeerMaps(t *testing.T) {
	resp, err := seedEndpoint(t).ServeJSON(
		t.Context(),
		yacyproto.SeedlistRequest{IncludeSelf: true, MaxCount: yacymodel.Some(1)},
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
	if peer[yacymodel.SeedHash] != yacymodel.WordHash("self").String() {
		t.Fatalf("Hash = %v", peer[yacymodel.SeedHash])
	}
	if peer[yacymodel.SeedName] != "self-peer" {
		t.Fatalf("Name = %v", peer[yacymodel.SeedName])
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
		yacyproto.SeedlistRequest{IncludeSelf: true},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("ServeJSON error = %v, want %v", err, sentinel)
	}
}

func TestSeedlistJSONAddressOnlyOmitsSeedProperties(t *testing.T) {
	resp, err := seedEndpoint(t).ServeJSON(
		t.Context(),
		yacyproto.SeedlistRequest{
			IncludeSelf: true,
			MaxCount:    yacymodel.Some(1),
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
	if _, ok := payload.Peers[0][yacymodel.SeedName]; ok {
		t.Fatalf("Name present in address-only response: %#v", payload.Peers[0])
	}
	if _, ok := payload.Peers[0][yacymodel.SeedHash]; !ok {
		t.Fatalf("Hash absent: %#v", payload.Peers[0])
	}
	if _, ok := payload.Peers[0]["Address"]; !ok {
		t.Fatalf("Address absent: %#v", payload.Peers[0])
	}
}

func TestSeedlistJSONPSupportsUpstreamCallbackShape(t *testing.T) {
	resp, err := seedEndpoint(t).ServeJSON(
		t.Context(),
		yacyproto.SeedlistRequest{IncludeSelf: true, Callback: "seedlist"},
	)
	if err != nil {
		t.Fatal(err)
	}

	if resp.ContentType != seedlistJavaScriptContentType {
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
		yacyproto.SeedlistRequest{IncludeSelf: true, MaxCount: yacymodel.Some(1)},
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
	if payload.Seeds[0].Hash != yacymodel.WordHash("self").String() {
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
		yacyproto.SeedlistRequest{
			IncludeSelf: true,
			MaxCount:    yacymodel.Some(1),
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
	endpoint.reachability = seedReachability{seeds: []yacymodel.Seed{
		{Hash: yacymodel.WordHash("addressless"), Name: yacymodel.Some("addressless")},
	}}

	resp, err := endpoint.ServeXML(
		t.Context(),
		yacyproto.SeedlistRequest{IncludeSelf: false},
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
	endpoint.reachability = seedReachability{seeds: []yacymodel.Seed{
		seedWithIPv6(t, "ipv6", "ipv6-peer", "2001:db8::1"),
	}}

	resp, err := endpoint.ServeJSON(
		t.Context(),
		yacyproto.SeedlistRequest{IncludeSelf: false},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Body, "[2001:db8::1]:8090") {
		t.Fatalf("IPv6 address absent: %s", resp.Body)
	}
}

func TestSeedlistClearTextSkipsAddresslessSeeds(t *testing.T) {
	endpoint := seedEndpoint(t)
	endpoint.reachability = seedReachability{seeds: []yacymodel.Seed{
		{Hash: yacymodel.WordHash("addressless"), Name: yacymodel.Some("addressless")},
	}}

	resp, err := endpoint.ServeJSON(
		t.Context(),
		yacyproto.SeedlistRequest{IncludeSelf: false},
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
