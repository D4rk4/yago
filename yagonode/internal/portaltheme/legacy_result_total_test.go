package portaltheme

import (
	"strings"
	"testing"
)

func TestDefaultResultsDescribeTheAccessibleSearchWindow(t *testing.T) {
	t.Parallel()

	body := DefaultBody(PageResults)
	if !strings.Contains(body, currentSearchWindowFragment) ||
		strings.Contains(body, legacyResultTotalFragment) {
		t.Fatalf("default result total text = %q", body)
	}
}

func TestRepairLegacyResultTotalPreservesSurroundingDesign(t *testing.T) {
	t.Parallel()

	body := "<header>custom</header>" + legacyResultTotalFragment + "<footer>custom</footer>"
	repaired := repairLegacyPortalDocument(PageResults, body)
	if !strings.Contains(repaired, currentResultTotalFragment) ||
		strings.Contains(repaired, legacyResultTotalFragment) ||
		!strings.HasPrefix(repaired, "<header>custom</header>") ||
		!strings.HasSuffix(repaired, "<footer>custom</footer>") {
		t.Fatalf("repaired result total text = %q", repaired)
	}
}
