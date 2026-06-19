package yacycrawler

import "strings"

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

type BotWallDetector struct{}

func NewBotWallDetector() *BotWallDetector {
	return &BotWallDetector{}
}

func (d *BotWallDetector) IsBotWall(page FetchedPage) bool {
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
