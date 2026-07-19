package yagonode

import (
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/remotecrawl"
)

const remoteCrawlEventName = "crawl.remote.decision"

type remoteCrawlEventObserver struct {
	recorder *events.Recorder
}

func (o remoteCrawlEventObserver) ObserveRemoteCrawl(observation remotecrawl.Observation) {
	if o.recorder == nil || !remoteCrawlDurableEventOutcome(observation.Outcome) {
		return
	}
	o.recorder.Record(
		events.SeverityWarn,
		events.CategoryCrawl,
		remoteCrawlEventName,
		fmt.Sprintf(
			"remote crawl %s %s for %d item(s)",
			observation.Action,
			observation.Outcome,
			observation.Count,
		),
	)
}

func remoteCrawlDurableEventOutcome(outcome string) bool {
	return outcome == "untrusted" || outcome == "store_requeued" ||
		strings.HasSuffix(outcome, "_rejected") ||
		strings.HasSuffix(outcome, "_limited") ||
		strings.HasSuffix(outcome, "_failed")
}
