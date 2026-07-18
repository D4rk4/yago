package crawlorder

import (
	"fmt"
	"net/url"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const msgOrderValidationFailed = "crawl order validation failed"

func validateCrawlOrderRequests(requests []yagocrawlcontract.CrawlRequest) error {
	for index, request := range requests {
		if _, ok := yagocrawlcontract.NormalizeCrawlRequestMode(request.Mode); !ok {
			return fmt.Errorf("request %d has unsupported mode %q", index, request.Mode)
		}
		target, err := url.Parse(request.URL)
		if err != nil || target.Host == "" ||
			(target.Scheme != "http" && target.Scheme != "https") {
			return fmt.Errorf("request %d has invalid URL", index)
		}
	}

	return nil
}
