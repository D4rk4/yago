package documentstore

import "testing"

func TestInboundAnchorRefreshStoresPageWithinMutationRowLimit(t *testing.T) {
	for _, page := range []struct {
		targets      int
		transactions int
	}{
		{targets: outboundAnchorTargetPageSize, transactions: 2},
		{targets: outboundAnchorMutationMaximumRows + 1, transactions: 4},
	} {
		_, receiver, engine := openPagedDocuments(t)
		targets := outboundAnchorReplacementTargets(page.targets)
		stored := make([]Document, 0, len(targets))
		for _, target := range targets {
			stored = append(stored, Document{NormalizedURL: target})
		}
		if _, err := receiver.Receive(t.Context(), stored); err != nil {
			t.Fatal(err)
		}
		if _, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
			t.Context(),
			[]OutboundAnchorSet{{
				SourceURL: "https://source.example/refresh-rows",
				Anchors:   outboundAnchorReplacementEdges(targets),
			}},
		); err != nil {
			t.Fatal(err)
		}
		transactions := 0
		engine.beforeUpdate = func() { transactions++ }
		receipt, err := receiver.(InboundAnchorDocumentRefresher).RefreshInboundAnchorDocuments(
			t.Context(),
			page.targets,
			func([]Document) error { return nil },
		)
		if err != nil || receipt.More || receipt.Refreshed != page.targets {
			t.Fatalf("%d-target refresh = %#v/%v", page.targets, receipt, err)
		}
		if transactions != page.transactions {
			t.Fatalf(
				"%d-target refresh storage transactions = %d, want %d",
				page.targets,
				transactions,
				page.transactions,
			)
		}
	}
}
