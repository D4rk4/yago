package worker

import (
	"errors"
	"net/url"
	"strings"
	"sync"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type crawlerURLDenylist struct {
	mutex   sync.RWMutex
	revision []byte
	exact   map[string]struct{}
	domains []string
}

func newCrawlerURLDenylist() *crawlerURLDenylist {
	list, _ := yagocrawlcontract.NewCrawlURLDenylist(nil, nil)
	return &crawlerURLDenylist{
		revision: append([]byte(nil), list.Revision...),
		exact:    make(map[string]struct{}),
	}
}

func (list *crawlerURLDenylist) Revision() []byte {
	list.mutex.RLock()
	revision := append([]byte(nil), list.revision...)
	list.mutex.RUnlock()
	return revision
}

func (list *crawlerURLDenylist) Replace(message *crawlrpc.CrawlURLDenylist) error {
	if message == nil {
		return errors.New("crawler URL denylist is required")
	}
	parsed, err := yagocrawlcontract.ParseCrawlURLDenylist(
		message.GetRevision(),
		message.GetExactUrls(),
		message.GetDomains(),
	)
	if err != nil {
		return err
	}
	exact := make(map[string]struct{}, len(parsed.ExactURLs))
	for _, rawURL := range parsed.ExactURLs {
		canonical, valid := yagocrawlcontract.CanonicalURL(rawURL)
		if !valid {
			return errors.New("crawler URL denylist contains an invalid URL")
		}
		exact[canonical] = struct{}{}
	}
	list.mutex.Lock()
	list.revision = append(list.revision[:0], parsed.Revision...)
	list.exact = exact
	list.domains = append(list.domains[:0], parsed.Domains...)
	list.mutex.Unlock()
	return nil
}

func (list *crawlerURLDenylist) Blocks(rawURL string) bool {
	canonical, valid := yagocrawlcontract.CanonicalURL(rawURL)
	if !valid {
		return true
	}
	parsed, err := url.Parse(canonical)
	if err != nil || parsed.Hostname() == "" {
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	list.mutex.RLock()
	_, exact := list.exact[canonical]
	blocked := exact
	if !blocked {
		for _, domain := range list.domains {
			if host == domain || strings.HasSuffix(host, "."+domain) {
				blocked = true
				break
			}
		}
	}
	list.mutex.RUnlock()
	return blocked
}
