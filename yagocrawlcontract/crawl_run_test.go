package yagocrawlcontract

import "testing"

func TestCrawlRunTallyAcceptsLegacyAndClassifiedSnapshots(t *testing.T) {
	legacy := CrawlRunTally{Fetched: 103321, Indexed: 16237}
	if !legacy.Valid() {
		t.Fatal("legacy tally with unknown fetched outcomes was rejected")
	}
	classified := CrawlRunTally{
		Fetched: 8, Indexed: 4, DocumentCandidates: 5, RemovalMutations: 1,
		NotIndexable: 1, NodeRejected: 1, PendingIngest: 1,
		OutcomeCounts: []CrawlOutcomeCount{
			{Class: CrawlOutcomeNotIndexable, Reason: CrawlOutcomeSitemap, Pages: 1},
			{Class: CrawlOutcomeNodeRejected, Reason: "too-few-words", Pages: 1},
		},
	}
	if !classified.Valid() {
		t.Fatal("classified tally with a valid remainder was rejected")
	}
}

func TestCrawlRunTallyRejectsUnboundedOrInconsistentOutcomes(t *testing.T) {
	valid := CrawlRunTally{
		Fetched: 3, Indexed: 1, NotIndexable: 1, NodeRejected: 1,
		OutcomeCounts: []CrawlOutcomeCount{
			{Class: CrawlOutcomeNotIndexable, Reason: CrawlOutcomeEmptyContent, Pages: 1},
			{Class: CrawlOutcomeNodeRejected, Reason: "too-few-words", Pages: 1},
		},
	}
	cases := []struct {
		name   string
		mutate func(*CrawlRunTally)
	}{
		{
			name: "unbounded class and reason",
			mutate: func(tally *CrawlRunTally) {
				tally.OutcomeCounts[0] = CrawlOutcomeCount{Class: "url:" + string(make([]byte, 256)), Reason: "host", Pages: 1}
			},
		},
		{
			name:   "reason totals do not match classes",
			mutate: func(tally *CrawlRunTally) { tally.NotIndexable++ },
		},
		{
			name: "duplicate reason label",
			mutate: func(tally *CrawlRunTally) {
				tally.OutcomeCounts = append(tally.OutcomeCounts, tally.OutcomeCounts[0])
				tally.NotIndexable++
			},
		},
		{
			name:   "classified pages exceed fetched total",
			mutate: func(tally *CrawlRunTally) { tally.PendingIngest = 1 },
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := valid
			candidate.OutcomeCounts = append([]CrawlOutcomeCount(nil), valid.OutcomeCounts...)
			testCase.mutate(&candidate)
			if candidate.Valid() {
				t.Fatal("invalid crawler outcome tally was accepted")
			}
		})
	}
}
