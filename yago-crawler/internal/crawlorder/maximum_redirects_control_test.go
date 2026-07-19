package crawlorder

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestMaximumRedirectsControlAppliesBoundedDirective(t *testing.T) {
	var applied []int
	handler := NewMaximumRedirectsControl(func(maximum int) {
		applied = append(applied, maximum)
	}, nil)
	for _, maximum := range []uint32{0, 10, yagocrawlcontract.MaximumPageRedirects} {
		handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
			Kind:             yagocrawlcontract.CrawlControlSetMaximumRedirects,
			MaximumRedirects: maximum,
		})
	}
	handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
		Kind:             yagocrawlcontract.CrawlControlSetMaximumRedirects,
		MaximumRedirects: yagocrawlcontract.MaximumPageRedirects + 1,
	})
	if len(applied) != 3 || applied[0] != 0 || applied[1] != 10 ||
		applied[2] != yagocrawlcontract.MaximumPageRedirects {
		t.Fatalf("applied redirect limits = %v", applied)
	}
}

func TestMaximumRedirectsControlDelegatesOtherDirectives(t *testing.T) {
	next := &recordingControlHandler{}
	handler := NewMaximumRedirectsControl(nil, next)
	directive := yagocrawlcontract.CrawlControlDirective{
		Kind:         yagocrawlcontract.CrawlControlSetWorkers,
		FetchWorkers: 2,
	}
	handler.Apply(t.Context(), directive)
	if got := next.snapshot(); len(got) != 1 || got[0] != directive {
		t.Fatalf("delegated directives = %+v", got)
	}
	if NewMaximumRedirectsControl(nil, nil).ApplyControl(
		t.Context(),
		yagocrawlcontract.CrawlControlDirective{
			Kind:             yagocrawlcontract.CrawlControlSetMaximumRedirects,
			MaximumRedirects: 10,
		},
	) {
		t.Fatal("maximum redirects without an apply sink was acknowledged")
	}
	NewMaximumRedirectsControl(nil, nil).Apply(t.Context(), directive)
}
