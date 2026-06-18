//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type httpProbe struct {
	t        testing.TB
	client   *http.Client
	failDiag map[string]string
}

type probeResult struct {
	ok          bool
	body        string
	httpStatus  int
	contentType string
	errMsg      string
}

func newHTTPProbe(t testing.TB) *httpProbe {
	t.Helper()
	return &httpProbe{
		t:        t,
		client:   &http.Client{Timeout: 15 * time.Second},
		failDiag: map[string]string{},
	}
}

func (p *httpProbe) Get(ctx context.Context, u string) probeResult {
	p.t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return probeResult{errMsg: err.Error()}
	}
	return p.do(req)
}

func (p *httpProbe) OK(ctx context.Context, u string) bool {
	p.t.Helper()
	return p.Get(ctx, u).ok
}

func (p *httpProbe) PostRaw(ctx context.Context, u, body string, headers ...string) probeResult {
	p.t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(body))
	if err != nil {
		return probeResult{errMsg: err.Error()}
	}
	for _, h := range headers {
		if name, value, found := strings.Cut(h, ":"); found {
			req.Header.Set(strings.TrimSpace(name), strings.TrimSpace(value))
		}
	}
	return p.do(req)
}

func (p *httpProbe) do(req *http.Request) probeResult {
	p.t.Helper()
	var result probeResult
	resp, err := p.client.Do(req)
	if err != nil {
		result = probeResult{errMsg: err.Error()}
	} else {
		raw, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		result = probeResult{
			body:        string(raw),
			httpStatus:  resp.StatusCode,
			contentType: resp.Header.Get("Content-Type"),
		}
		switch {
		case readErr != nil:
			result.errMsg = "read body: " + readErr.Error()
		case resp.StatusCode < 200 || resp.StatusCode >= 300:
			result.errMsg = "non-2xx status"
		default:
			result.ok = true
		}
	}
	p.logFailureChanged(req.URL.String(), result)
	return result
}

func (p *httpProbe) logFailureChanged(u string, result probeResult) {
	if result.ok {
		delete(p.failDiag, u)
		return
	}
	diag := result.diag()
	if p.failDiag[u] != diag {
		p.t.Logf("e2e probe failed url=%s %s", u, diag)
		p.failDiag[u] = diag
	}
}

func (r probeResult) diag() string {
	return fmt.Sprintf("http_status=%d content_type=%q err=%q body_preview=%q",
		r.httpStatus, r.contentType, r.errMsg, shortPreview(r.body))
}

func shortPreview(s string) string {
	const limit = 240
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", `\n`)
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
