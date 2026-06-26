package botwall

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
)

const botWallScanLimit = 64 << 10

var botWallMarkers = []string{
	"/cdn-cgi/challenge-platform/",
	"_cf_chl_opt",
	"cf-browser-verification",
	"challenges.cloudflare.com/turnstile",
	"just a moment...",
	"captcha-delivery.com",
	"px-captcha",
	"/recaptcha/api2/anchor",
	"hcaptcha.com/captcha",
}

type BotWallScreeningFetcher struct {
	inner pagefetch.PageSource
}

func NewBotWallScreeningFetcher(inner pagefetch.PageSource) *BotWallScreeningFetcher {
	return &BotWallScreeningFetcher{inner: inner}
}

func (f *BotWallScreeningFetcher) Fetch(
	ctx context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	page, err := f.inner.Fetch(ctx, target)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("inner fetch: %w", err)
	}
	if showsBotWall(page.Body) {
		return pagefetch.FetchedPage{}, fmt.Errorf("bot wall: %w", pagefetch.ErrPageRejected)
	}
	return page, nil
}

func showsBotWall(body []byte) bool {
	if len(body) > botWallScanLimit {
		body = body[:botWallScanLimit]
	}
	haystack := strings.ToLower(string(body))
	for _, marker := range botWallMarkers {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}
