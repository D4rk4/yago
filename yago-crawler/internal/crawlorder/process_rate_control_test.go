package crawlorder

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestProcessRateControlAppliesBoundedProcessDirective(t *testing.T) {
	var applied []uint32
	next := &recordingControlHandler{}
	handler := NewProcessRateControl(func(rate uint32) {
		applied = append(applied, rate)
	}, next)
	for _, rate := range []uint32{0, 27, yagocrawlcontract.MaximumProcessPagesPerSecond} {
		handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
			Kind:                  yagocrawlcontract.CrawlControlSetProcessRate,
			ProcessPagesPerSecond: rate,
		})
	}
	handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
		Kind:                  yagocrawlcontract.CrawlControlSetProcessRate,
		ProcessPagesPerSecond: yagocrawlcontract.MaximumProcessPagesPerSecond + 1,
	})
	if len(applied) != 3 || applied[0] != 0 || applied[1] != 27 ||
		applied[2] != yagocrawlcontract.MaximumProcessPagesPerSecond {
		t.Fatalf("applied process rates = %v", applied)
	}
	if got := next.snapshot(); len(got) != 0 {
		t.Fatalf("process-rate directives leaked to run controller: %+v", got)
	}
}

func TestProcessRateControlDelegatesOtherDirectives(t *testing.T) {
	next := &recordingControlHandler{}
	handler := NewProcessRateControl(nil, next)
	directive := yagocrawlcontract.CrawlControlDirective{
		Kind:         yagocrawlcontract.CrawlControlSetWorkers,
		FetchWorkers: 2,
	}
	handler.Apply(t.Context(), directive)
	if got := next.snapshot(); len(got) != 1 || got[0] != directive {
		t.Fatalf("delegated directives = %+v", got)
	}
	NewProcessRateControl(nil, nil).Apply(t.Context(), directive)
	if NewProcessRateControl(nil, nil).ApplyControl(
		t.Context(),
		yagocrawlcontract.CrawlControlDirective{
			Kind:                  yagocrawlcontract.CrawlControlSetProcessRate,
			ProcessPagesPerSecond: 10,
		},
	) {
		t.Fatal("process rate without an apply sink was acknowledged")
	}
}
