package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestOverviewSourceMapsReport(t *testing.T) {
	report := stubReport{seed: yagomodel.Seed{Hash: yagomodel.Hash("0123456789AB")}}
	overview := newOverviewSource(report).Overview(context.Background())
	if overview.PeerHash != "0123456789AB" {
		t.Fatalf("peer hash = %q", overview.PeerHash)
	}
	if overview.Version != "1.83" || overview.UptimeSeconds != 315 {
		t.Fatalf("overview = %+v", overview)
	}
}

type stubSearchIndex struct {
	stats searchindex.IndexStats
	err   error
}

func (stubSearchIndex) Index(context.Context, documentstore.Document) error { return nil }

func (stubSearchIndex) Delete(context.Context, string) error { return nil }

func (stubSearchIndex) Search(
	context.Context,
	searchindex.SearchRequest,
) (searchindex.SearchResultSet, error) {
	return searchindex.SearchResultSet{}, nil
}

func (s stubSearchIndex) Stats(context.Context) (searchindex.IndexStats, error) {
	return s.stats, s.err
}

func TestIndexSourceCoversAllPaths(t *testing.T) {
	ctx := context.Background()

	if got := newIndexSource(nil).Index(ctx); got.Available {
		t.Fatal("nil index should be unavailable")
	}
	if got := (newIndexSource(stubSearchIndex{err: errors.New("boom")})).Index(ctx); got.Available {
		t.Fatal("failed stats should be unavailable")
	}
	got := newIndexSource(stubSearchIndex{
		stats: searchindex.IndexStats{Documents: 9, Backend: "bleve"},
	}).Index(ctx)
	if !got.Available || got.Documents != 9 || got.Backend != "bleve" {
		t.Fatalf("index stats = %+v", got)
	}
}

type stubAdminSearcher struct {
	response   searchcore.Response
	err        error
	gotRequest searchcore.Request
}

func (s *stubAdminSearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.gotRequest = req

	return s.response, s.err
}

func TestSearchSourceMapsResultsAndFailures(t *testing.T) {
	ctx := context.Background()
	searcher := &stubAdminSearcher{response: searchcore.Response{
		TotalResults: 2,
		Results: []searchcore.Result{
			{Title: "A", URL: "https://a", Source: searchcore.SourceWeb},
			{Title: "B", URL: "https://b", Source: searchcore.SourceLocal},
		},
		PartialFailures: []searchcore.PartialFailure{{Source: "global", Reason: "timeout"}},
	}}
	got, err := newSearchSource(searcher).Search(ctx, adminui.SearchQuery{
		Query: "q", Global: true, Offset: 40, Limit: 20,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got.TotalResults != 2 || len(got.Results) != 2 || len(got.Failures) != 1 {
		t.Fatalf("results = %+v", got)
	}
	if searcher.gotRequest.Offset != 40 || searcher.gotRequest.Limit != 20 {
		t.Fatalf("offset=%d limit=%d, want the window forwarded to the searcher",
			searcher.gotRequest.Offset, searcher.gotRequest.Limit)
	}
	if got.Results[0].Source != "web" || got.Results[1].Source != "local" {
		t.Fatalf("result provenance = %q / %q, want web / local",
			got.Results[0].Source, got.Results[1].Source)
	}
	if got.Failures[0] != "global: timeout" {
		t.Fatalf("failure = %q", got.Failures[0])
	}

	if _, err := newSearchSource(&stubAdminSearcher{err: errors.New("down")}).
		Search(ctx, adminui.SearchQuery{Query: "q"}); err == nil {
		t.Fatal("expected search error")
	}
}

func TestLogsSourceCoversNilAndEvents(t *testing.T) {
	if got := newLogsSource(nil).Logs(context.Background()); got != nil {
		t.Fatal("nil recorder should yield nil logs")
	}
	recorder := events.NewRecorder(4)
	recorder.Record(events.SeverityInfo, events.CategoryConfig, "startup", "node started")
	got := newLogsSource(recorder).Logs(context.Background())
	if len(got) != 1 || got[0].Name != "startup" || got[0].Message != "node started" {
		t.Fatalf("logs = %+v", got)
	}
}

func TestConfigSourceRendersDisabledToggles(t *testing.T) {
	config := testConfig(t)
	config.SearchRequireAPIKey = false
	config.EgressAllowLAN = false
	config.PublicSearchUIEnabled = false
	config.WebFallback.Enabled = false
	view := newConfigSource(config).Config(context.Background())
	if len(view.Groups) == 0 {
		t.Fatal("config view has no groups")
	}
}
