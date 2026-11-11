package yagonode

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchdemand"
)

const msgSearchDemandRecordFailed = "search demand record failed"

type searchDemandSearcher struct {
	inner searchcore.Searcher
	store *searchdemand.Store
	clock func() time.Time
}

type crawlerSearchDemandReader struct {
	store *searchdemand.Store
}

func (reader crawlerSearchDemandReader) ReadPage(
	ctx context.Context,
	cursor string,
	pageSize int,
) (yagocrawlcontract.SearchDemandPage, error) {
	page, err := reader.store.ReadUpdates(ctx, cursor, pageSize)
	if err != nil {
		return yagocrawlcontract.SearchDemandPage{}, err
	}
	result := yagocrawlcontract.SearchDemandPage{
		Records:    make([]yagocrawlcontract.SearchDemandRecord, len(page.Records)),
		NextCursor: page.NextCursor,
	}
	for index, record := range page.Records {
		result.Records[index] = yagocrawlcontract.SearchDemandRecord{
			Topic: record.Topic, URL: record.URL, Shown: record.Shown, Clicked: record.Clicked,
			LastSeen: record.LastSeen, LastClicked: record.LastClicked, FocusedAt: record.FocusedAt,
		}
	}
	return result, nil
}

func (searcher searchDemandSearcher) Search(
	ctx context.Context,
	request searchcore.Request,
) (searchcore.Response, error) {
	response, err := searcher.inner.Search(ctx, request)
	if err != nil {
		return response, err
	}
	urls := make([]string, 0, len(response.Results))
	for _, result := range response.Results {
		urls = append(urls, result.URL)
	}
	if len(urls) > 0 {
		at := time.Now().UTC()
		if searcher.clock != nil {
			at = searcher.clock().UTC()
		}
		if err := searcher.store.RecordShownBatch(ctx, request.Query, urls, at); err != nil {
			slog.WarnContext(ctx, msgSearchDemandRecordFailed,
				slog.Int("results", len(urls)),
				slog.Any("error", err),
			)
		}
	}
	return response, nil
}
