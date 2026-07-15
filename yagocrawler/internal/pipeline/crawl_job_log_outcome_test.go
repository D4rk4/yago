package pipeline

import (
	"errors"
	"log/slog"
	"net"
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawler/internal/robots"
)

func TestCrawlJobLogOutcomeKeepsExpectedTerminalRejectionsAtDebug(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		level slog.Level
	}{
		{name: "gone", err: &pagefetch.GoneError{Status: 404}, level: slog.LevelDebug},
		{
			name:  "robots",
			err:   errors.Join(robots.ErrDisallowed, pagefetch.ErrPageRejected),
			level: slog.LevelDebug,
		},
		{
			name:  "unsupported format",
			err:   pagefetch.ErrUnsupportedContentType,
			level: slog.LevelDebug,
		},
		{name: "forbidden", err: &pagefetch.HTTPStatusError{Status: 403}, level: slog.LevelWarn},
		{name: "plain rejection", err: pagefetch.ErrPageRejected, level: slog.LevelWarn},
		{
			name:  "network",
			err:   &net.DNSError{Err: "timeout", Name: "example.com", IsTimeout: true},
			level: slog.LevelWarn,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if level := crawlJobLogLevel(test.err); level != test.level {
				t.Fatalf("log level = %s, want %s", level, test.level)
			}
		})
	}
}
