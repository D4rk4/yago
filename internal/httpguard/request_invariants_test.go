package httpguard_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func testGuard() httpguard.RequestGuard {
	return httpguard.NewRequestGuard(
		yacymodel.PeerIdentity{Hash: yacymodel.WordHash("self"), NetworkName: "freeworld"},
		32,
		time.Second,
	)
}

func TestParseRejectsDisallowedMethod(t *testing.T) {
	guard := testGuard()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yacyproto.PathTransferURL,
		nil,
	)

	_, _, _, ok := guard.Parse(rec, req, yacyproto.TransferURLEndpointMethods)
	if ok {
		t.Fatal("Parse accepted GET on a POST-only endpoint")
	}
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestParseRejectsOversizedBody(t *testing.T) {
	guard := testGuard()
	rec := httptest.NewRecorder()
	body := strings.NewReader(strings.Repeat("x", 1024))
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yacyproto.PathTransferURL,
		body,
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, _, _, ok := guard.Parse(rec, req, yacyproto.TransferURLEndpointMethods)
	if ok {
		t.Fatal("Parse accepted an oversized body")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestParseAcceptsValidPost(t *testing.T) {
	guard := testGuard()
	rec := httptest.NewRecorder()
	body := strings.NewReader("a=b")
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yacyproto.PathTransferURL,
		body,
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	form, ctx, cancel, ok := guard.Parse(rec, req, yacyproto.TransferURLEndpointMethods)
	if !ok {
		t.Fatalf("Parse rejected a valid request, status %d", rec.Code)
	}
	defer cancel()
	if ctx == nil {
		t.Fatal("Parse returned a nil context")
	}
	if form.Get("a") != "b" {
		t.Fatalf("form[a] = %q, want b", form.Get("a"))
	}
}

func TestNetworkAndYouAreMatch(t *testing.T) {
	guard := testGuard()

	if !guard.NetworkMatches(url.Values{yacyproto.FieldNetworkName: {"freeworld"}}) {
		t.Fatal("NetworkMatches rejected the configured network")
	}
	if guard.NetworkMatches(url.Values{yacyproto.FieldNetworkName: {"othernet"}}) {
		t.Fatal("NetworkMatches accepted a foreign network")
	}
	if !guard.YouAreMatches(yacymodel.WordHash("self")) {
		t.Fatal("YouAreMatches rejected the local hash")
	}
	if guard.YouAreMatches(yacymodel.WordHash("other")) {
		t.Fatal("YouAreMatches accepted a foreign hash")
	}
}
