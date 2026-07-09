package urlmeta

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

type transferURLEndpoint struct {
	identity nodeidentity.Identity
	intake   URLReceiver
	gate     *httpguard.IntakeGate
	accept   bool
}

func (e transferURLEndpoint) Serve(
	ctx context.Context,
	req yagoproto.TransferURLRequest,
) (yagoproto.TransferURLResponse, error) {
	resp := yagoproto.TransferURLResponse{}

	if !e.identity.NetworkMatches(req.NetworkName) {
		return resp, nil
	}
	// The operator turned the accept-remote-index capability off: refuse the
	// transfer outright, matching YaCy's transferURL with allowReceiveIndex
	// disabled.
	if !e.accept {
		resp.Result = yagoproto.ResultErrorNotGranted

		return resp, nil
	}
	// DHT-in load limitation (YaCy 1.6): shed metadata intake when all
	// admission slots are busy, using this endpoint's busy result.
	release, ok := e.gate.TryAcquire()
	if !ok {
		resp.Result = yagoproto.ResultErrorNotGranted

		return resp, nil
	}
	defer release()
	if req.YouAre != e.identity.Hash {
		resp.Result = yagoproto.ResultWrongTarget

		return resp, nil
	}

	receipt, err := e.intake.Receive(ctx, req.URLs)
	if err != nil {
		return yagoproto.TransferURLResponse{}, fmt.Errorf("receive url: %w", err)
	}

	if receipt.Busy {
		resp.Result = yagoproto.ResultErrorNotGranted
	} else {
		resp.Result = yagoproto.ResultOK
	}
	resp.Double = receipt.Double
	resp.ErrorURL = receipt.ErrorURL

	slog.DebugContext(ctx, "transfer url stored",
		slog.Bool("busy", receipt.Busy),
		slog.Int("doubleCount", receipt.Double),
		slog.Int("errorUrlCount", len(receipt.ErrorURL)),
	)

	return resp, nil
}
