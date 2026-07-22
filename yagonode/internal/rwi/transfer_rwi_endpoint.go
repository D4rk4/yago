package rwi

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

const transferRWIDefaultPause = 60000

// missingFieldResult reports the YaCy preflight answer for a transfer that
// lacks a required field, in the order transferRWI.java checks them.
func missingFieldResult(
	req yagoproto.TransferRWIRequest,
) (yagoproto.TransferRWIResult, bool) {
	switch {
	case req.MissingWordCountField():
		return yagoproto.ResultMissingWordC, true
	case req.MissingEntryCountField():
		return yagoproto.ResultMissingEntryC, true
	case req.MissingIndexesField():
		return yagoproto.ResultMissingIndexes, true
	}

	return "", false
}

type transferRWIEndpoint struct {
	identity          nodeidentity.Identity
	intake            PostingReceiver
	senders           SenderDirectory
	gate              *httpguard.IntakeGate
	batchCap          int
	pauseMilliseconds int
	accept            bool
}

func (e transferRWIEndpoint) Serve(
	ctx context.Context,
	req yagoproto.TransferRWIRequest,
) (yagoproto.TransferRWIResponse, error) {
	resp, admitted := e.preflight(req)
	if !admitted {
		return resp, nil
	}
	release, ok := e.gate.TryAcquire()
	if !ok {
		resp.Result = yagoproto.ResultTooHighLoad

		return resp, nil
	}
	defer release()
	if req.YouAre != e.identity.Hash {
		resp.Result = yagoproto.ResultWrongTarget
		resp.Pause = 0

		return resp, nil
	}
	if _, known := e.senders.PeerByHash(ctx, req.Iam); !known {
		resp.Result = yagoproto.ResultNotGranted

		return resp, nil
	}

	return e.receive(ctx, req, resp)
}

func (e transferRWIEndpoint) preflight(
	req yagoproto.TransferRWIRequest,
) (yagoproto.TransferRWIResponse, bool) {
	resp := yagoproto.TransferRWIResponse{Pause: transferRWIDefaultPause}
	if !e.identity.Authenticates(
		req.NetworkName,
		req.NetworkNamePresent,
		req.Key,
		req.Iam.String(),
		req.MagicMD5,
	) {
		resp.Result = yagoproto.ResultNotAuthentified

		return resp, false
	}
	if !e.accept {
		resp.Result = yagoproto.ResultNotGranted

		return resp, false
	}
	if result, missing := missingFieldResult(req); missing {
		resp.Result = result

		return resp, false
	}
	if req.ExceedsEntryLimit() || e.batchCap > 0 && len(req.Indexes) > e.batchCap {
		resp.Result = yagoproto.ResultBusy
		resp.Pause = e.pauseMilliseconds

		return resp, false
	}

	return resp, true
}

func (e transferRWIEndpoint) receive(
	ctx context.Context,
	req yagoproto.TransferRWIRequest,
	resp yagoproto.TransferRWIResponse,
) (yagoproto.TransferRWIResponse, error) {
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
