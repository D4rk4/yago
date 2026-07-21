package yagonode

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestPublicEndpointSelfTestAcceptsZeroRWICount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := yagoproto.QueryResponse{Response: 0}
		_, _ = strings.NewReader(response.Encode().Encode()).WriteTo(w)
	}))
	defer server.Close()

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	probe := newPublicEndpointSelfTest(
		server.Client(),
		"freeworld",
		yagomodel.Hash("AAAAAAAAAAAA"),
		base,
	)
	probe.pinned = true
	if !probe.Reachable(t.Context()) {
		t.Fatal("zero RWI capacity response did not confirm reachability")
	}
}
