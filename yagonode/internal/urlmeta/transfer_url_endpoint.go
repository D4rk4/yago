package urlmeta

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

type transferURLEndpoint struct {
	identity nodeidentity.Identity
	intake   URLReceiver
}

func (e transferURLEndpoint) Serve(
	ctx context.Context,
	req yagoproto.TransferURLRequest,
) (yagoproto.TransferURLResponse, error) {
	resp := yagoproto.TransferURLResponse{}

	if !e.identity.NetworkMatches(req.NetworkName) {
		return resp, nil
	}
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
