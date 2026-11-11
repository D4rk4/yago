package nodeexchange

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestTallyToProtoPreservesFetchedPageOutcomes(t *testing.T) {
	got := tallyToProto(yagocrawlcontract.CrawlRunTally{
		Fetched: 8, Indexed: 4, Pending: 2, DocumentCandidates: 5,
		RemovalMutations: 1, NotIndexable: 1, NodeRejected: 1, PendingIngest: 1,
		OutcomeCounts: []yagocrawlcontract.CrawlOutcomeCount{
			{Class: "not_indexable", Reason: "sitemap", Pages: 1},
			{Class: "node_rejected", Reason: "too-few-words", Pages: 1},
		},
	})
	if got.GetFetched() != 8 || got.GetIndexed() != 4 || got.GetPending() != 2 ||
		got.GetDocumentCandidates() != 5 || got.GetRemovalMutations() != 1 ||
		got.GetNotIndexable() != 1 || got.GetNodeRejected() != 1 || got.GetPendingIngest() != 1 {
		t.Fatalf("tally fields = %+v", got)
	}
	if len(got.GetOutcomeCounts()) != 2 || got.GetOutcomeCounts()[0].GetReason() != "sitemap" ||
		got.GetOutcomeCounts()[1].GetReason() != "too-few-words" {
		t.Fatalf("outcome counts = %+v", got.GetOutcomeCounts())
	}
}
