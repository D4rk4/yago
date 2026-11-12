package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"math"
	"net/url"
	"sort"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagomodel"
)

const (
	searchDemandPollInterval    = 5 * time.Second
	searchDemandPageSize        = 512
	searchDemandHalfLife        = 30 * 24 * time.Hour
	searchDemandRecrawlInterval = searchDemandHalfLife
	searchDemandHistoryLimit    = 5 * searchDemandHalfLife
	searchDemandRunID           = "crawler-owned"
)

const messageSearchDemandCycle = "crawler search demand cycle finished"
const messageSearchDemandCursorReset = "crawler search demand cursor reset"

type searchDemandCandidate struct {
	url       string
	score     float64
	focusedAt time.Time
}

func (worker *Worker) runSearchDemand(ctx context.Context) error {
	order := yagocrawlcontract.CrawlOrder{
		Provenance: searchDemandRunIdentity(searchDemandRunID),
		Profile:    searchDemandProfile(worker.policy),
	}
	order.Profile.Handle = searchDemandRunID
	runID := hex.EncodeToString(order.Provenance)
	rules, err := compileRunRules(order, worker.policy)
	if err != nil {
		return err
	}
	rules.maximumPages = 0
	rules.maximumHostPages = 0
	go worker.readSearchDemand(ctx, runID, rules)
	go worker.reportPersistentFrontier(ctx, order, runID)
	_, err = worker.executePersistentFrontier(ctx, order, searchDemandRunID, runID, rules)
	return err
}

func (worker *Worker) readSearchDemand(ctx context.Context, runID string, rules runRules) {
	for ctx.Err() == nil {
		started := time.Now()
		if err := worker.syncSearchDemand(ctx, runID, rules); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, messageSearchDemandCycle,
				slog.String("stage", "read_demand"), slog.Any("error", err))
		} else if ctx.Err() == nil {
			slog.InfoContext(ctx, messageSearchDemandCycle,
				slog.Duration("elapsed", time.Since(started)))
		}
		if !waitContext(ctx, searchDemandPollInterval) {
			return
		}
	}
}

func (worker *Worker) syncSearchDemand(ctx context.Context, runID string, rules runRules) error {
	cursor, err := worker.frontier.SearchDemandCursor(ctx)
	if err != nil {
		return err
	}
	for ctx.Err() == nil {
		page, err := worker.exchange.ReadSearchDemand(ctx, cursor, searchDemandPageSize)
		if err != nil {
			return err
		}
		records := page.GetRecords()
		nextCursor := page.GetNextCursor()
		if searchDemandCursorRegressed(cursor, nextCursor) {
			if err := worker.frontier.ResetSearchDemandCursor(ctx); err != nil {
				return err
			}
			slog.WarnContext(ctx, messageSearchDemandCursorReset,
				slog.String("previous_cursor", cursor),
				slog.String("node_cursor", nextCursor),
			)
			cursor = "0"
		}
		candidates := searchDemandCandidates(records, time.Now())
		for _, candidate := range candidates {
			if worker.urlDenylist.Blocks(candidate.url) {
				continue
			}
			rules.roots.add(candidate.url)
			now := time.Now().UTC()
			queued, err := worker.frontier.RequeueForRecrawl(
				ctx,
				runID,
				candidate.url,
				now,
				searchDemandRecrawlMinimumAge(candidate, now),
			)
			if err != nil {
				return err
			}
			if queued {
				continue
			}
			if _, _, err := worker.frontier.Enqueue(
				ctx, runID, candidate.url, "", 0, time.Now().UTC(),
			); err != nil && !errors.Is(err, frontier.ErrPendingLimit) &&
				!errors.Is(err, frontier.ErrStateLimit) {
				return err
			} else if err != nil {
				return err
			}
		}
		if len(records) > 0 && nextCursor == cursor {
			return errors.New("search demand cursor did not advance")
		}
		if nextCursor != cursor {
			if err := worker.frontier.SaveSearchDemandCursor(ctx, nextCursor); err != nil {
				return err
			}
			cursor = nextCursor
		}
		if len(records) < searchDemandPageSize {
			return nil
		}
	}
	return ctx.Err()
}

func searchDemandCandidates(records []*crawlrpc.SearchDemandRecord, now time.Time) []searchDemandCandidate {
	byURL := make(map[string]searchDemandCandidate, len(records))
	for _, record := range records {
		candidate, ok := demandCandidate(record, now)
		if !ok {
			continue
		}
		previous, found := byURL[candidate.url]
		if found {
			candidate = mergeSearchDemandCandidates(previous, candidate)
		}
		byURL[candidate.url] = candidate
	}
	candidates := make([]searchDemandCandidate, 0, len(byURL))
	for _, candidate := range byURL {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].score == candidates[right].score {
			return candidates[left].url < candidates[right].url
		}
		return candidates[left].score > candidates[right].score
	})
	return candidates
}

func demandCandidate(record *crawlrpc.SearchDemandRecord, now time.Time) (searchDemandCandidate, bool) {
	if record == nil || record.GetUrl() == "" {
		return searchDemandCandidate{}, false
	}
	canonicalURL, ok := yagocrawlcontract.CanonicalURL(record.GetUrl())
	if !ok {
		return searchDemandCandidate{}, false
	}
	parsed, err := url.Parse(canonicalURL)
	if err != nil || parsed.Hostname() == "" {
		return searchDemandCandidate{}, false
	}
	lastSeen := timeFromUnixNano(record.GetLastSeenUnixNano())
	lastClicked := timeFromUnixNano(record.GetLastClickedUnixNano())
	focusedAt := timeFromUnixNano(record.GetFocusedAtUnixNano())
	lastActivity := latestTime(lastSeen, lastClicked, focusedAt)
	if lastActivity.IsZero() || now.Sub(lastActivity) > searchDemandHistoryLimit {
		return searchDemandCandidate{}, false
	}
	age := max(time.Duration(0), now.Sub(lastActivity))
	signal := math.Log1p(float64(record.GetShown())) + 5*math.Log1p(float64(record.GetClicked()))
	if !focusedAt.IsZero() {
		signal += 8
	}
	return searchDemandCandidate{
		url: canonicalURL, score: signal * math.Exp2(-float64(age)/float64(searchDemandHalfLife)),
		focusedAt: focusedAt,
	}, true
}

func timeFromUnixNano(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(0, value).UTC()
}

func latestTime(values ...time.Time) time.Time {
	var latest time.Time
	for _, value := range values {
		if value.After(latest) {
			latest = value
		}
	}
	return latest
}

func searchDemandProfile(policy yagocrawlcontract.CrawlerRuntimePolicy) yagocrawlcontract.CrawlProfile {
	return yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name:                "search-demand",
		Scope:               yagocrawlcontract.ScopeDomain,
		URLMustMatch:        yagocrawlcontract.MatchAll,
		MaxDepth:            policy.MaximumDepth,
		MaxPagesPerHost:     yagocrawlcontract.UnlimitedPagesPerHost,
		AllowQueryURLs:      true,
		FollowNoFollowLinks: false,
		CrawlDelay:          policy.CrawlDelay,
	})
}

func searchDemandRunIdentity(domain string) []byte {
	identity := sha256.Sum256([]byte(domain))
	return append([]byte(nil), identity[:yagomodel.HashLength]...)
}
