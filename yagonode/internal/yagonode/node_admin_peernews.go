package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/peernews"
)

const adminPeerNewsLimit = 20

// peerNewsReader is the read-only slice of the news pool the console needs.
type peerNewsReader interface {
	Recent(ctx context.Context, queue peernews.Queue, limit int) ([]peernews.Record, error)
}

// peerNewsSource surfaces the most recent received peer-news records for the
// console's Network section. It reads the incoming queue and carries no secrets.
type peerNewsSource struct {
	news peerNewsReader
	now  func() time.Time
}

func newPeerNewsSource(news peerNewsReader) peerNewsSource {
	return peerNewsSource{news: news, now: time.Now}
}

func (s peerNewsSource) PeerNews(ctx context.Context) []adminui.PeerNewsItem {
	records, err := s.news.Recent(ctx, peernews.Incoming, adminPeerNewsLimit)
	if err != nil {
		slog.WarnContext(ctx, "peer news snapshot failed", slog.Any("error", err))

		return nil
	}

	now := s.now()
	items := make([]adminui.PeerNewsItem, 0, len(records))
	for _, record := range records {
		items = append(items, adminui.PeerNewsItem{
			Category:   record.Category,
			Originator: record.Originator.String(),
			Age:        peerNewsAge(record, now),
			Detail:     peerNewsDetail(record.Attributes),
		})
	}

	return items
}

// peerNewsAge humanizes how long ago a record was received (or created, when the
// received time is absent), as a compact minutes/hours/days string.
func peerNewsAge(record peernews.Record, now time.Time) string {
	stamp := record.Received
	if stamp.IsZero() {
		stamp = record.Created
	}
	if stamp.IsZero() {
		return ""
	}

	elapsed := now.Sub(stamp)
	if elapsed < 0 {
		elapsed = 0
	}
	switch {
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd", int(elapsed.Hours()/24))
	}
}

// peerNewsDetail renders a record's payload attributes as a compact, stable
// key=value list so the operator can see the news content without secrets.
func peerNewsDetail(attributes map[string]string) string {
	if len(attributes) == 0 {
		return ""
	}

	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+attributes[key])
	}

	return strings.Join(pairs, ", ")
}
