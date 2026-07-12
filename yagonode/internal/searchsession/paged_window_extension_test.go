package searchsession

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type expandingSearcher struct {
	mu        sync.Mutex
	calls     int
	limits    []int
	total     int
	available int
	failAfter int
}

func (s *expandingSearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.limits = append(s.limits, req.Limit)
	if s.failAfter > 0 && s.calls >= s.failAfter {
		return searchcore.Response{}, errors.New("extension failed")
	}
	length := min(req.Limit, s.available)
	results := make([]searchcore.Result, length)
	for index := range results {
		results[index] = searchcore.Result{
			Title:   fmt.Sprintf("result-%d", index),
			URL:     fmt.Sprintf("https://example.test/%d", index),
			URLHash: fmt.Sprintf("hash-%d", index),
		}
	}

	return searchcore.Response{
		Request:      req,
		TotalResults: s.total,
		Results:      results,
	}, nil
}

func TestStableWindowExtendsInBlocksAndReusesExpandedPrefix(t *testing.T) {
	inner := &expandingSearcher{total: maxSessionDepth, available: maxSessionDepth}
	searcher := WithStableWindow(inner)
	ctx := context.Background()

	page1, err := searcher.Search(ctx, searchcore.Request{Query: "go", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page1.TotalResults != maxSessionDepth || page1.Results[0].Title != "result-0" {
		t.Fatalf("page one = total %d, first %q", page1.TotalResults, page1.Results[0].Title)
	}

	page6, err := searcher.Search(ctx, searchcore.Request{Query: "go", Offset: 50, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page6.Results[0].Title != "result-50" {
		t.Fatalf("page six first = %q", page6.Results[0].Title)
	}

	page7, err := searcher.Search(ctx, searchcore.Request{Query: "go", Offset: 60, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page7.Results[0].Title != "result-60" {
		t.Fatalf("page seven first = %q", page7.Results[0].Title)
	}
	if !reflect.DeepEqual(inner.limits, []int{sessionDepth, 100}) {
		t.Fatalf("search depths after page seven = %v", inner.limits)
	}

	page50, err := searcher.Search(ctx, searchcore.Request{Query: "go", Offset: 490, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page50.Results[0].Title != "result-490" || page50.TotalResults != maxSessionDepth {
		t.Fatalf("page fifty = total %d, first %q", page50.TotalResults, page50.Results[0].Title)
	}
	if !reflect.DeepEqual(inner.limits, []int{sessionDepth, 100, maxSessionDepth}) {
		t.Fatalf("search depths after page fifty = %v", inner.limits)
	}
}

func TestStableWindowKeepsTotalAfterProgressingShortExtension(t *testing.T) {
	inner := &expandingSearcher{total: maxSessionDepth, available: 75}
	searcher := WithStableWindow(inner)
	ctx := context.Background()

	first, err := searcher.Search(ctx, searchcore.Request{Query: "go", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if first.TotalResults != maxSessionDepth {
		t.Fatalf("initial total = %d", first.TotalResults)
	}
	page6, err := searcher.Search(ctx, searchcore.Request{Query: "go", Offset: 50, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page6.TotalResults != maxSessionDepth || page6.Results[0].Title != "result-50" {
		t.Fatalf("short extension = total %d, first %q", page6.TotalResults, page6.Results[0].Title)
	}
	page8, err := searcher.Search(ctx, searchcore.Request{Query: "go", Offset: 70, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page8.TotalResults != 75 || len(page8.Results) != 5 || len(inner.limits) != 3 {
		t.Fatalf(
			"last page = total %d, rows %d, depths %v",
			page8.TotalResults,
			len(page8.Results),
			inner.limits,
		)
	}
}

type progressivelyDeduplicatedSearcher struct {
	limits []int
}

func (s *progressivelyDeduplicatedSearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.limits = append(s.limits, req.Limit)
	length := min(120, req.Limit/2)
	results := make([]searchcore.Result, length)
	for index := range results {
		results[index] = searchcore.Result{
			Title:   fmt.Sprintf("result-%d", index),
			URL:     fmt.Sprintf("https://example.test/%d", index),
			URLHash: fmt.Sprintf("hash-%d", index),
		}
	}

	return searchcore.Response{
		Request:      req,
		TotalResults: 120,
		Results:      results,
	}, nil
}

func TestStableWindowExtendsPastShortDeduplicatedWindows(t *testing.T) {
	inner := &progressivelyDeduplicatedSearcher{}
	searcher := WithStableWindow(inner)
	ctx := context.Background()

	first, err := searcher.Search(ctx, searchcore.Request{Query: "go", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if first.TotalResults != 120 || first.Results[0].Title != "result-0" {
		t.Fatalf("initial page = total %d, first %q", first.TotalResults, first.Results[0].Title)
	}
	page4, err := searcher.Search(ctx, searchcore.Request{Query: "go", Offset: 30, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page4.TotalResults != 120 || page4.Results[0].Title != "result-30" ||
		!reflect.DeepEqual(inner.limits, []int{sessionDepth, 100}) {
		t.Fatalf(
			"page four = total %d, first %q, depths %v",
			page4.TotalResults,
			page4.Results[0].Title,
			inner.limits,
		)
	}
	page11, err := searcher.Search(ctx, searchcore.Request{Query: "go", Offset: 100, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page11.TotalResults != 120 || page11.Results[0].Title != "result-100" ||
		!reflect.DeepEqual(inner.limits, []int{sessionDepth, 100, 150, 200, 250}) {
		t.Fatalf(
			"page eleven = total %d, first %q, depths %v",
			page11.TotalResults,
			page11.Results[0].Title,
			inner.limits,
		)
	}

	directInner := &progressivelyDeduplicatedSearcher{}
	directSearcher := WithStableWindow(directInner)
	directPage11, err := directSearcher.Search(ctx, searchcore.Request{
		Query: "go", Offset: 100, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if directPage11.TotalResults != 120 || directPage11.Results[0].Title != "result-100" ||
		!reflect.DeepEqual(directInner.limits, []int{150, 200, 250}) {
		t.Fatalf(
			"direct page eleven = total %d, first %q, depths %v",
			directPage11.TotalResults,
			directPage11.Results[0].Title,
			directInner.limits,
		)
	}
}

type duplicateWindowSearcher struct {
	calls int
}

func (s *duplicateWindowSearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.calls++
	results := make([]searchcore.Result, req.Limit)
	for index := range results {
		identity := index % sessionDepth
		results[index] = searchcore.Result{
			Title: fmt.Sprintf("result-%d", identity),
			URL:   fmt.Sprintf("https://example.test/%d", identity),
		}
	}

	return searchcore.Response{
		Request:      req,
		TotalResults: maxSessionDepth,
		Results:      results,
	}, nil
}

func TestStableWindowCollapsesTotalWhenRefreshAddsNothing(t *testing.T) {
	inner := &duplicateWindowSearcher{}
	searcher := WithStableWindow(inner)
	ctx := context.Background()

	if _, err := searcher.Search(ctx, searchcore.Request{Query: "go", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	page6, err := searcher.Search(ctx, searchcore.Request{Query: "go", Offset: 50, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page6.TotalResults != sessionDepth || len(page6.Results) != 0 || inner.calls != 2 {
		t.Fatalf(
			"duplicate refresh = total %d, rows %d, calls %d",
			page6.TotalResults,
			len(page6.Results),
			inner.calls,
		)
	}
	if _, err := searcher.Search(
		ctx,
		searchcore.Request{Query: "go", Offset: 50, Limit: 10},
	); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 2 {
		t.Fatalf("collapsed session searched again: %d", inner.calls)
	}
}

func TestStableWindowKeepsCachedPrefixWhenRefreshReorders(t *testing.T) {
	inner := &reorderingSearcher{}
	searcher := WithStableWindow(inner)
	ctx := context.Background()

	if _, err := searcher.Search(ctx, searchcore.Request{Query: "go", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := searcher.Search(
		ctx,
		searchcore.Request{Query: "go", Offset: 50, Limit: 10},
	); err != nil {
		t.Fatal(err)
	}
	page2, err := searcher.Search(ctx, searchcore.Request{Query: "go", Offset: 10, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page2.Results[0].Title != "call-1-result-10" {
		t.Fatalf("cached prefix shifted: %q", page2.Results[0].Title)
	}
}

type reorderingSearcher struct {
	calls int
}

func (s *reorderingSearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.calls++
	results := make([]searchcore.Result, req.Limit)
	for index := range results {
		identity := index
		if s.calls > 1 {
			identity = req.Limit - index
		}
		results[index] = searchcore.Result{
			Title: fmt.Sprintf("call-%d-result-%d", s.calls, identity),
			URL:   fmt.Sprintf("https://example.test/%d", identity),
		}
	}

	return searchcore.Response{
		Request:      req,
		TotalResults: maxSessionDepth,
		Results:      results,
	}, nil
}

func TestStableWindowSurfacesExtensionFailureAndRetries(t *testing.T) {
	inner := &expandingSearcher{
		total: maxSessionDepth, available: maxSessionDepth, failAfter: 2,
	}
	searcher := WithStableWindow(inner)
	ctx := context.Background()

	if _, err := searcher.Search(ctx, searchcore.Request{Query: "go", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := searcher.Search(ctx, searchcore.Request{
		Query: "go", Offset: 50, Limit: 10,
	}); err == nil {
		t.Fatal("extension failure must surface")
	}
	if _, err := searcher.Search(ctx, searchcore.Request{
		Query: "go", Offset: 50, Limit: 10,
	}); err == nil || inner.calls != 3 {
		t.Fatalf("extension retry = calls %d, error %v", inner.calls, err)
	}

	directInner := &expandingSearcher{
		total: maxSessionDepth, available: 75, failAfter: 2,
	}
	directSearcher := WithStableWindow(directInner)
	if _, err := directSearcher.Search(ctx, searchcore.Request{
		Query: "go", Offset: 80, Limit: 10,
	}); err == nil || directInner.calls != 2 {
		t.Fatalf("new-session extension = calls %d, error %v", directInner.calls, err)
	}
}

func TestStableWindowBoundsDirectDeepLink(t *testing.T) {
	inner := &expandingSearcher{total: maxSessionDepth * 2, available: maxSessionDepth}
	searcher := WithStableWindow(inner)

	resp, err := searcher.Search(context.Background(), searchcore.Request{
		Query: "go", Offset: maxSessionDepth + 100, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 0 || resp.TotalResults != maxSessionDepth ||
		!reflect.DeepEqual(inner.limits, []int{maxSessionDepth}) {
		t.Fatalf(
			"deep link = rows %d, total %d, depths %v",
			len(resp.Results),
			resp.TotalResults,
			inner.limits,
		)
	}
}

func TestStableWindowSerializesConcurrentExtension(t *testing.T) {
	inner := &expandingSearcher{total: maxSessionDepth, available: maxSessionDepth}
	searcher := WithStableWindow(inner)
	ctx := context.Background()
	if _, err := searcher.Search(ctx, searchcore.Request{Query: "go", Limit: 10}); err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			resp, err := searcher.Search(ctx, searchcore.Request{
				Query: "go", Offset: 50, Limit: 10,
			})
			if err == nil && len(resp.Results) != 10 {
				err = fmt.Errorf("rows = %d", len(resp.Results))
			}
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if inner.calls != 2 {
		t.Fatalf("concurrent extensions searched %d times", inner.calls)
	}
}

func TestStableWindowAdvancesPastPreviouslySearchedDepth(t *testing.T) {
	inner := &expandingSearcher{total: maxSessionDepth, available: maxSessionDepth}
	stable := &stableSearcher{inner: inner}
	results := make([]searchcore.Result, 75)
	for index := range results {
		results[index] = searchcore.Result{
			URLHash: fmt.Sprintf("hash-%d", index),
		}
	}
	entry := &session{
		results: results, total: maxSessionDepth, searchDepth: 100,
	}
	if err := stable.extend(context.Background(), entry, searchcore.Request{
		Offset: 80, Limit: 10,
	}); err != nil {
		t.Fatal(err)
	}
	if entry.searchDepth != 150 || len(entry.results) != 150 ||
		!reflect.DeepEqual(inner.limits, []int{150}) {
		t.Fatalf(
			"extended entry = depth %d, rows %d, calls %v",
			entry.searchDepth,
			len(entry.results),
			inner.limits,
		)
	}

	entry.results = entry.results[:50]
	entry.searchDepth = maxSessionDepth
	entry.total = maxSessionDepth
	if err := stable.extend(context.Background(), entry, searchcore.Request{
		Offset: 490, Limit: 10,
	}); err != nil {
		t.Fatal(err)
	}
	if entry.total != 50 || !reflect.DeepEqual(inner.limits, []int{150}) {
		t.Fatalf("exhausted entry = total %d, calls %v", entry.total, inner.limits)
	}
}
