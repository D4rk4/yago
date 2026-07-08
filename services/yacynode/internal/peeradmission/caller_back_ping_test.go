package peeradmission

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

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

func TestCallerBackPingConfirmsValidQueryResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := yacyproto.QueryResponse{Response: 3}
		_, _ = io.WriteString(w, resp.Encode().Encode())
	}))
	defer srv.Close()

	probe := newCallerBackPing(srv.Client())

	if !probe.Reachable(
		context.Background(),
		serverSeed(t, srv.URL),
		hashFor("self"),
		"freeworld",
	) {
		t.Fatal("Reachable = false, want true for a confirming caller")
	}
}

func TestCallerBackPingRejectsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	probe := newCallerBackPing(srv.Client())

	if probe.Reachable(context.Background(), serverSeed(t, srv.URL), hashFor("self"), "freeworld") {
		t.Fatal("Reachable = true, want false on error status")
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
