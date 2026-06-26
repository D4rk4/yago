package botwall

import (
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

type BotWallScreen interface {
	IsBotWall(page pagefetch.FetchedPage) bool
}

type BotWallDetector struct{}

func NewBotWallDetector() *BotWallDetector {
	return &BotWallDetector{}
}

func (d *BotWallDetector) IsBotWall(page pagefetch.FetchedPage) bool {
	body := page.Body
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
