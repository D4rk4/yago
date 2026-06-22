package urlmeta

import (
	"log/slog"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type transferURLEndpoint struct {
	guard   httpguard.RequestGuard
	respond httpguard.WireResponder
	intake  urlIntake
}

func (e transferURLEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	form, ctx, cancel, ok := e.guard.Parse(w, r, yacyproto.TransferURLEndpointMethods)
	if !ok {
		return
	}
	defer cancel()

	req, err := yacyproto.ParseTransferURLRequest(ctx, form)
	if err != nil {
		httpguard.FailBadRequest(ctx, w, err)

		return
	}

	resp := yacyproto.TransferURLResponse{}

	if !e.guard.NetworkMatches(form) || !e.guard.YouAreMatches(req.YouAre) {
		resp.Result = yacyproto.ResultWrongTarget
		e.respond.Write(ctx, w, resp.Encode())

		return
	}

	receipt, err := e.intake.Receive(ctx, req.URLs)
	if err != nil {
		httpguard.FailInternal(ctx, w, "receive failed", err)

		return
	}

	if receipt.Busy {
		resp.Result = yacyproto.ResultErrorNotGranted
	} else {
		resp.Result = yacyproto.ResultOK
	}
	resp.Double = receipt.Double
	resp.ErrorURL = receipt.ErrorURL

	slog.DebugContext(ctx, "transfer url stored",
		slog.Bool("busy", receipt.Busy),
		slog.Int("doubleCount", receipt.Double),
		slog.Int("errorUrlCount", len(receipt.ErrorURL)),
	)
	e.respond.Write(ctx, w, resp.Encode())
}
