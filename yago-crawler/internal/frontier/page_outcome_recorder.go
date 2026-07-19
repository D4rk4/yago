package frontier

import (
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type pageOutcomeRecorder interface {
	CommitPage([]byte, string, yagocrawlcontract.CrawlRunTally, string)
}

type pageOutcomeEvidenceRecorder interface {
	CommitPageWithStatus([]byte, string, yagocrawlcontract.CrawlRunTally, uint32, string)
}

func recordPageOutcome(
	tally RunTally,
	work crawljob.CrawlJob,
	outcome yagocrawlcontract.CrawlRunTally,
	httpStatus uint32,
	reason string,
) {
	evidenceRecorder, ok := tally.(pageOutcomeEvidenceRecorder)
	if ok {
		evidenceRecorder.CommitPageWithStatus(
			work.Provenance,
			work.URL,
			outcome,
			httpStatus,
			reason,
		)

		return
	}
	recorder, ok := tally.(pageOutcomeRecorder)
	if ok {
		recorder.CommitPage(work.Provenance, work.URL, outcome, reason)
	}
}
