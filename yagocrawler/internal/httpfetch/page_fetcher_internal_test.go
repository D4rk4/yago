package httpfetch

import "testing"

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
