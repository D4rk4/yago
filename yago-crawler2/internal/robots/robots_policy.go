package robots

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/D4rk4/yago/yago-crawler2/internal/httpfetch"
	"github.com/temoto/robotstxt"
)

const robotsCacheLifetime = 24 * time.Hour

type originRules struct {
	group     *robotstxt.Group
	validTill time.Time
}

type Policy struct {
	fetchPage func(context.Context, string) (httpfetch.PageResponse, error)
	userAgent string
	delay     time.Duration
	cache     map[string]originRules
}

func NewPolicy(
	fetchPage func(context.Context, string) (httpfetch.PageResponse, error),
	userAgent string,
	delay time.Duration,
) *Policy {
	return &Policy{
		fetchPage: fetchPage,
		userAgent: userAgent,
		delay:     delay,
		cache:     make(map[string]originRules),
	}
}

func (policy *Policy) Allows(ctx context.Context, rawURL string, ignore bool) (bool, error) {
	if ignore {
		return true, nil
	}
	target, err := url.Parse(rawURL)
	if err != nil {
		return false, err
	}
	origin := target.Scheme + "://" + target.Host
	rules, found := policy.cache[origin]
	if !found || !rules.validTill.After(time.Now()) {
		rules, err = policy.load(ctx, origin)
		if err != nil {
			return false, err
		}
		policy.cache[origin] = rules
	}
	requestTarget := target.EscapedPath()
	if requestTarget == "" {
		requestTarget = "/"
	}
	if target.RawQuery != "" {
		requestTarget += "?" + target.RawQuery
	}
	return rules.group.Test(requestTarget), nil
}

func (policy *Policy) load(ctx context.Context, origin string) (originRules, error) {
	robotsURL := strings.TrimRight(origin, "/") + "/robots.txt"
	response, err := policy.fetchPage(ctx, robotsURL)
	if err != nil {
		if ctx.Err() != nil {
			return originRules{}, ctx.Err()
		}
		return originRules{group: disallowAll(), validTill: time.Now().Add(robotsCacheLifetime)}, nil
	}
	var group *robotstxt.Group
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		data, _ := robotstxt.FromBytes(response.Body)
		if data != nil {
			group = data.FindGroup(policy.userAgent)
		}
	case response.StatusCode >= 400 && response.StatusCode < 500:
		group = allowAll()
	}
	if group == nil {
		group = disallowAll()
	}
	if policy.delay > 0 {
		timer := time.NewTimer(policy.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return originRules{}, ctx.Err()
		}
	}
	return originRules{group: group, validTill: time.Now().Add(robotsCacheLifetime)}, nil
}

func allowAll() *robotstxt.Group {
	data, _ := robotstxt.FromBytes(nil)
	return data.FindGroup("*")
}

func disallowAll() *robotstxt.Group {
	data, _ := robotstxt.FromBytes([]byte("User-agent: *\nDisallow: /\n"))
	return data.FindGroup("*")
}
