package yagonode

import (
	"context"
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

// Denylist probe and plaintext import/export — YaCy's BlacklistTest_p and
// BlacklistImpExp_p parity (UI-17). The wire format is one entry per line,
// "kind value" ("domain example.com" / "url https://…"); a bare line is
// treated as a domain when it has no scheme and as a URL otherwise, so a
// plain host list from another system imports as-is.

// BlacklistBlocks reports whether the denylist would block rawURL right now,
// through the same snapshot matcher the search filter uses.
func (c *blacklistController) BlacklistBlocks(_ context.Context, rawURL string) (bool, error) {
	snapshot := c.store.Snapshot()
	return snapshot.Blocks(strings.TrimSpace(rawURL)), nil
}

// ExportBlacklist renders the whole denylist as importable plaintext.
func (c *blacklistController) ExportBlacklist(ctx context.Context) (string, error) {
	entries, err := c.store.Entries(ctx)
	if err != nil {
		return "", fmt.Errorf("list denylist entries: %w", err)
	}
	var b strings.Builder
	for _, entry := range entries {
		b.WriteString(string(entry.Kind) + " " + entry.Value + "\n")
	}

	return b.String(), nil
}

// ImportBlacklist adds every parseable line, reporting how many were added;
// blank lines and #-comments are skipped, a malformed line aborts with its
// line number so the operator can fix the file instead of half-importing.
func (c *blacklistController) ImportBlacklist(
	ctx context.Context,
	payload string,
) (int, error) {
	added := 0
	for number, line := range strings.Split(payload, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kind, value, err := parseDenylistLine(line)
		if err != nil {
			return added, fmt.Errorf("line %d: %w", number+1, err)
		}
		if err := c.store.Add(ctx, kind, value); err != nil {
			return added, fmt.Errorf("line %d: add: %w", number+1, err)
		}
		added++
	}

	return added, nil
}

// parseDenylistLine reads one import line into a typed entry.
func parseDenylistLine(line string) (urldenylist.Kind, string, error) {
	kindToken, value, explicit := strings.Cut(line, " ")
	if explicit {
		kind, err := parseDenylistKind(kindToken)
		if err != nil {
			return "", "", err
		}

		return kind, strings.TrimSpace(value), nil
	}
	if strings.Contains(line, "://") {
		return urldenylist.KindURL, line, nil
	}

	return urldenylist.KindDomain, line, nil
}
