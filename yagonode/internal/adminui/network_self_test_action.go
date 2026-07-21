package adminui

import (
	"context"
	"log/slog"
	"net/http"
)

const adminNetworkSelfTestFailureMessage = "admin public endpoint reachability check failed"

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
		page.err = "The public endpoint reachability check could not run."
	} else {
		page.status = &status
		page.notice, page.err = publicReachabilityResult(status)
	}
	ctx := context.WithValue(r.Context(), networkSelfTestPageKey{}, page)
	c.handleNetwork(w, r.WithContext(ctx))
}

func publicReachabilityResult(status NetworkStatus) (string, string) {
	if !status.PublicReachabilityKnown {
		if status.PublicReachabilitySource == PublicReachabilityDerivedProbe {
			return "", "No external observation is available; a local compatibility query cannot establish public reachability."
		}

		return "", "No current public endpoint reachability observation is available."
	}
	if status.PublicReachabilitySource == PublicReachabilityPeerBackPing {
		if status.PublicReachable {
			return "A current peer back-ping confirmed that the public YaCy endpoint is reachable.", ""
		}

		return "", "Current peer back-ping evidence reports that the public YaCy endpoint is unreachable."
	}
	if status.PublicReachabilitySource == PublicReachabilityPinnedProbe {
		if status.PublicReachable {
			return "The configured direct YaCy query confirmed that the public endpoint is reachable.", ""
		}

		return "", "The configured direct YaCy query could not reach the public endpoint."
	}
	if status.PublicReachable {
		return "The current reachability check confirmed that the public YaCy endpoint is reachable.", ""
	}

	return "", "The current reachability check could not reach the public YaCy endpoint."
}
