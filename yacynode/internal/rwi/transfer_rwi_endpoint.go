package rwi

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacyproto"
)

const transferRWIDefaultPause = 60000

type transferRWIEndpoint struct {
	identity nodeidentity.Identity
	intake   PostingReceiver
}

func (e transferRWIEndpoint) Serve(
	ctx context.Context,
	req yacyproto.TransferRWIRequest,
) (yacyproto.TransferRWIResponse, error) {
	resp := yacyproto.TransferRWIResponse{Pause: transferRWIDefaultPause}

	if !e.identity.NetworkMatches(req.NetworkName) {
		resp.Result = yacyproto.ResultNotAuthentified

		return resp, nil
	}
	if req.MissingWordCountField() {
		resp.Result = yacyproto.ResultMissingWordC

		return resp, nil
	}
	if req.MissingEntryCountField() {
		resp.Result = yacyproto.ResultMissingEntryC

		return resp, nil
	}
	if req.MissingIndexesField() {
		resp.Result = yacyproto.ResultMissingIndexes

		return resp, nil
	}
	if req.YouAre != e.identity.Hash {
		resp.Result = yacyproto.ResultWrongTarget
		resp.Pause = 0

		return resp, nil
	}

	slog.DebugContext(ctx, "transfer rwi request accepted",
		slog.Int("wordCount", req.WordCount),
		slog.Int("entryCount", req.EntryCount),
		slog.Int("acceptedEntryCount", len(req.Indexes)),
	)

	receipt, err := e.intake.Receive(ctx, req.Indexes)
	if err != nil {
		return yacyproto.TransferRWIResponse{}, fmt.Errorf("receive rwi: %w", err)
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

	return resp, nil
}
