package pagefetch

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestDuplicatePageFetchSuppressorSeparatesValidators(t *testing.T) {
	inner := newBlockingPageSource()
	suppressor := NewDuplicatePageFetchSuppressor(inner)
	target := parseFetchTarget(t, "https://example.test/page")
	fetch := func(entityTag string) <-chan pageFetchResult {
		result := make(chan pageFetchResult, 1)
		go func() {
			page, err := suppressor.Fetch(t.Context(), FetchRequest{
				Target:     target,
				Validators: yagocrawlcontract.HTTPValidators{ETag: entityTag},
			})
			result <- pageFetchResult{page: page, err: err}
		}()

		return result
	}
	first := fetch(`"v1"`)
	<-inner.started
	second := fetch(`"v2"`)
	select {
	case <-inner.started:
	case <-time.After(time.Second):
		t.Fatal("a fetch with different validators joined the in-flight fetch")
	}
	inner.release <- pageFetchResult{}
	inner.release <- pageFetchResult{}
	if result := <-first; result.err != nil {
		t.Fatalf("first fetch: %v", result.err)
	}
	if result := <-second; result.err != nil {
		t.Fatalf("second fetch: %v", result.err)
	}
	if inner.calls.Load() != 2 {
		t.Fatalf("underlying fetches = %d, want 2", inner.calls.Load())
	}
}
