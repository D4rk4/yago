package adminui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type fakeScheduleSource struct {
	views   []CrawlScheduleView
	created []CrawlScheduleRequest
	deleted []string
	toggled map[string]bool
	err     error
}

func (f *fakeScheduleSource) Schedules(context.Context) []CrawlScheduleView { return f.views }
func (f *fakeScheduleSource) CreateSchedule(_ context.Context, req CrawlScheduleRequest) error {
	f.created = append(f.created, req)

	return f.err
}

func (f *fakeScheduleSource) DeleteSchedule(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)

	return f.err
}

func (f *fakeScheduleSource) SetScheduleEnabled(_ context.Context, id string, enabled bool) error {
	if f.toggled == nil {
		f.toggled = map[string]bool{}
	}
	f.toggled[id] = enabled

	return f.err
}

// TestCrawlSchedulePost pins UI-19's console routes: create parses the form
// into a request, delete and enable/disable carry the ID, a failed create
// re-renders the page with the error, and the route 404s without a source.
func TestCrawlSchedulePost(t *testing.T) {
	source := &fakeScheduleSource{}
	console := New(Options{Schedules: source})

	post := func(form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(
			t.Context(), http.MethodPost, "/admin/crawl/schedule",
			strings.NewReader(form.Encode()),
		)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		console.ServeHTTP(rec, req)

		return rec
	}

	created := post(url.Values{
		"action": {"create"}, "name": {"Docs"}, "seeds": {"https://a.example\nhttps://b.example"},
		"scope": {"domain"}, "maxDepth": {"3"}, "interval": {"24h"},
	})
	if created.Code != http.StatusSeeOther || len(source.created) != 1 ||
		len(source.created[0].Seeds) != 2 || source.created[0].MaxDepth != 3 {
		t.Fatalf("create = %d %+v", created.Code, source.created)
	}

	if rec := post(
		url.Values{"action": {"delete"}, "id": {"docs"}},
	); rec.Code != http.StatusSeeOther ||
		len(source.deleted) != 1 {
		t.Fatalf("delete = %d %v", rec.Code, source.deleted)
	}
	if rec := post(
		url.Values{"action": {"disable"}, "id": {"docs"}},
	); rec.Code != http.StatusSeeOther ||
		source.toggled["docs"] != false {
		t.Fatalf("disable = %d %v", rec.Code, source.toggled)
	}
	if rec := post(url.Values{"action": {"bogus"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus action = %d", rec.Code)
	}

	source.err = errStubControl
	failed := post(url.Values{
		"action": {"create"}, "name": {""}, "seeds": {""}, "interval": {"24h"},
	})
	if failed.Code != http.StatusOK ||
		!strings.Contains(failed.Body.String(), "stub control failure") {
		t.Fatalf("failed create must re-render with the error: %d", failed.Code)
	}

	none := New(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, "/admin/crawl/schedule",
		strings.NewReader("action=create"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	none.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("no source = %d, want 404", rec.Code)
	}
}
