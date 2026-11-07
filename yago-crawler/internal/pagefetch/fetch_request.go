package pagefetch

import (
	"net/url"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type FetchRequest struct {
	Target     *url.URL
	Validators yagocrawlcontract.HTTPValidators
}

func (r FetchRequest) Unconditional() FetchRequest {
	return FetchRequest{Target: r.Target}
}
