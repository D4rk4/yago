package yagonode

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yagoegress"
)

type recordingRoundTripper struct {
	seenUserAgent string
}

func (r *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.seenUserAgent = req.Header.Get("User-Agent")

	return &http.Response{
		StatusCode: http.StatusNoContent,
		Body:       http.NoBody,
		Header:     http.Header{},
	}, nil
}

func TestUserAgentTransportBrandsRequestsWithoutAgent(t *testing.T) {
	recorder := &recordingRoundTripper{}
	transport := userAgentTransport{agent: userAgent, next: recorder}

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://peer.example.net/yacy/hello.html",
		nil,
	)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if recorder.seenUserAgent != userAgent {
		t.Fatalf("User-Agent = %q, want the branded agent %q", recorder.seenUserAgent, userAgent)
	}
	if got := req.Header.Get("User-Agent"); got != "" {
		t.Errorf("caller request mutated: User-Agent = %q, want it left unset", got)
	}
}

func TestUserAgentTransportPreservesCallerAgent(t *testing.T) {
	recorder := &recordingRoundTripper{}
	transport := userAgentTransport{agent: userAgent, next: recorder}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.net/", nil)
	req.Header.Set("User-Agent", "yago-extract/1.0")
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if recorder.seenUserAgent != "yago-extract/1.0" {
		t.Fatalf("User-Agent = %q, want the caller's agent preserved", recorder.seenUserAgent)
	}
}

func TestNewGuardedEgressClientInstallsUserAgentTransport(t *testing.T) {
	client := newGuardedEgressClient(yagoegress.NewGuard(false))

	if _, ok := client.Transport.(userAgentTransport); !ok {
		t.Fatalf("transport = %T, want userAgentTransport", client.Transport)
	}
}
