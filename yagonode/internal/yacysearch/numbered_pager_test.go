package yacysearch

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestHTMLNumberedPagesWindows(t *testing.T) {
	resp := searchcore.Response{TotalResults: 250, Request: searchcore.Request{Limit: 10}}

	first := htmlNumberedPages("/yacysearch.html", resp, 1, 10)
	if len(first) != htmlPagerWindow || first[0].Number != 1 || !first[0].Current {
		t.Fatalf("first window = %+v", first)
	}

	tail := htmlNumberedPages("/yacysearch.html", resp, 25, 10)
	if tail[0].Number != 16 || tail[len(tail)-1].Number != 25 {
		t.Fatalf("tail window = %d..%d", tail[0].Number, tail[len(tail)-1].Number)
	}

	single := searchcore.Response{TotalResults: 5, Request: searchcore.Request{Limit: 10}}
	if got := htmlNumberedPages("/yacysearch.html", single, 1, 10); got != nil {
		t.Fatalf("single page must render no numbers: %+v", got)
	}
}
