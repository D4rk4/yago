package botwall_test

import (
	"strings"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/botwall"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
)

func TestBotWallDetector(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"cloudflare interstitial", "<title>Just a moment...</title>", true},
		{
			"cloudflare challenge platform",
			`<script src="/cdn-cgi/challenge-platform/h/b/orchestrate"></script>`,
			true,
		},
		{
			"cloudflare turnstile",
			`<script src="https://challenges.cloudflare.com/turnstile/v0/api.js"></script>`,
			true,
		},
		{"datadome", `<script src="https://js.captcha-delivery.com/captcha.js"></script>`, true},
		{"perimeterx", `<div id="px-captcha"></div>`, true},
		{
			"recaptcha challenge",
			`<iframe src="https://www.google.com/recaptcha/api2/anchor"></iframe>`,
			true,
		},
		{"hcaptcha challenge", `<iframe src="https://hcaptcha.com/captcha/v1"></iframe>`, true},
		{"case insensitive", "<TITLE>JUST A MOMENT...</TITLE>", true},
		{"legit article", "<title>The platypus swims in rivers</title><p>content</p>", false},
		{"empty", "", false},
	}
	detector := botwall.NewBotWallDetector()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := pagefetch.FetchedPage{Body: []byte(tc.body)}
			if got := detector.IsBotWall(page); got != tc.want {
				t.Errorf("IsBotWall = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBotWallDetectorScansBoundedPrefix(t *testing.T) {
	body := strings.Repeat("a", 70<<10) + "just a moment..."
	page := pagefetch.FetchedPage{Body: []byte(body)}
	if botwall.NewBotWallDetector().IsBotWall(page) {
		t.Error("marker beyond scan limit should not be detected")
	}
}
