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

func TestFetcherBoundsResponseSize(t *testing.T) {
	body := "<html><body>" + strings.Repeat("a", 1000) + "</body></html>"
	content, err := New(htmlClient(body, "text/html"), 0, 16).
		Fetch(context.Background(), "https://example.com/")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(content.Text) > 16 {
		t.Errorf("text not bounded: %q", content.Text)
	}
}
