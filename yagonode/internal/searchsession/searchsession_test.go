package searchsession

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

// shufflingSearcher returns a different result set on every invocation, the way
// the live federated fan-out does with its random DHT peer sample.
type shufflingSearcher struct {
	calls  int
	err    error
	limits []int
}

func (s *shufflingSearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	if s.err != nil {
		return searchcore.Response{}, s.err
	}
	s.calls++
	s.limits = append(s.limits, req.Limit)
	results := make([]searchcore.Result, 25)
	for i := range results {
		results[i] = searchcore.Result{
			Title: fmt.Sprintf("call%d-result%d", s.calls, i),
			URL:   "https://a.example/" + strconv.Itoa(s.calls) + "/" + strconv.Itoa(i),
		}
	}

	return searchcore.Response{
		Request:      req,
		TotalResults: len(results),
		Results:      results,
	}, nil
}

func TestStableWindowPagesFromOneSession(t *testing.T) {
	inner := &shufflingSearcher{}
	searcher := WithStableWindow(inner)

	page1, err := searcher.Search(context.Background(), searchcore.Request{
		Query: "go", Terms: []string{"go"}, Offset: 0, Limit: 10,
	})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	page2, err := searcher.Search(context.Background(), searchcore.Request{
		Query: "go", Terms: []string{"go"}, Offset: 10, Limit: 10,
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	page3, err := searcher.Search(context.Background(), searchcore.Request{
		Query: "go", Terms: []string{"go"}, Offset: 20, Limit: 10,
	})
	if err != nil {
		t.Fatalf("page3: %v", err)
	}

	if inner.calls != 1 {
		t.Fatalf("inner searched %d times for three pages, want 1 session", inner.calls)
	}
	if len(inner.limits) != 1 || inner.limits[0] != retrievalDepth(sessionDepth) {
		t.Fatalf(
			"initial candidate window = %v, want %d",
			inner.limits,
			retrievalDepth(sessionDepth),
		)
	}
	if page1.TotalResults != 25 || page2.TotalResults != 25 {
		t.Fatalf("total = %d, want the honest collected count", page1.TotalResults)
	}
	if page1.Results[0].Title != "call1-result0" || page2.Results[0].Title != "call1-result10" {
		t.Fatalf("pages not consistent: %q / %q", page1.Results[0].Title, page2.Results[0].Title)
	}
	if len(page3.Results) != 5 {
		t.Fatalf("page3 = %d results, want the 5 remaining", len(page3.Results))
	}
	if window, err := searcher.Search(context.Background(), searchcore.Request{
		Query: "go", Terms: []string{"go"}, Offset: 30, Limit: 10,
	}); err != nil || len(window.Results) != 0 {
		t.Fatalf("beyond the session = %d results, want 0", len(window.Results))
	}
}

func TestStableWindowResponseDoesNotAliasSession(t *testing.T) {
	inner := &shufflingSearcher{}
	searcher := WithStableWindow(inner)
	ctx := context.Background()
	first, err := searcher.Search(ctx, searchcore.Request{Query: "go", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	first.Results[1].Title = "changed"

	cached, err := searcher.Search(ctx, searchcore.Request{Query: "go", Offset: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cached.Results[0].Title != "call1-result1" {
		t.Fatalf("cached result changed through response alias: %q", cached.Results[0].Title)
	}
}

func TestStableWindowDeepLinkWithoutSessionSearchesOnce(t *testing.T) {
	inner := &shufflingSearcher{}
	searcher := WithStableWindow(inner)

	// A deep link straight to page two of a never-run query builds the session.
	page2, err := searcher.Search(context.Background(), searchcore.Request{
		Query: "go", Offset: 10, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 || page2.Results[0].Title != "call1-result10" {
		t.Fatalf("deep link: calls=%d first=%q", inner.calls, page2.Results[0].Title)
	}

	// A zero limit falls back to the public default page size.
	deflt, err := searcher.Search(context.Background(), searchcore.Request{
		Query: "go", Offset: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(deflt.Results) != searchcore.DefaultPublicLimit {
		t.Fatalf("default window = %d, want %d", len(deflt.Results), searchcore.DefaultPublicLimit)
	}
}

func TestStableWindowFreshSearchOnPageOne(t *testing.T) {
	inner := &shufflingSearcher{}
	searcher := WithStableWindow(inner)
	ctx := context.Background()

	if _, err := searcher.Search(ctx, searchcore.Request{Query: "go", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	second, err := searcher.Search(ctx, searchcore.Request{Query: "go", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if inner.calls != 2 {
		t.Fatalf("repeated page one searched %d times, want a fresh search each time", inner.calls)
	}
	if second.Results[0].Title != "call2-result0" {
		t.Fatalf("page one served stale session: %q", second.Results[0].Title)
	}

	// The refreshed session serves the deep pages.
	deep, err := searcher.Search(ctx, searchcore.Request{Query: "go", Offset: 10, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if deep.Results[0].Title != "call2-result10" {
		t.Fatalf("deep page not from the refreshed session: %q", deep.Results[0].Title)
	}
}

func TestStableWindowExpiryAndErrors(t *testing.T) {
	base := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	current := base
	oldClock := clock
	t.Cleanup(func() { clock = oldClock })
	clock = func() time.Time { return current }

	inner := &shufflingSearcher{}
	searcher := WithStableWindow(inner)
	ctx := context.Background()

	if _, err := searcher.Search(ctx, searchcore.Request{Query: "go", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	current = base.Add(sessionTTL + time.Minute)
	if _, err := searcher.Search(ctx, searchcore.Request{
		Query: "go", Offset: 10, Limit: 10,
	}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 2 {
		t.Fatalf("expired session not refreshed: %d calls", inner.calls)
	}

	failing := WithStableWindow(&shufflingSearcher{err: errors.New("boom")})
	if _, err := failing.Search(ctx, searchcore.Request{Query: "go", Limit: 10}); err == nil {
		t.Fatal("inner error must surface")
	}
}

func TestStableWindowBoundsSessions(t *testing.T) {
	inner := &shufflingSearcher{}
	searcher := WithStableWindow(inner)
	ctx := context.Background()
	for i := 0; i < maxSessions+10; i++ {
		if _, err := searcher.Search(ctx, searchcore.Request{
			Query: "q" + strconv.Itoa(i), Limit: 10,
		}); err != nil {
			t.Fatal(err)
		}
	}
	stable, ok := searcher.(*stableSearcher)
	if !ok {
		t.Fatal("unexpected searcher type")
	}
	if len(stable.sessions) > maxSessions {
		t.Fatalf("sessions = %d, want bounded to %d", len(stable.sessions), maxSessions)
	}
}

func TestSessionKeySeparatesDifferentQueries(t *testing.T) {
	base := searchcore.Request{Query: "go", Source: searchcore.SourceGlobal}
	same := sessionKey(base)
	if sessionKey(searchcore.Request{Query: "go", Source: searchcore.SourceLocal}) == same {
		t.Fatal("source must separate sessions")
	}
	if sessionKey(searchcore.Request{Query: "rust", Source: searchcore.SourceGlobal}) == same {
		t.Fatal("query must separate sessions")
	}
	paged := base
	paged.Offset, paged.Limit = 20, 10
	if sessionKey(paged) != same {
		t.Fatal("paging fields must not separate sessions")
	}
}

func TestSessionKeyCoversEveryResultAffectingRequestField(t *testing.T) {
	requestType := reflect.TypeOf(searchcore.Request{})
	base := reflect.New(requestType).Elem()
	baseKey := sessionKey(base.Interface().(searchcore.Request))
	for index := 0; index < requestType.NumField(); index++ {
		fieldType := requestType.Field(index)
		if fieldType.Name == "Offset" || fieldType.Name == "Limit" {
			continue
		}
		changed := reflect.New(requestType).Elem()
		field := changed.Field(index)
		switch {
		case field.Type() == reflect.TypeOf(time.Time{}):
			field.Set(reflect.ValueOf(time.Unix(1, 0).UTC()))
		case field.Kind() == reflect.String:
			field.SetString("changed")
		case field.Kind() == reflect.Bool:
			field.SetBool(true)
		case field.Kind() == reflect.Slice:
			field.Set(reflect.ValueOf([]string{"changed"}))
		default:
			t.Fatalf("unsupported request field %s with type %s", fieldType.Name, fieldType.Type)
		}
		if sessionKey(changed.Interface().(searchcore.Request)) == baseKey {
			t.Fatalf("request field %s does not separate sessions", fieldType.Name)
		}
	}
}

func TestStableWindowDoesNotReuseDifferentPolicySession(t *testing.T) {
	inner := &shufflingSearcher{}
	searcher := WithStableWindow(inner)
	ctx := context.Background()

	if _, err := searcher.Search(ctx, searchcore.Request{
		Query: "go", Verify: searchcore.VerifyFalse,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := searcher.Search(ctx, searchcore.Request{
		Query: "go", Offset: 10, Verify: searchcore.VerifyIfExist, SafeSearch: true,
	}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 2 {
		t.Fatalf("policy-changing deep page reused a session: calls=%d", inner.calls)
	}
}
