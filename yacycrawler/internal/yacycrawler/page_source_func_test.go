package yacycrawler_test

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/yacycrawler"
)

type pageSourceFunc func(context.Context, string) (yacycrawler.FetchedPage, error)

func (f pageSourceFunc) Fetch(ctx context.Context, rawURL string) (yacycrawler.FetchedPage, error) {
	return f(ctx, rawURL)
}
