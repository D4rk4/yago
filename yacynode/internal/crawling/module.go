package crawling

import (
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
)

type Module struct {
	Endpoint http.Handler
}

func New(guard httpguard.RequestGuard, respond httpguard.WireResponder) Module {
	return Module{
		Endpoint: crawlReceiptEndpoint{guard: guard, respond: respond},
	}
}
