package crawlbroker

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestControlDirectiveLedgersDeliverBoundedBacklogPages(t *testing.T) {
	persistent, err := newPersistentControlDirectiveLedger(memQueue(t).vault)
	if err != nil {
		t.Fatalf("open persistent directive ledger: %v", err)
	}
	ledgers := []struct {
		name   string
		ledger controlDirectiveLedger
	}{
		{name: "memory", ledger: newMemoryControlDirectiveLedger()},
		{name: "persistent", ledger: persistent},
	}
	for _, test := range ledgers {
		t.Run(test.name, func(t *testing.T) {
			assertBoundedDirectiveBacklog(t, test.ledger)
		})
	}
}

func assertBoundedDirectiveBacklog(t *testing.T, ledger controlDirectiveLedger) {
	t.Helper()
	const remainingDirectiveCount = 17
	total := yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments + remainingDirectiveCount
	for range total {
		if _, err := ledger.Enqueue(
			t.Context(),
			"worker",
			yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlRestart},
		); err != nil {
			t.Fatalf("enqueue directive: %v", err)
		}
	}
	first, err := ledger.Exchange(t.Context(), "worker", nil)
	if err != nil {
		t.Fatalf("read first directive page: %v", err)
	}
	if len(first) != yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments {
		t.Fatalf("first directive page = %d", len(first))
	}
	firstAcknowledgments := directiveIdentities(t, first, 1)
	second, err := ledger.Exchange(t.Context(), "worker", firstAcknowledgments)
	if err != nil {
		t.Fatalf("acknowledge first directive page: %v", err)
	}
	if len(second) != remainingDirectiveCount ||
		second[0].DirectiveID != firstAcknowledgments[len(firstAcknowledgments)-1]+1 ||
		second[len(second)-1].DirectiveID !=
			firstAcknowledgments[len(firstAcknowledgments)-1]+remainingDirectiveCount {
		t.Fatalf("second directive page = %+v", second)
	}
	secondAcknowledgments := directiveIdentities(
		t,
		second,
		firstAcknowledgments[len(firstAcknowledgments)-1]+1,
	)
	remaining, err := ledger.Exchange(t.Context(), "worker", secondAcknowledgments)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("remaining directives = %+v, err=%v", remaining, err)
	}
}

func directiveIdentities(
	t *testing.T,
	directives []yagocrawlcontract.CrawlControlDirective,
	first uint64,
) []uint64 {
	t.Helper()
	identities := make([]uint64, len(directives))
	want := first
	for index, directive := range directives {
		identities[index] = directive.DirectiveID
		if directive.DirectiveID != want {
			t.Fatalf("directive identity %d = %d", index, directive.DirectiveID)
		}
		want++
	}

	return identities
}
