package publicportal

import "testing"

func TestNumberedPagesWindow(t *testing.T) {
	pages := numberedPages("go", 1, 250) // 25 pages of 10
	if len(pages) != pagerWindow || pages[0].Number != 1 || pages[9].Number != 10 {
		t.Fatalf("first-page window = %+v", pages)
	}
	if !pages[0].Current || pages[1].Current {
		t.Fatal("current page not marked")
	}

	middle := numberedPages("go", 20, 250)
	if middle[0].Number != 15 || middle[len(middle)-1].Number != 24 {
		t.Fatalf("middle window = %d..%d", middle[0].Number, middle[len(middle)-1].Number)
	}

	tail := numberedPages("go", 25, 250)
	if tail[len(tail)-1].Number != 25 || tail[0].Number != 16 {
		t.Fatalf("tail window = %d..%d", tail[0].Number, tail[len(tail)-1].Number)
	}

	if got := numberedPages("go", 1, 10); got != nil {
		t.Fatalf("single page must render no numbers: %+v", got)
	}

	// The window never promises pages past the portal cap.
	capped := numberedPages("go", portalMaxPage, portalMaxPage*portalPageSize*2)
	if capped[len(capped)-1].Number != portalMaxPage {
		t.Fatalf("cap ignored: %+v", capped[len(capped)-1])
	}
}
