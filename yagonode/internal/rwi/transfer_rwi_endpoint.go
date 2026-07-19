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
	gate              *httpguard.IntakeGate
	batchCap          int
	pauseMilliseconds int
	accept            bool
}

func (e transferRWIEndpoint) Serve(
	ctx context.Context,
	req yagoproto.TransferRWIRequest,
) (yagoproto.TransferRWIResponse, error) {
	resp := yagoproto.TransferRWIResponse{Pause: transferRWIDefaultPause}

	if !e.identity.Authenticates(
		req.NetworkName,
		req.Key,
		req.Iam.String(),
		req.MagicMD5,
	) {
		resp.Result = yagoproto.ResultNotAuthentified

		return resp, nil
	}
	// The operator turned the accept-remote-index capability off: refuse the
	// transfer outright, matching YaCy's transferRWI with allowReceiveIndex
	// disabled, so senders mark this peer as not accepting and move on.
	if !e.accept {
		resp.Result = yagoproto.ResultNotGranted

		return resp, nil
	}
	if result, missing := missingFieldResult(req); missing {
		resp.Result = result

		return resp, nil
	}
	// A single peer transfer carrying more postings than a batch may hold is a
	// swarm-transfer admission limit, not a storage failure; answer Busy so the
	// sender backs off and retries a smaller batch. A non-positive cap disables
	// the limit. Local crawl ingest bypasses this endpoint and is never
	// size-capped.
	if req.ExceedsEntryLimit() || e.batchCap > 0 && len(req.Indexes) > e.batchCap {
		resp.Result = yagoproto.ResultBusy
		resp.Pause = e.pauseMilliseconds

		return resp, nil
	}
	// YaCy rejects inbound RWI under high system load without details
	// (transferRWI.java "too high load"); admission slots bound the same harm.
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
