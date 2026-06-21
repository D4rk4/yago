package yacycrawler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/temoto/robotstxt"
	"golang.org/x/time/rate"
)

const DefaultCrawlDelay = 1 * time.Second

type PolitenessGate struct {
	client    *http.Client
	userAgent string
	delay     time.Duration
	requests  chan politenessRequest
}

type politenessRequest struct {
	ctx      context.Context
	target   *url.URL
	response chan politenessResponse
}

type politenessResponse struct {
	allowed bool
	err     error
}

type politenessHost struct {
	gate    *PolitenessGate
	group   *robotstxt.Group
	limiter *rate.Limiter
}

func NewPolitenessGate(client *http.Client, userAgent string, delay time.Duration) *PolitenessGate {
	gate := &PolitenessGate{
		client:    client,
		userAgent: userAgent,
		delay:     delay,
		requests:  make(chan politenessRequest),
	}
	go gate.run()
	return gate
}

func (g *PolitenessGate) Allow(ctx context.Context, rawURL string) (bool, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return false, fmt.Errorf("parse url: %w", err)
	}
	response := make(chan politenessResponse, 1)
	request := politenessRequest{ctx: ctx, target: target, response: response}
	select {
	case g.requests <- request:
	case <-ctx.Done():
		return false, fmt.Errorf("politeness request: %w", ctx.Err())
	}
	select {
	case result := <-response:
		return result.allowed, result.err
	case <-ctx.Done():
		return false, fmt.Errorf("politeness response: %w", ctx.Err())
	}
}

func (g *PolitenessGate) run() {
	hosts := make(map[string]chan politenessRequest)
	for request := range g.requests {
		hostRequests, ok := hosts[request.target.Host]
		if !ok {
			hostRequests = make(chan politenessRequest, 1)
			hosts[request.target.Host] = hostRequests
			go (&politenessHost{
				gate:    g,
				limiter: rate.NewLimiter(rate.Every(g.delay), 1),
			}).run(hostRequests)
		}
		hostRequests <- request
	}
}

func (g *PolitenessGate) fetchRobotsGroup(ctx context.Context, target *url.URL) *robotstxt.Group {
	robotsURL := target.Scheme + "://" + target.Host + "/robots.txt"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		return allowAll()
	}
	response, err := g.client.Do(request)
	if err != nil {
		slog.WarnContext(
			ctx,
			"robots fetch failed",
			slog.String("host", target.Host),
			slog.Any("error", err),
		)
		return allowAll()
	}
	defer func() {
		if cerr := response.Body.Close(); cerr != nil {
			slog.WarnContext(
				ctx,
				"robots body close failed",
				slog.String("host", target.Host),
				slog.Any("error", cerr),
			)
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

func (h *politenessHost) run(requests <-chan politenessRequest) {
	for request := range requests {
		request.response <- h.allow(request.ctx, request.target)
	}
}

func (h *politenessHost) allow(ctx context.Context, target *url.URL) politenessResponse {
	if h.group == nil {
		h.group = h.gate.fetchRobotsGroup(ctx, target)
	}
	if !h.group.Test(target.Path) {
		return politenessResponse{allowed: false}
	}
	if err := h.limiter.Wait(ctx); err != nil {
		return politenessResponse{err: fmt.Errorf("rate limit wait: %w", err)}
	}
	return politenessResponse{allowed: true}
}

func allowAll() *robotstxt.Group {
	data, _ := robotstxt.FromBytes(nil)
	return data.FindGroup("*")
}
