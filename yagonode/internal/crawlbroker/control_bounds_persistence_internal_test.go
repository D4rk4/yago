package crawlbroker

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type boundedFleetSetting struct {
	name      string
	set       func(*ControlRegistry, int) int
	get       func(*ControlRegistry) int
	atMaximum int
	pastLimit int
	belowZero int
}

// TestRefusedFleetSettingsAreNotPersisted pins what a refused bound must leave
// behind. The setters report a refusal as zero signalled workers, which is also
// what a legitimate change reports when no crawler is connected — so a broker
// configured before its fleet dials in cannot distinguish the two from the return
// value. That makes the stored value the only observable proof the guard ran: if
// an out-of-range setting were recorded and merely left unsignalled, the first
// crawler to connect would receive it in its initial convergence directives and
// run past the contract ceiling. The registries here deliberately have no
// registered worker so the signal count cannot mask the state change.
func TestRefusedFleetSettingsAreNotPersisted(t *testing.T) {
	for _, setting := range boundedFleetSettings() {
		t.Run(setting.name, func(t *testing.T) {
			registry := newControlRegistry()
			accepted := setting.get(registry)
			for _, refused := range []int{setting.pastLimit, setting.belowZero} {
				setting.set(registry, refused)
				if got := setting.get(registry); got != accepted {
					t.Fatalf("refused %s %d was stored as %d, want %d",
						setting.name, refused, got, accepted)
				}
			}
			registry.register("crawler")
			for _, directive := range deliveredControls(t, registry, "crawler") {
				if directive.FetchWorkers > yagocrawlcontract.MaximumFetchWorkerConcurrency ||
					directive.MaximumActiveRuns >
						yagocrawlcontract.MaximumActiveCrawlRunConcurrency ||
					directive.ProcessPagesPerSecond >
						yagocrawlcontract.MaximumProcessPagesPerSecond ||
					directive.MaximumRedirects > yagocrawlcontract.MaximumPageRedirects {
					t.Fatalf("convergence delivered a refused %s: %+v",
						setting.name, directive)
				}
			}
		})
	}
}

// TestFleetSettingsAcceptTheirContractMaximum is the accepting half. The ceiling
// is a supported operator choice, so a setter narrowed to exclude it would make
// the documented maximum unreachable while every refusal assertion kept passing.
func TestFleetSettingsAcceptTheirContractMaximum(t *testing.T) {
	for _, setting := range boundedFleetSettings() {
		t.Run(setting.name, func(t *testing.T) {
			registry := newControlRegistry()
			registry.register("crawler")
			deliveredControls(t, registry, "crawler")
			if signalled := setting.set(registry, setting.atMaximum); signalled != 1 {
				t.Fatalf("%s at its contract maximum signalled %d workers, want 1",
					setting.name, signalled)
			}
			if got := setting.get(registry); got != setting.atMaximum {
				t.Fatalf("%s = %d, want its contract maximum %d",
					setting.name, got, setting.atMaximum)
			}
		})
	}
}

func boundedFleetSettings() []boundedFleetSetting {
	return []boundedFleetSetting{
		{
			name:      "maximum-redirects",
			set:       (*ControlRegistry).SetMaximumRedirects,
			get:       (*ControlRegistry).MaximumRedirects,
			atMaximum: yagocrawlcontract.MaximumPageRedirects,
			pastLimit: yagocrawlcontract.MaximumPageRedirects + 1,
			belowZero: -1,
		},
		{
			name:      "active-runs",
			set:       (*ControlRegistry).SetMaximumActiveRuns,
			get:       (*ControlRegistry).MaximumActiveRuns,
			atMaximum: yagocrawlcontract.MaximumActiveCrawlRunConcurrency,
			pastLimit: yagocrawlcontract.MaximumActiveCrawlRunConcurrency + 1,
			belowZero: 0,
		},
		{
			name:      "process-rate",
			set:       (*ControlRegistry).SetProcessPagesPerSecond,
			get:       (*ControlRegistry).ProcessPagesPerSecond,
			atMaximum: yagocrawlcontract.MaximumProcessPagesPerSecond,
			pastLimit: yagocrawlcontract.MaximumProcessPagesPerSecond + 1,
			belowZero: -1,
		},
	}
}
