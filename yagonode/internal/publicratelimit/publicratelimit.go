// Package publicratelimit throttles the anonymous public search surfaces the
// way YaCy's search.public.max.access tiers do: per-client sliding windows at
// three second, one minute, and ten minute horizons, with raised limits for
// authenticated callers (a valid bearer key or the local operator). Cheap
// asset routes (favicons, thumbnails) are not throttled here — only the
// search-serving paths.
package publicratelimit

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Tiers holds the request budgets per window (YaCy: search.public.max.access).
type Tiers struct {
	Per3Seconds  int
	PerMinute    int
	Per10Minutes int
}

// DefaultPublicTiers throttles anonymous searchers.
func DefaultPublicTiers() Tiers {
	return Tiers{Per3Seconds: 10, PerMinute: 60, Per10Minutes: 300}
}

// authenticatedMultiplier raises every tier for authenticated callers.
const authenticatedMultiplier = 10

const maxTrackedClients = 4096

type windowCounts struct {
	stamps []time.Time
}

// Limiter tracks per-client request timestamps over the largest window.
type Limiter struct {
	mu      sync.Mutex
	clients map[string]*windowCounts
	tiers   Tiers
	now     func() time.Time
}

// NewLimiter builds a limiter over the given tiers.
func NewLimiter(tiers Tiers) *Limiter {
	return &Limiter{
		clients: make(map[string]*windowCounts),
		tiers:   tiers,
		now:     time.Now,
	}
}

// Allow records one request for the client and reports whether it fits the
// tiers; when it does not, retryAfter suggests the earliest sensible retry.
func (l *Limiter) Allow(client string, authenticated bool) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	entry := l.clients[client]
	if entry == nil {
		if len(l.clients) >= maxTrackedClients {
			l.evictStale(now)
		}
		entry = &windowCounts{}
		l.clients[client] = entry
	}
	horizon := now.Add(-10 * time.Minute)
	kept := entry.stamps[:0]
	for _, stamp := range entry.stamps {
		if stamp.After(horizon) {
			kept = append(kept, stamp)
		}
	}
	entry.stamps = kept

	multiplier := 1
	if authenticated {
		multiplier = authenticatedMultiplier
	}
	if !l.fits(entry.stamps, now, 3*time.Second, l.tiers.Per3Seconds*multiplier) {
		return false, 3 * time.Second
	}
	if !l.fits(entry.stamps, now, time.Minute, l.tiers.PerMinute*multiplier) {
		return false, time.Minute
	}
	if !l.fits(entry.stamps, now, 10*time.Minute, l.tiers.Per10Minutes*multiplier) {
		return false, 10 * time.Minute
	}
	entry.stamps = append(entry.stamps, now)

	return true, 0
}

func (l *Limiter) fits(stamps []time.Time, now time.Time, window time.Duration, budget int) bool {
	if budget <= 0 {
		return false
	}
	edge := now.Add(-window)
	count := 0
	for _, stamp := range stamps {
		if stamp.After(edge) {
			count++
		}
	}

	return count < budget
}

// evictStale drops clients whose entire history left the largest window.
func (l *Limiter) evictStale(now time.Time) {
	horizon := now.Add(-10 * time.Minute)
	for client, entry := range l.clients {
		stale := true
		for _, stamp := range entry.stamps {
			if stamp.After(horizon) {
				stale = false

				break
			}
		}
		if stale {
			delete(l.clients, client)
		}
	}
}

// Authenticated reports whether the request carries raised-limit credentials.
type Authenticated func(r *http.Request) bool

// Wrap throttles the search-serving paths of next; other paths pass through.
func Wrap(next http.Handler, limiter *Limiter, authenticated Authenticated) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !throttledPath(r) {
			next.ServeHTTP(w, r)

			return
		}
		raised := clientIsLocal(r)
		if !raised && authenticated != nil {
			raised = authenticated(r)
		}
		ok, retryAfter := limiter.Allow(clientKey(r), raised)
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			http.Error(w, "search rate limit exceeded", http.StatusTooManyRequests)

			return
		}
		next.ServeHTTP(w, r)
	})
}

// throttledPath selects the search-serving routes: the yacysearch family,
// suggestions, and portal queries (the bare portal page stays free).
func throttledPath(r *http.Request) bool {
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/yacysearch."):
		return true
	case path == "/suggest.json" || path == "/opensearch/suggest":
		return true
	case path == "/" && r.URL.Query().Get("q") != "":
		return true
	default:
		return false
	}
}

// clientKey identifies the caller by remote IP.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

// clientIsLocal reports whether the request arrived from the local host, which
// YaCy grants raised limits.
func clientIsLocal(r *http.Request) bool {
	ip := net.ParseIP(clientKey(r))

	return ip != nil && ip.IsLoopback()
}
