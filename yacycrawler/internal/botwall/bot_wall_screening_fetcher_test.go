package botwall_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacycrawler/internal/botwall"
	"github.com/D4rk4/yago/yacycrawler/internal/pagefetch"
)

type pageSourceFunc func(context.Context, *url.URL) (pagefetch.FetchedPage, error)

func (f pageSourceFunc) Fetch(ctx context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
	return f(ctx, target)
}

func bodySource(body string) pageSourceFunc {
	return func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
		return pagefetch.FetchedPage{URL: target, Body: []byte(body)}, nil
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return parsed
}

func TestBotWallScreeningFetcher(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		rejected bool
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := botwall.NewBotWallScreeningFetcher(bodySource(tc.body))
			page, err := fetcher.Fetch(context.Background(), mustParse(t, "https://example.com/"))
			switch {
			case tc.rejected && !errors.Is(err, pagefetch.ErrPageRejected):
				t.Errorf("err = %v, want ErrPageRejected", err)
			case !tc.rejected && err != nil:
				t.Errorf("unexpected err: %v", err)
			case !tc.rejected && page.URL.String() != "https://example.com/":
				t.Errorf("page not delegated: %+v", page)
			}
		})
	}
}

func TestBotWallScreeningFetcherScansBoundedPrefix(t *testing.T) {
	body := strings.Repeat("a", 70<<10) + "just a moment..."
	fetcher := botwall.NewBotWallScreeningFetcher(bodySource(body))
	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, "https://example.com/"),
	); err != nil {
		t.Errorf("marker beyond scan limit should not be detected: %v", err)
	}
}

func TestBotWallScreeningFetcherPropagatesInnerError(t *testing.T) {
	sentinel := errors.New("boom")
	fetcher := botwall.NewBotWallScreeningFetcher(
		pageSourceFunc(func(_ context.Context, _ *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, sentinel
		}),
	)
	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, "https://example.com/"),
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Errorf("err = %v, want inner sentinel", err)
	}
}
