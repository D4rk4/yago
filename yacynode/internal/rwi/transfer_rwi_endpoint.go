package rwi

import (
	"log/slog"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type transferRWIEndpoint struct {
	guard  httpguard.RequestGuard
	status RuntimeStatus
	intake postingIntake
}

func (e transferRWIEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	form, ctx, cancel, ok := e.guard.Parse(w, r, yacyproto.TransferRWIEndpointMethods)
	if !ok {
		return
	}
	defer cancel()

	req, err := yacyproto.ParseTransferRWIRequest(ctx, form)
	if err != nil {
		httpguard.FailBadRequest(ctx, w, err)

		return
	}

	snapshot := e.status.Snapshot(ctx)
	resp := yacyproto.TransferRWIResponse{
		ResponseHeader: yacyproto.ResponseHeader{
			Version: snapshot.Version,
			Uptime:  snapshot.Uptime,
		},
	}

	if !e.guard.NetworkMatches(form) || !e.guard.YouAreMatches(req.YouAre) {
		resp.Result = yacyproto.ResultWrongTarget
		httpguard.WriteWireMessage(ctx, w, resp.Encode())

		return
	}

	slog.DebugContext(ctx, "transfer rwi request accepted",
		slog.Int("wordCount", req.WordCount),
		slog.Int("entryCount", req.EntryCount),
		slog.Int("acceptedEntryCount", len(req.Indexes)),
	)

	receipt, err := e.intake.Receive(ctx, req.Indexes)
	if err != nil {
		httpguard.FailInternal(ctx, w, "receive failed", err)

		return
	}

	if receipt.Busy {
		resp.Result = yacyproto.ResultBusy
	} else {
		resp.Result = yacyproto.ResultOK
	}
	resp.Pause = receipt.Pause
	resp.UnknownURL = receipt.UnknownURL

	slog.DebugContext(ctx, "transfer rwi stored",
		slog.Bool("busy", receipt.Busy),
		slog.Int("unknownUrlCount", len(receipt.UnknownURL)),
	)
	httpguard.WriteWireMessage(ctx, w, resp.Encode())
}
