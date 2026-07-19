package adminui

import (
	"context"
	"log/slog"
	"net/http"
)

const adminNetworkSelfTestFailureMessage = "admin public endpoint self-test failed"

type networkSelfTestPage struct {
	status *NetworkStatus
	notice string
	err    string
}

type networkSelfTestPageKey struct{}

func networkSelfTestPageFromContext(ctx context.Context) networkSelfTestPage {
	page, _ := ctx.Value(networkSelfTestPageKey{}).(networkSelfTestPage)

	return page
}

func (c *Console) handleNetworkSelfTest(w http.ResponseWriter, r *http.Request) {
	if c.networkSelfTest == nil || c.network == nil {
		http.NotFound(w, r)

		return
	}
	status, err := c.networkSelfTest.TestPublicEndpoint(r.Context())
	page := networkSelfTestPage{}
	if err != nil {
		slog.WarnContext(r.Context(), adminNetworkSelfTestFailureMessage, slog.Any("error", err))
		page.err = "The public endpoint self-test could not run."
	} else {
		page.status = &status
		if status.PublicReachable {
			page.notice = "The fresh self-test confirmed that the public YaCy endpoint is reachable."
		} else {
			page.err = "The fresh self-test could not reach the public YaCy endpoint."
		}
	}
	ctx := context.WithValue(r.Context(), networkSelfTestPageKey{}, page)
	c.handleNetwork(w, r.WithContext(ctx))
}
