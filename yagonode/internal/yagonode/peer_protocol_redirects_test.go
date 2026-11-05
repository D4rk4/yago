package yagonode

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/peerwire"
)

func TestPeerClientRefusesRedirectWithoutChangingGenericClient(t *testing.T) {
	var toHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/from" {
			http.Redirect(w, r, "/to", http.StatusTemporaryRedirect)
			return
		}
		if r.URL.Path == "/to" {
			toHits.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	base := server.Client()
	peer := nodePeerClient(nodeConfig{}, base)

	response := clientGet(t, context.Background(), peer, server.URL+"/from")
	defer closeBody(response)
	if response.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("peer followed redirect: status=%d", response.StatusCode)
	}
	if got := toHits.Load(); got != 0 {
		t.Fatalf("peer request leaked to redirect destination: hits=%d", got)
	}

	response = clientGet(t, context.Background(), base, server.URL+"/from")
	defer closeBody(response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("generic redirect behavior changed: status=%d", response.StatusCode)
	}
	if got := toHits.Load(); got != 1 {
		t.Fatalf("generic client did not follow redirect: hits=%d", got)
	}
}

func TestPeerClientRefusesEveryRedirectStatus(t *testing.T) {
	statuses := []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	}
	methods := []string{http.MethodGet, http.MethodPost}
	origins := []string{"same", "cross"}
	for _, status := range statuses {
		t.Run("status="+http.StatusText(status), func(t *testing.T) {
			for _, method := range methods {
				t.Run("method="+method, func(t *testing.T) {
					for _, origin := range origins {
						t.Run("origin="+origin, func(t *testing.T) {
							refuseRedirectsFor(t, status, method, origin)
						})
					}
				})
			}
		})
	}
}

func refuseRedirectsFor(t *testing.T, status int, method, origin string) {
	t.Helper()
	var destHits atomic.Int64
	var crossServer *httptest.Server
	if origin == "cross" {
		crossServer = newDestinationServer(t, &destHits)
		defer crossServer.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/from" {
			target := "/to"
			if crossServer != nil {
				target = crossServer.URL + "/to"
			}
			http.Redirect(w, r, target, status)
			return
		}
		destHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	peer := peerClientWithoutRedirects(server.Client())

	var req *http.Request
	var err error
	if method == http.MethodGet {
		req, err = http.NewRequestWithContext(context.Background(), http.MethodGet,
			server.URL+"/from", nil)
	} else {
		body, contentType := peerwire.EncodeMultipartForm(url.Values{"k": {"v"}})
		req, err = http.NewRequestWithContext(context.Background(), http.MethodPost,
			server.URL+"/from", bytes.NewReader(body))
		req.Header.Set("Content-Type", contentType)
	}
	if err != nil {
		t.Fatal(err)
	}

	response, err := peer.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBody(response)
	if response.StatusCode != status {
		t.Fatalf("peer followed redirect: status=%d want %d", response.StatusCode, status)
	}
	if got := destHits.Load(); got != 0 {
		t.Fatalf("peer request leaked to destination: hits=%d want 0", got)
	}
}

func newDestinationServer(t *testing.T, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
}

func TestPeerClientRefusesRedirectWithCanceledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/from" {
			http.Redirect(w, r, "/to", http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/from", nil)
	if err != nil {
		t.Fatal(err)
	}
	peer := peerClientWithoutRedirects(server.Client())
	response, err := peer.Do(req)
	if err == nil {
		defer closeBody(response)
		t.Fatalf("expected canceled-context error, got status=%d", response.StatusCode)
	}
}

func TestPeerClientDirectSuccessStillWorks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	peer := peerClientWithoutRedirects(server.Client())
	response := clientGet(t, context.Background(), peer, server.URL+"/ok")
	defer closeBody(response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("direct success status=%d want %d", response.StatusCode, http.StatusOK)
	}
}

func clientGet(t *testing.T, ctx context.Context, client *http.Client, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func closeBody(response *http.Response) {
	if err := response.Body.Close(); err != nil {
		_, _ = io.WriteString(io.Discard, err.Error())
	}
}
