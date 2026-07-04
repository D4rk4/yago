package rwi

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

const transferRWIDefaultPause = 60000

type transferRWIEndpoint struct {
	identity nodeidentity.Identity
	intake   PostingReceiver
}

func (e transferRWIEndpoint) Serve(
	ctx context.Context,
	req yagoproto.TransferRWIRequest,
) (yagoproto.TransferRWIResponse, error) {
	resp := yagoproto.TransferRWIResponse{Pause: transferRWIDefaultPause}

	if !e.identity.NetworkMatches(req.NetworkName) {
		resp.Result = yagoproto.ResultNotAuthentified

		return resp, nil
	}
	if req.MissingWordCountField() {
		resp.Result = yagoproto.ResultMissingWordC

		return resp, nil
	}
	if req.MissingEntryCountField() {
		resp.Result = yagoproto.ResultMissingEntryC

		return resp, nil
	}
	if req.MissingIndexesField() {
		resp.Result = yagoproto.ResultMissingIndexes

		return resp, nil
	}
	if req.YouAre != e.identity.Hash {
		resp.Result = yagoproto.ResultWrongTarget
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
		return yagoproto.TransferRWIResponse{}, fmt.Errorf("receive rwi: %w", err)
	}

	if receipt.Busy {
		resp.Result = yagoproto.ResultBusy
	} else {
		resp.Result = yagoproto.ResultOK
	}
	resp.Pause = receipt.Pause
	resp.UnknownURL = receipt.UnknownURL

	slog.DebugContext(ctx, "transfer rwi stored",
		slog.Bool("busy", receipt.Busy),
		slog.Int("unknownUrlCount", len(receipt.UnknownURL)),
	)

	return resp, nil
}
