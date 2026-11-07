package worker

import (
	"context"

	"github.com/D4rk4/yago/yago-crawler2/internal/httpfetch"
)

type pageFetcher interface {
	Fetch(context.Context, string) (httpfetch.PageResponse, error)
	SetMaximumRedirects(int) bool
	CloseIdleConnections()
}
