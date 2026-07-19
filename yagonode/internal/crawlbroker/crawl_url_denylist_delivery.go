package crawlbroker

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

var errCrawlURLDenylistUnavailable = errors.New("crawl URL denylist unavailable")

type CrawlURLDenylistSource func() (yagocrawlcontract.CrawlURLDenylist, error)

type crawlURLDenylistDelivery struct {
	mu     sync.RWMutex
	source CrawlURLDenylistSource
}

func newCrawlURLDenylistDelivery() *crawlURLDenylistDelivery {
	return &crawlURLDenylistDelivery{source: func() (
		yagocrawlcontract.CrawlURLDenylist,
		error,
	) {
		return yagocrawlcontract.NewCrawlURLDenylist(nil, nil)
	}}
}

func (d *crawlURLDenylistDelivery) SetSource(source CrawlURLDenylistSource) {
	d.mu.Lock()
	d.source = source
	d.mu.Unlock()
}

func (d *crawlURLDenylistDelivery) Snapshot(
	knownRevision []byte,
) (*crawlrpc.CrawlURLDenylist, error) {
	d.mu.RLock()
	source := d.source
	d.mu.RUnlock()
	if source == nil {
		return nil, errCrawlURLDenylistUnavailable
	}
	policy, err := source()
	if err != nil {
		return nil, err
	}
	verified, err := yagocrawlcontract.ParseCrawlURLDenylist(
		policy.Revision,
		policy.ExactURLs,
		policy.Domains,
	)
	if err != nil {
		return nil, fmt.Errorf("validate crawl URL denylist: %w", err)
	}
	if bytes.Equal(knownRevision, verified.Revision) {
		return nil, nil
	}

	return &crawlrpc.CrawlURLDenylist{
		Revision:  append([]byte(nil), verified.Revision...),
		ExactUrls: append([]string(nil), verified.ExactURLs...),
		Domains:   append([]string(nil), verified.Domains...),
	}, nil
}
