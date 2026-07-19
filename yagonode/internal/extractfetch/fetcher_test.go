package extractfetch

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func htmlClient(body string, contentType string) *http.Client {
	header := make(http.Header)
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}

	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     header,
		}, nil
	})}
}

func TestFetcherExtractsTitleAndText(t *testing.T) {
	body := `<html><head><title>Hello</title><style>x{color:red}</style></head>` +
		`<body><h1>Head</h1><script>ignore()</script><p>First para.</p><p>Second.</p></body></html>`
	content, err := New(htmlClient(body, "text/html; charset=utf-8"), time.Second, 0).
		Fetch(context.Background(), "https://example.com/")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if content.Title != "Hello" {
		t.Errorf("title = %q", content.Title)
	}
	if content.Text != "Head First para. Second." {
		t.Errorf("text = %q", content.Text)
	}
}

func TestFetcherRejectsNon200(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})}
	if _, err := New(client, 0, 0).Fetch(context.Background(), "https://example.com/"); err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestFetcherRejectsNonHTML(t *testing.T) {
	client := htmlClient(`{"a":1}`, "application/json")
	if _, err := New(client, 0, 0).Fetch(context.Background(), "https://example.com/"); err == nil {
		t.Fatal("expected error for non-HTML content type")
	}
}

func TestFetcherRejectsResponseSizePlusOne(t *testing.T) {
	body := "<html><body>x</body></html>"
	content, err := New(htmlClient(body, "text/html"), 0, int64(len(body))).Fetch(
		context.Background(),
		"https://example.com/",
	)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if content.Text != "x" {
		t.Fatalf("text = %q", content.Text)
	}
	if _, err := New(htmlClient(body+"x", "text/html"), 0, int64(len(body))).Fetch(
		context.Background(),
		"https://example.com/",
	); err == nil {
		t.Fatal("maximum plus one response accepted")
	}
}

func TestFetcherRejectsConfiguredResponseSizeAboveCeiling(t *testing.T) {
	if got := New(nil, 0, MaximumResponseBytes).maxBytes; got != MaximumResponseBytes {
		t.Fatalf("exact maximum = %d", got)
	}
	rejected := false
	func() {
		defer func() {
			rejected = recover() != nil
		}()
		New(nil, 0, MaximumResponseBytes+1)
	}()
	if !rejected {
		t.Fatal("maximum plus one was not rejected")
	}
}
