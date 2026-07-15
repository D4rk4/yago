package adminui

import (
	"strings"
	"testing"
)

func TestConsoleSearchDoesNotClaimZeroAfterPartialFailure(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Search: fakeSearch{results: SearchResults{
		Query: "go", Global: true, Failures: []string{"peer federation unavailable"},
	}}}), "/admin/search?q=go")
	if !strings.Contains(got.body, "No complete result set is available") ||
		!strings.Contains(got.body, "one or more enabled sources failed") {
		t.Fatalf("partial empty search lacks an explicit incomplete state: %s", got.body)
	}
	if strings.Contains(got.body, "0 result(s)") ||
		strings.Contains(got.body, ">No results.<") {
		t.Fatalf("partial empty search claims a complete zero: %s", got.body)
	}
}
