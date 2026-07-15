package pipeline

import (
	"errors"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawler/internal/robots"
)

func crawlJobLogLevel(err error) slog.Level {
	if _, gone := pagefetch.AsGone(err); gone ||
		errors.Is(err, robots.ErrDisallowed) ||
		errors.Is(err, pagefetch.ErrUnsupportedContentType) {
		return slog.LevelDebug
	}

	return slog.LevelWarn
}
