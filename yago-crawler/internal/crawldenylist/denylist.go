package crawldenylist

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type snapshot struct {
	revision []byte
	urls     map[string]struct{}
	domains  map[string]struct{}
}

type Denylist struct {
	current atomic.Pointer[snapshot]
	ready   chan struct{}
	once    sync.Once
}

func New() *Denylist {
	return &Denylist{ready: make(chan struct{})}
}

func (d *Denylist) Apply(policy yagocrawlcontract.CrawlURLDenylist) error {
	verified, err := yagocrawlcontract.ParseCrawlURLDenylist(
		policy.Revision,
		policy.ExactURLs,
		policy.Domains,
	)
	if err != nil {
		return fmt.Errorf("validate crawl URL denylist: %w", err)
	}
	current := d.current.Load()
	if current != nil && bytes.Equal(current.revision, verified.Revision) {
		return nil
	}
	next := &snapshot{
		revision: append([]byte(nil), verified.Revision...),
		urls:     make(map[string]struct{}, len(verified.ExactURLs)),
		domains:  make(map[string]struct{}, len(verified.Domains)),
	}
	for _, exactURL := range verified.ExactURLs {
		next.urls[exactURL] = struct{}{}
	}
	for _, domain := range verified.Domains {
		next.domains[domain] = struct{}{}
	}
	d.current.Store(next)
	d.once.Do(func() { close(d.ready) })

	return nil
}

func (d *Denylist) Revision() []byte {
	current := d.current.Load()
	if current == nil {
		return nil
	}

	return append([]byte(nil), current.revision...)
}

func (d *Denylist) Ready() bool {
	return d.current.Load() != nil
}

func (d *Denylist) Wait(ctx context.Context) bool {
	select {
	case <-d.ready:
		return true
	case <-ctx.Done():
		return false
	}
}

func (d *Denylist) Blocks(rawURL string) bool {
	current := d.current.Load()
	if current == nil {
		return true
	}
	if _, blocked := current.urls[rawURL]; blocked {
		return true
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	for host != "" {
		if _, blocked := current.domains[host]; blocked {
			return true
		}
		separator := strings.IndexByte(host, '.')
		if separator < 0 {
			return false
		}
		host = host[separator+1:]
	}

	return false
}
