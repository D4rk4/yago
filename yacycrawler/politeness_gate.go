package yacycrawler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/temoto/robotstxt"
	"golang.org/x/time/rate"
)

const DefaultCrawlDelay = 1 * time.Second

type PolitenessGate struct {
	client    *http.Client
	userAgent string
	delay     time.Duration

	mu       sync.Mutex
	robots   map[string]*robotstxt.Group
	limiters map[string]*rate.Limiter
}

func NewPolitenessGate(client *http.Client, userAgent string, delay time.Duration) *PolitenessGate {
	return &PolitenessGate{
		client:    client,
		userAgent: userAgent,
		delay:     delay,
		robots:    make(map[string]*robotstxt.Group),
		limiters:  make(map[string]*rate.Limiter),
	}
}

func (g *PolitenessGate) Allow(ctx context.Context, rawURL string) (bool, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return false, fmt.Errorf("parse url: %w", err)
	}
	group := g.robotsGroup(ctx, target)
	if !group.Test(target.Path) {
		return false, nil
	}
	if err := g.limiter(target.Host).Wait(ctx); err != nil {
		return false, fmt.Errorf("rate limit wait: %w", err)
	}
	return true, nil
}

func (g *PolitenessGate) robotsGroup(ctx context.Context, target *url.URL) *robotstxt.Group {
	g.mu.Lock()
	cached, ok := g.robots[target.Host]
	g.mu.Unlock()
	if ok {
		return cached
	}

	group := g.fetchRobotsGroup(ctx, target)
	g.mu.Lock()
	g.robots[target.Host] = group
	g.mu.Unlock()
	return group
}

func (g *PolitenessGate) fetchRobotsGroup(ctx context.Context, target *url.URL) *robotstxt.Group {
	robotsURL := target.Scheme + "://" + target.Host + "/robots.txt"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		return allowAll()
	}
	response, err := g.client.Do(request)
	if err != nil {
		slog.Warn("robots fetch failed", "host", target.Host, "error", err)
		return allowAll()
	}
	defer func() {
		if cerr := response.Body.Close(); cerr != nil {
			slog.Warn("robots body close failed", "host", target.Host, "error", cerr)
		}
	}()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return allowAll()
	}
	data, err := robotstxt.FromStatusAndBytes(response.StatusCode, body)
	if err != nil {
		return allowAll()
	}
	return data.FindGroup(g.userAgent)
}

func (g *PolitenessGate) limiter(host string) *rate.Limiter {
	g.mu.Lock()
	defer g.mu.Unlock()
	if existing, ok := g.limiters[host]; ok {
		return existing
	}
	limiter := rate.NewLimiter(rate.Every(g.delay), 1)
	g.limiters[host] = limiter
	return limiter
}

func allowAll() *robotstxt.Group {
	data, _ := robotstxt.FromBytes(nil)
	return data.FindGroup("*")
}
