package adminui

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakePublicSearchStatusSource struct {
	status PublicSearchStatus
}

func (s *fakePublicSearchStatusSource) PublicSearchStatus(context.Context) PublicSearchStatus {
	return s.status
}

func requestWithHost(host string) *http.Request {
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/admin/overview", nil)
	req.Host = host

	return req
}

func TestPublicListenerPort(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		":8090":        "8090",
		"0.0.0.0:8090": "8090",
		"127.0.0.1:80": "80",
		"  :8090  ":    "8090",
		"":             "",
		"off":          "",
		"8090":         "",
		"[::1]:8090":   "8090",
	}
	for addr, want := range cases {
		if got := publicListenerPort(addr); got != want {
			t.Fatalf("publicListenerPort(%q) = %q, want %q", addr, got, want)
		}
	}
}

func TestPublicSearchHrefConfiguredBaseWins(t *testing.T) {
	t.Parallel()

	console := New(Options{PublicBaseURL: "https://search.example.com/", PublicAddr: ":8090"})
	if got := console.publicSearchHref(
		requestWithHost("node.local:9090"),
	); got != "https://search.example.com" {
		t.Fatalf("configured base: got %q", got)
	}
}

func TestPublicSearchHrefDerivedFromRequestHost(t *testing.T) {
	t.Parallel()

	console := New(Options{PublicAddr: ":8090"})
	if got := console.publicSearchHref(
		requestWithHost("node.local:9090"),
	); got != "http://node.local:8090/" {
		t.Fatalf("derived href: got %q", got)
	}
	if got := console.publicSearchHref(
		requestWithHost("node.local"),
	); got != "http://node.local:8090/" {
		t.Fatalf("derived href without request port: got %q", got)
	}
}

func TestPublicSearchHrefHTTPS(t *testing.T) {
	t.Parallel()

	console := New(Options{PublicAddr: ":8090"})

	forwarded := requestWithHost("node.local:9090")
	forwarded.Header.Set("X-Forwarded-Proto", "https")
	if got := console.publicSearchHref(forwarded); got != "https://node.local:8090/" {
		t.Fatalf("forwarded https: got %q", got)
	}

	direct := requestWithHost("node.local:9090")
	direct.TLS = &tls.ConnectionState{}
	if got := console.publicSearchHref(direct); got != "https://node.local:8090/" {
		t.Fatalf("tls https: got %q", got)
	}
}

func TestPublicSearchHrefHiddenWhenDisabled(t *testing.T) {
	t.Parallel()

	// No base and no public listener: nothing to link to.
	if got := New(Options{}).publicSearchHref(requestWithHost("node.local:9090")); got != "" {
		t.Fatalf("disabled surface: got %q", got)
	}
	// A listener but no request host to derive from.
	if got := New(Options{PublicAddr: ":8090"}).publicSearchHref(requestWithHost("")); got != "" {
		t.Fatalf("missing host: got %q", got)
	}
	// A port-only Host (":9090") leaves no hostname after splitting.
	if got := New(
		Options{PublicAddr: ":8090"},
	).publicSearchHref(requestWithHost(":9090")); got != "" {
		t.Fatalf("port-only host: got %q", got)
	}
}

func TestPublicSearchLinkRendersInHeader(t *testing.T) {
	t.Parallel()

	console := New(Options{Config: fakeConfig{view: ConfigView{}}, PublicAddr: ":8090"})
	got := do(t, console, "/admin/configuration")
	if !strings.Contains(got.body, `href="http://example.com:8090/"`) ||
		!strings.Contains(got.body, "Public search") {
		t.Fatalf("public search link missing: %s", got.body)
	}
}

func TestPublicSearchLinkOmittedWhenDisabled(t *testing.T) {
	t.Parallel()

	console := New(Options{Config: fakeConfig{view: ConfigView{}}})
	got := do(t, console, "/admin/configuration")
	if strings.Contains(got.body, "Public search") {
		t.Fatalf("public search link rendered while disabled: %s", got.body)
	}
}

func TestPublicSearchHrefTracksLivePortalStatus(t *testing.T) {
	t.Parallel()

	status := &fakePublicSearchStatusSource{status: PublicSearchStatus{
		Enabled: true,
		BaseURL: "https://one.example/",
	}}
	console := New(Options{PublicSearch: status, PublicAddr: ":8090"})
	request := requestWithHost("node.local:9090")
	if got := console.publicSearchHref(request); got != "https://one.example" {
		t.Fatalf("initial href = %q", got)
	}

	status.status = PublicSearchStatus{Enabled: false, BaseURL: "https://two.example"}
	if got := console.publicSearchHref(request); got != "" {
		t.Fatalf("disabled href = %q", got)
	}

	status.status = PublicSearchStatus{Enabled: true}
	if got := console.publicSearchHref(request); got != "http://node.local:8090/" {
		t.Fatalf("derived live href = %q", got)
	}
}

func TestConfigurationLabelsStartupSnapshot(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Config: fakeConfig{view: ConfigView{}}}), "/admin/configuration")
	for _, want := range []string{
		"Startup snapshot",
		"Values applied when this process started.",
		"Editable values above are the desired settings",
		"pending restart",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("configuration page missing %q", want)
		}
	}
	if strings.Contains(got.body, "Values in effect for this node") {
		t.Fatal("configuration page claims the startup snapshot is live")
	}
}
