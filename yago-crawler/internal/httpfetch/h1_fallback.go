package httpfetch

import (
	"strings"
	"sync"
	"time"
)

// HTTP/1.1 fallback for HTTP/2-hostile hosts (CRAWL-18). Bot-protection
// layers (Akamai, Cloudflare) fingerprint Go's TLS/h2 handshake and reset
// streams with INTERNAL_ERROR while serving the same page happily over
// HTTP/1.1 — YaCy never hits this because its Java client speaks h1. On an
// h2 stream failure the fetcher retries the request once through an
// h1-only client and remembers the host, so subsequent fetches skip the
// doomed h2 attempt for a while.

const (
	// h1DowngradeTTL is how long a host stays on the h1 path after an h2
	// stream failure; an hour outlives any crawl burst without pinning the
	// host to h1 forever.
	h1DowngradeTTL = time.Hour
	// h1DowngradeCap bounds the remembered hosts.
	h1DowngradeCap = 1024
)

// IsHTTP2StreamError recognizes transport errors where the server accepted
// the connection but killed the HTTP/2 exchange — the signature of protocol
// fingerprinting rather than an unreachable or misconfigured site. The http2
// error types live in an internal bundle, so matching is textual.
func IsHTTP2StreamError(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	if !strings.Contains(text, "http2") && !strings.Contains(text, "stream error") {
		return false
	}

	return strings.Contains(text, "stream error") ||
		strings.Contains(text, "GOAWAY") ||
		strings.Contains(text, "server sent") ||
		strings.Contains(text, "timeout awaiting response headers")
}

// hostDowngrades remembers hosts whose h2 exchanges fail, bounded and
// expiring so the map cannot grow without limit.
type hostDowngrades struct {
	mu      sync.Mutex
	expires map[string]time.Time
	now     func() time.Time
}

func newHostDowngrades() *hostDowngrades {
	return &hostDowngrades{expires: map[string]time.Time{}, now: time.Now}
}

// Mark puts one host on the h1 path; at capacity the expired entries are
// swept first and, if the map is still full, the mark is skipped — a full
// table only costs the doomed h2 attempt again.
func (d *hostDowngrades) Mark(host string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.expires) >= h1DowngradeCap {
		now := d.now()
		for existing, expiry := range d.expires {
			if now.After(expiry) {
				delete(d.expires, existing)
			}
		}
		if len(d.expires) >= h1DowngradeCap {
			return
		}
	}
	d.expires[host] = d.now().Add(h1DowngradeTTL)
}

// Active reports whether the host is currently downgraded.
func (d *hostDowngrades) Active(host string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	expiry, found := d.expires[host]
	if !found {
		return false
	}
	if d.now().After(expiry) {
		delete(d.expires, host)

		return false
	}

	return true
}
