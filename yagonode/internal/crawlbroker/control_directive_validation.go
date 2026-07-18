package crawlbroker

import (
	"encoding/hex"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func validControlDirective(directive yagocrawlcontract.CrawlControlDirective) bool {
	switch directive.Kind {
	case yagocrawlcontract.CrawlControlPause,
		yagocrawlcontract.CrawlControlResume,
		yagocrawlcontract.CrawlControlCancel,
		yagocrawlcontract.CrawlControlSetRate:
		if directive.RunID == "" {
			return false
		}
		_, err := hex.DecodeString(directive.RunID)

		return err == nil
	case yagocrawlcontract.CrawlControlRestart:
		return directive.RunID == ""
	case yagocrawlcontract.CrawlControlSetWorkers:
		return directive.RunID == "" && directive.FetchWorkers >= 1 &&
			directive.FetchWorkers <= yagocrawlcontract.MaximumFetchWorkerConcurrency
	case yagocrawlcontract.CrawlControlSetActiveRuns:
		return directive.RunID == "" && directive.MaximumActiveRuns >= 1 &&
			directive.MaximumActiveRuns <= yagocrawlcontract.MaximumActiveCrawlRunConcurrency
	case yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority:
		return directive.RunID == ""
	default:
		return false
	}
}
