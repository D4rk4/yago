//go:build e2e

package httpprobe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type Probe struct {
	t        testing.TB
	client   *http.Client
	failDiag map[string]string
}

type Result struct {
	OK          bool
	Body        string
	HTTPStatus  int
	ContentType string
	ErrMsg      string
}

func New(t testing.TB) *Probe {
	t.Helper()
	return &Probe{
		t:        t,
		client:   &http.Client{Timeout: 15 * time.Second},
		failDiag: map[string]string{},
	}
}

func (p *Probe) Get(ctx context.Context, url string) Result {
	p.t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Result{ErrMsg: err.Error()}
	}
	return p.do(req)
}

func (p *Probe) OK(ctx context.Context, url string) bool {
	p.t.Helper()
	return p.Get(ctx, url).OK
}

func (p *Probe) PostRaw(ctx context.Context, url, body string, headers ...string) Result {
	p.t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return Result{ErrMsg: err.Error()}
	}
	for _, header := range headers {
		if name, value, found := strings.Cut(header, ":"); found {
			req.Header.Set(strings.TrimSpace(name), strings.TrimSpace(value))
		}
	}
	return p.do(req)
}

func (p *Probe) do(req *http.Request) Result {
	p.t.Helper()
	var result Result
	resp, err := p.client.Do(req)
	if err != nil {
		result = Result{ErrMsg: err.Error()}
	} else {
		raw, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		result = Result{
			Body:        string(raw),
			HTTPStatus:  resp.StatusCode,
			ContentType: resp.Header.Get("Content-Type"),
		}
		switch {
		case readErr != nil:
			result.ErrMsg = "read body: " + readErr.Error()
		case resp.StatusCode < 200 || resp.StatusCode >= 300:
			result.ErrMsg = "non-2xx status"
		default:
			result.OK = true
		}
	}
	p.logFailureChanged(req.URL.String(), result)
	return result
}

func (p *Probe) logFailureChanged(url string, result Result) {
	if result.OK {
		delete(p.failDiag, url)
		return
	}
	diag := result.Diag()
	if p.failDiag[url] != diag {
		p.t.Logf("e2e probe failed url=%s %s", url, diag)
		p.failDiag[url] = diag
	}
}

func (r Result) Diag() string {
	return fmt.Sprintf("http_status=%d content_type=%q err=%q body_preview=%q",
		r.HTTPStatus, r.ContentType, r.ErrMsg, shortPreview(r.Body))
}

func shortPreview(body string) string {
	const limit = 240
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", `\n`)
	if len(body) <= limit {
		return body
	}
	return body[:limit] + "…"
}
