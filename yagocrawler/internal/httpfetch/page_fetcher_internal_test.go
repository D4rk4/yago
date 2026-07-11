package httpfetch

import (
	"testing"
	"time"
)

func TestResponseContentType(t *testing.T) {
	if got := responseContentType(" text/html ", []byte("ignored")); got != " text/html " {
		t.Fatalf("header content type = %q", got)
	}
	if got := responseContentType("", nil); got != "text/plain; charset=utf-8" {
		t.Fatalf("empty body must sniff to the detector default, got %q", got)
	}
	if got := responseContentType("", []byte("<html></html>")); got != "text/html; charset=utf-8" {
		t.Fatalf("detected content type = %q", got)
	}
}

func TestResponseLastModified(t *testing.T) {
	if got := responseLastModified("bad"); !got.IsZero() {
		t.Fatalf("invalid last-modified = %v", got)
	}
	if got := responseLastModified(
		"Wed, 01 Jul 2026 10:00:00 GMT",
	); got != time.Date(
		2026,
		7,
		1,
		10,
		0,
		0,
		0,
		time.UTC,
	) {
		t.Fatalf("last-modified = %v", got)
	}
}
