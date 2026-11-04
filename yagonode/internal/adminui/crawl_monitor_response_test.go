package adminui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCrawlMonitorReturnsOnlyRequestedRefreshFragment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		target  string
		present string
		absent  string
	}{
		{
			target:  crawlMonitorLiveTarget,
			present: `id="crawl-live"`,
			absent:  `id="crawl-history-list"`,
		},
		{
			target:  crawlHistoryListTarget,
			present: `id="crawl-history-list"`,
			absent:  `id="crawl-live"`,
		},
	}
	for _, test := range tests {
		t.Run(test.target, func(t *testing.T) {
			t.Parallel()

			console := New(Options{Monitor: fakeMonitor{snap: sampleMonitor()}})
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				crawlMonitorPath,
				nil,
			)
			request.Header.Set("HX-Request", "true")
			request.Header.Set("HX-Target", test.target)
			recorder := httptest.NewRecorder()
			console.ServeHTTP(recorder, request)

			body := recorder.Body.String()
			if recorder.Code != http.StatusOK || !strings.Contains(body, test.present) ||
				strings.Contains(body, test.absent) || strings.Contains(body, "<html") {
				t.Fatalf("fragment response = %d %q", recorder.Code, body)
			}
		})
	}
}
