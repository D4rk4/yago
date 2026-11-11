package yagonode

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/searchdemand"
)

type focusedDemandRecorder interface {
	RecordFocusedBatch(context.Context, string, []string, time.Time) error
}

type searchDemandCrawlSource struct {
	recorder focusedDemandRecorder
	now      func() time.Time
}

func newSearchDemandCrawlSource(recorder focusedDemandRecorder) adminui.CrawlSource {
	if recorder == nil {
		return nil
	}
	return searchDemandCrawlSource{recorder: recorder, now: time.Now}
}

func (source searchDemandCrawlSource) Start(
	ctx context.Context,
	start adminui.CrawlStart,
) (adminui.CrawlDispatch, error) {
	topic := strings.Join(strings.Fields(strings.TrimSpace(start.Name)), " ")
	if topic == "" {
		topic = "Operator focus"
	}
	seeds := make([]string, 0, len(start.Seeds))
	seen := make(map[string]struct{}, len(start.Seeds))
	for _, rawURL := range start.Seeds {
		canonical, valid := yagocrawlcontract.CanonicalURL(strings.TrimSpace(rawURL))
		if !valid {
			return adminui.CrawlDispatch{}, searchdemand.ErrInvalidDemand
		}
		if _, found := seen[canonical]; found {
			continue
		}
		seen[canonical] = struct{}{}
		seeds = append(seeds, canonical)
	}
	if len(seeds) == 0 {
		return adminui.CrawlDispatch{}, errors.New("at least one seed URL is required")
	}
	at := time.Now().UTC()
	if source.now != nil {
		at = source.now().UTC()
	}
	if err := source.recorder.RecordFocusedBatch(ctx, topic, seeds, at); err != nil {
		return adminui.CrawlDispatch{}, err
	}
	return adminui.CrawlDispatch{ProfileHandle: topic, Seeds: len(seeds)}, nil
}

var _ focusedDemandRecorder = (*searchdemand.Store)(nil)
