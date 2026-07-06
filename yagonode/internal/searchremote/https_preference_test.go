package searchremote

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

// sslSeed decorates a plain-http server seed with an advertised HTTPS endpoint
// on the given port, the way a YaCy peer publishes PortSSL plus the SSL flag.
func sslSeed(tb testing.TB, raw string, sslPort int) yagomodel.Seed {
	tb.Helper()
	seed := serverSeed(tb, raw)
	flags, _ := seed.Flags.Get()
	seed.Flags = yagomodel.Some(flags.Set(yagomodel.FlagSSLAvailable, true))
	seed.PortSSL = yagomodel.Some(yagomodel.Port(sslPort))
	seed.Version = yagomodel.Some(yagomodel.YaCyVersion("1.941"))

	return seed
}

// deadPort reserves a port with no listener so an https attempt fails at the
// transport layer.
func deadPort(tb testing.TB, ctx context.Context) int {
	tb.Helper()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("reserve port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		tb.Fatalf("release port: %v", err)
	}

	return port
}

func TestRemoteSearcherFallsBackToPlainHTTPWhenHTTPSFails(t *testing.T) {
	word := yagomodel.WordHash("golang")
	hash := hashFor("doc1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := yagoproto.SearchResponse{
			JoinCount: 1,
			Count:     1,
			Resources: []yagomodel.URIMetadataRow{
				metadataRow(t, hash, "https://example.org/doc.html", "Remote Result"),
			},
		}
		message := response.Encode()
		yagoproto.InjectResponseHeader(message, "1.940", 42)
		writeFixtureResponse(t, w, message.Encode())
	}))
	defer server.Close()

	// The advertised https endpoint has no listener, so the searcher must
	// retry the same peer over plain http, YaCy-style.
	peer := sslSeed(t, server.URL, deadPort(t, t.Context()))
	resp, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       fakePeerSource{peers: []yagomodel.Seed{peer}},
		PreferHTTPS: true,
	}).Search(t.Context(), searchcore.Request{
		Terms:  []string{word.String()},
		Source: searchcore.SourceGlobal,
		Limit:  10,
		Verify: searchcore.VerifyFalse,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].URL != "https://example.org/doc.html" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRemoteSearcherPrefersAdvertisedHTTPS(t *testing.T) {
	word := yagomodel.WordHash("golang")
	var plainHits int
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		plainHits++
	}))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		message := yagoproto.SearchResponse{}.Encode()
		yagoproto.InjectResponseHeader(message, "1.940", 42)
		writeFixtureResponse(t, w, message.Encode())
	}))
	defer secure.Close()

	securePort := serverSeed(t, secure.URL)
	sslPort, _ := securePort.Port.Get()
	peer := sslSeed(t, plain.URL, int(sslPort))

	resp, err := NewSearcher(Config{
		Client:      secure.Client(),
		NetworkName: "freeworld",
		Peers:       fakePeerSource{peers: []yagomodel.Seed{peer}},
		PreferHTTPS: true,
	}).Search(t.Context(), searchcore.Request{
		Terms:  []string{word.String()},
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if plainHits != 0 {
		t.Fatalf("plain http endpoint hit %d times despite a working https endpoint", plainHits)
	}
	if len(resp.PartialFailures) != 0 {
		t.Fatalf("partial failures = %#v", resp.PartialFailures)
	}
}
