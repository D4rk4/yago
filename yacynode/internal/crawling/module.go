package crawling

import (
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
)

type Module struct {
	Endpoint http.Handler
}

func New(guard httpguard.RequestGuard, status RuntimeStatus) Module {
	return Module{
		Endpoint: crawlReceiptEndpoint{guard: guard, status: status},
	}
}
