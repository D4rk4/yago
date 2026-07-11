package publicportal

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type portalImpressionRecorder struct {
	candidates []ImpressionCandidate
	prepared   PreparedImpression
	err        error
	calls      int
}

func (r *portalImpressionRecorder) PrepareImpression(
	_ context.Context,
	_ string,
	candidates []ImpressionCandidate,
) (PreparedImpression, error) {
	r.calls++
	r.candidates = append([]ImpressionCandidate(nil), candidates...)

	return r.prepared, r.err
}

func impressionResults() SearchResults {
	return SearchResults{
		Query: "query", TotalResults: 2,
		Results: []SearchResult{
			{
				Title: "First", URL: "https://first/", URLIdentity: "https://first/",
				ClusterIdentity: "first", Position: 11, LexicalPosition: 12,
			},
			{
				Title: "Second", URL: "https://second/", URLIdentity: "https://second/",
				ClusterIdentity: "second", Position: 12, LexicalPosition: 11,
			},
		},
	}
}

func TestPortalIssuesImpressionAndKeepsDirectLinks(t *testing.T) {
	recorder := &portalImpressionRecorder{prepared: PreparedImpression{
		Token: "signed-token", Order: []int{1, 0},
	}}
	portal := New(&fakeSource{results: impressionResults()}, false)
	portal.SetImpressionRecorder(recorder)
	_, body := get(t, portal, "/?q=query&p=2")
	for _, expected := range []string{
		`data-t="signed-token"`, `data-p="11"`, `data-i="https://second/"`,
		`href="https://second/"`, `navigator.sendBeacon("/searchclick", body)`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("portal impression missing %q: %s", expected, body)
		}
	}
	if strings.Index(body, "Second") > strings.Index(body, "First") ||
		len(recorder.candidates) != 2 || recorder.candidates[0].LexicalPosition != 12 {
		t.Fatalf("portal impression order = %#v", recorder)
	}
}

func TestPortalImpressionFallbacksAndThemeIsolation(t *testing.T) {
	for _, recorder := range []*portalImpressionRecorder{
		{err: errors.New("unavailable")},
		{prepared: PreparedImpression{Order: []int{0, 1}}},
		{prepared: PreparedImpression{Token: "token", Order: []int{0}}},
		{prepared: PreparedImpression{Token: "token", Order: []int{0, 0}}},
		{prepared: PreparedImpression{Token: "token", Order: []int{-1, 1}}},
		{prepared: PreparedImpression{Token: "token", Order: []int{0, 2}}},
	} {
		portal := New(&fakeSource{results: impressionResults()}, false)
		portal.SetImpressionRecorder(recorder)
		_, body := get(t, portal, "/?q=query")
		if strings.Contains(body, `data-t=`) {
			t.Fatalf("invalid impression rendered: %#v", recorder)
		}
	}
	imageRecorder := &portalImpressionRecorder{}
	imagePortal := New(&fakeSource{results: impressionResults()}, false)
	imagePortal.SetImpressionRecorder(imageRecorder)
	_, _ = get(t, imagePortal, "/?q=query&dom=image")
	if imageRecorder.calls != 0 {
		t.Fatal("image portal prepared a text impression")
	}
	themeRecorder := &portalImpressionRecorder{}
	themed := New(&fakeSource{results: impressionResults()}, false)
	themed.SetImpressionRecorder(themeRecorder)
	themed.SetTheme(&fakeTheme{html: "themed", ok: true})
	_, _ = get(t, themed, "/?q=query")
	if themeRecorder.calls != 0 {
		t.Fatal("operator theme recorded an unrendered impression")
	}
}
