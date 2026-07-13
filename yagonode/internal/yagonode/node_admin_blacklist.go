package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

type denylistStore interface {
	Entries(ctx context.Context) ([]urldenylist.Entry, error)
	Add(ctx context.Context, kind urldenylist.Kind, value string) error
	Remove(ctx context.Context, kind urldenylist.Kind, value string) (bool, error)
	Snapshot() urldenylist.Snapshot
}

// blacklistController adapts the durable denylist store to the admin console's
// blacklist source.
type blacklistController struct {
	store denylistStore
}

func newBlacklistController(store denylistStore) *blacklistController {
	return &blacklistController{store: store}
}

// BlacklistEntries lists the denylist for display; a read error degrades to an
// empty list rather than failing the page.
func (c *blacklistController) BlacklistEntries(ctx context.Context) []adminui.BlacklistEntry {
	entries, err := c.store.Entries(ctx)
	if err != nil {
		slog.WarnContext(ctx, "list denylist entries failed", slog.Any("error", err))

		return nil
	}

	views := make([]adminui.BlacklistEntry, 0, len(entries))
	for _, entry := range entries {
		views = append(views, adminui.BlacklistEntry{
			Kind:    string(entry.Kind),
			Value:   entry.Value,
			AddedAt: formatDenylistTime(entry.AddedAt),
		})
	}

	return views
}

// AddBlacklist blocks a URL or domain.
func (c *blacklistController) AddBlacklist(ctx context.Context, kind, value string) error {
	parsed, err := parseDenylistKind(kind)
	if err != nil {
		return err
	}
	if err := c.store.Add(ctx, parsed, value); err != nil {
		return fmt.Errorf("add denylist entry: %w", err)
	}

	return nil
}

// RemoveBlacklist unblocks a URL or domain.
func (c *blacklistController) RemoveBlacklist(ctx context.Context, kind, value string) error {
	parsed, err := parseDenylistKind(kind)
	if err != nil {
		return err
	}
	if _, err := c.store.Remove(ctx, parsed, value); err != nil {
		return fmt.Errorf("remove denylist entry: %w", err)
	}

	return nil
}

func parseDenylistKind(kind string) (urldenylist.Kind, error) {
	switch urldenylist.Kind(kind) {
	case urldenylist.KindURL:
		return urldenylist.KindURL, nil
	case urldenylist.KindDomain:
		return urldenylist.KindDomain, nil
	default:
		return "", fmt.Errorf("unknown denylist kind %q", kind)
	}
}

func formatDenylistTime(added time.Time) string {
	if added.IsZero() {
		return ""
	}

	return added.UTC().Format(time.RFC3339)
}
