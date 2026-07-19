package adminui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakeExtractionRecrawlSource struct {
	result       ExtractionRecrawlResult
	err          error
	continuation string
	actionID     string
	limit        int
	calls        int
}

func (source *fakeExtractionRecrawlSource) QueueOutdatedExtractions(
	_ context.Context,
	actionID string,
	continuation string,
	limit int,
) (ExtractionRecrawlResult, error) {
	source.calls++
	source.actionID = actionID
	source.continuation = continuation
	source.limit = limit

	return source.result, source.err
}

var extractionRecrawlTestActionID = strings.Repeat("A", 26)

func TestConsoleIndexRendersBoundedExtractionRecrawlControl(t *testing.T) {
	t.Parallel()

	body := do(t, New(Options{
		Index:             fakeIndex{snap: IndexStats{Available: true}},
		ExtractionRecrawl: &fakeExtractionRecrawlSource{},
	}), indexPath).body
	for _, want := range []string{
		"Refresh outdated extraction",
		`action="/admin/index/recrawl-extraction"`,
		`name="limit" min="1" max="100" value="20"`,
		`name="action_id" value="`,
		"Each action examines only the explicit bounded number",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("extraction recrawl control missing %q in %s", want, body)
		}
	}
}

func TestConsoleExtractionRecrawlReportsPartialAndCompleteProgress(t *testing.T) {
	t.Parallel()

	source := &fakeExtractionRecrawlSource{result: ExtractionRecrawlResult{
		Limit:             20,
		CurrentGeneration: 1,
		ActionID:          extractionRecrawlTestActionID,
		Examined:          20,
		Visible:           19,
		CurrentOrNewer:    12,
		Outdated:          7,
		Queued:            7,
		Continuation:      "next-position",
		Partial:           true,
	}}
	console := New(Options{
		Index:             fakeIndex{snap: IndexStats{Available: true}},
		ExtractionRecrawl: source,
	})
	partial := doPost(t, console, extractionRecrawlPath, url.Values{
		"action_id":    {extractionRecrawlTestActionID},
		"limit":        {"20"},
		"continuation": {"start-position"},
	})
	if partial.status != http.StatusOK || source.calls != 1 || source.limit != 20 ||
		source.continuation != "start-position" ||
		source.actionID != extractionRecrawlTestActionID {
		t.Fatalf("partial status/call = %d/%d, request = %q/%d",
			partial.status, source.calls, source.continuation, source.limit)
	}
	for _, want := range []string{
		"Examined 20 storage record(s)",
		"evaluated 19 visible document(s)",
		"found 7 outdated document(s)",
		"queued 7 URL(s)",
		`name="continuation" value="next-position"`,
		`name="action_id" value="` + extractionRecrawlTestActionID + `"`,
		"Continue refresh",
	} {
		if !strings.Contains(partial.body, want) {
			t.Fatalf("partial result missing %q in %s", want, partial.body)
		}
	}

	source.result = ExtractionRecrawlResult{
		Limit: 20, CurrentGeneration: 1, Examined: 3, Visible: 3, CurrentOrNewer: 3,
	}
	complete := doPost(t, console, extractionRecrawlPath, url.Values{
		"action_id": {extractionRecrawlTestActionID},
		"limit":     {"20"},
	})
	if !strings.Contains(complete.body, "bounded scan reached its captured end position") ||
		!strings.Contains(complete.body, "Start bounded refresh") {
		t.Fatalf("complete result = %s", complete.body)
	}
}

func TestConsoleExtractionRecrawlRejectsInvalidLimitAndHidesFailure(t *testing.T) {
	t.Parallel()

	for _, limit := range []string{"", "0", "101", "many"} {
		source := &fakeExtractionRecrawlSource{}
		got := doPost(t, New(Options{ExtractionRecrawl: source}), extractionRecrawlPath,
			url.Values{
				"action_id": {extractionRecrawlTestActionID},
				"limit":     {limit},
			})
		if got.status != http.StatusBadRequest || source.calls != 0 {
			t.Fatalf("limit %q status/calls = %d/%d", limit, got.status, source.calls)
		}
	}
	for _, actionID := range []string{"invalid", strings.Repeat("A", 25) + "0"} {
		invalidAction := &fakeExtractionRecrawlSource{}
		got := doPost(t, New(Options{ExtractionRecrawl: invalidAction}), extractionRecrawlPath,
			url.Values{"action_id": {actionID}, "limit": {"20"}})
		if got.status != http.StatusBadRequest || invalidAction.calls != 0 {
			t.Fatalf("action %q status/calls = %d/%d", actionID, got.status, invalidAction.calls)
		}
	}

	source := &fakeExtractionRecrawlSource{
		result: ExtractionRecrawlResult{
			Limit: 10, CurrentGeneration: 1, Examined: 10, Visible: 10,
			Outdated: 4, ActionID: extractionRecrawlTestActionID,
			Continuation: "same-position", Partial: true, Retry: true,
		},
		err: errors.New("private queue detail"),
	}
	failed := doPost(t, New(Options{
		Index:             fakeIndex{snap: IndexStats{Available: true}},
		ExtractionRecrawl: source,
	}), extractionRecrawlPath, url.Values{
		"action_id": {extractionRecrawlTestActionID},
		"limit":     {"10"},
	})
	if failed.status != http.StatusOK ||
		!strings.Contains(failed.body, "did not complete") ||
		!strings.Contains(failed.body, "Queue acceptance was not confirmed") ||
		!strings.Contains(failed.body, "Retry bounded batch") ||
		strings.Contains(failed.body, "private queue detail") {
		t.Fatalf("failed result = status %d body %s", failed.status, failed.body)
	}

	missing := doPost(t, New(Options{}), extractionRecrawlPath, url.Values{
		"action_id": {extractionRecrawlTestActionID},
		"limit":     {"20"},
	})
	if missing.status != http.StatusNotFound {
		t.Fatalf("missing source status = %d", missing.status)
	}
}

func TestExtractionRecrawlViewPreservesOnlyAnActivePassAction(t *testing.T) {
	t.Parallel()

	fresh := newExtractionRecrawlView(true, nil, "")
	if !validExtractionRecrawlActionID(fresh.ActionID) {
		t.Fatalf("fresh action = %q", fresh.ActionID)
	}
	partial := newExtractionRecrawlView(true, &ExtractionRecrawlResult{
		ActionID: extractionRecrawlTestActionID,
		Partial:  true,
	}, "")
	if partial.ActionID != extractionRecrawlTestActionID {
		t.Fatalf("partial action = %q", partial.ActionID)
	}
	complete := newExtractionRecrawlView(true, &ExtractionRecrawlResult{
		ActionID: extractionRecrawlTestActionID,
	}, "")
	if !validExtractionRecrawlActionID(complete.ActionID) ||
		complete.ActionID == extractionRecrawlTestActionID {
		t.Fatalf("complete action = %q", complete.ActionID)
	}
}
