package extractfetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestFetcherKeepsPlainTextVerbatim(t *testing.T) {
	body := "# Heading\n\n- first <b>item</b>\n- second item\n"
	for _, contentType := range []string{"text/plain; charset=utf-8", "text/markdown"} {
		content, err := New(htmlClient(body, contentType), 0, 0).
			Fetch(context.Background(), "https://example.com/readme.md")
		if err != nil {
			t.Fatalf("%s: Fetch: %v", contentType, err)
		}
		if content.Text != "# Heading\n\n- first <b>item</b>\n- second item" ||
			content.Title != "" || content.Images != nil {
			t.Fatalf("%s: content = %+v", contentType, content)
		}
	}
}

func TestFetchPageReturnsPlainTextWithoutLinks(t *testing.T) {
	page, err := New(htmlClient(`see <a href="/x">x</a>`, "text/plain"), 0, 0).
		FetchPage(context.Background(), "https://example.com/notes.txt")
	if err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if page.Text != `see <a href="/x">x</a>` || page.Links != nil {
		t.Fatalf("page = %+v", page)
	}
}

func TestFetcherRejectsPlainTextSizePlusOne(t *testing.T) {
	body := "plain"
	if _, err := New(htmlClient(body, "text/plain"), 0, int64(len(body))).
		Fetch(context.Background(), "https://example.com/"); err != nil {
		t.Fatalf("exact limit rejected: %v", err)
	}
	if _, err := New(htmlClient(body+"x", "text/plain"), 0, int64(len(body))).
		Fetch(context.Background(), "https://example.com/"); err == nil {
		t.Fatal("maximum plus one plain text accepted")
	}
}

func TestFetcherReportsPlainTextReadFailure(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Type", "text/plain")

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(failingReader{}),
			Header:     header,
		}, nil
	})}
	if _, err := New(client, 0, 0).Fetch(context.Background(), "https://example.com/"); err == nil {
		t.Fatal("plain text read failure accepted")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }
