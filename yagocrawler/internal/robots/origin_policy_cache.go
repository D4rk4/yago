package robots

import (
	"context"
	"net/url"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/temoto/robotstxt"
	"golang.org/x/sync/singleflight"
)

const (
	robotsPolicyFreshness = 24 * time.Hour
	robotsRetryInterval   = 5 * time.Minute
)

type originPolicy struct {
	group     *robotstxt.Group
	expiresAt time.Time
}

type originPolicyRefresh struct {
	group       *robotstxt.Group
	lifetime    time.Duration
	unreachable bool
}

type originPolicyCache struct {
	entries   *lru.Cache[string, originPolicy]
	refreshes singleflight.Group
	now       func() time.Time
}

func newOriginPolicyCache(size int) (*originPolicyCache, error) {
	entries, err := lru.New[string, originPolicy](size)
	if err != nil {
		return nil, err
	}
	return &originPolicyCache{entries: entries, now: time.Now}, nil
}

func (f *RobotsAdmissionFetcher) group(ctx context.Context, target *url.URL) *robotstxt.Group {
	origin := robotsOrigin(target)
	if policy, ok := f.policies.entries.Get(origin); ok && policy.fresh(f.policies.now()) {
		return policy.group
	}
	resolved, _, _ := f.policies.refreshes.Do(origin, func() (any, error) {
		return f.refreshOriginPolicy(ctx, target, origin), nil
	})
	return resolved.(*robotstxt.Group)
}

func (f *RobotsAdmissionFetcher) refreshOriginPolicy(
	ctx context.Context,
	target *url.URL,
	origin string,
) *robotstxt.Group {
	now := f.policies.now()
	previous, found := f.policies.entries.Get(origin)
	if found && previous.fresh(now) {
		return previous.group
	}
	refreshed := f.fetchRobotsGroup(ctx, target)
	if refreshed.unreachable && found {
		refreshed.group = previous.group
	}
	f.policies.entries.Add(origin, originPolicy{
		group:     refreshed.group,
		expiresAt: f.policies.now().Add(refreshed.lifetime),
	})
	return refreshed.group
}

func (p originPolicy) fresh(now time.Time) bool {
	return now.Before(p.expiresAt)
}

func robotsOrigin(target *url.URL) string {
	return strings.ToLower(target.Scheme + "://" + target.Host)
}

func unreachableOriginPolicy() originPolicyRefresh {
	return originPolicyRefresh{
		group:       disallowAll(),
		lifetime:    robotsRetryInterval,
		unreachable: true,
	}
}
