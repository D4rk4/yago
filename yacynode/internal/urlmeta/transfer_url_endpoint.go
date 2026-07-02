package urlmeta

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacyproto"
)

type transferURLEndpoint struct {
	identity nodeidentity.Identity
	intake   URLReceiver
}

func (e transferURLEndpoint) Serve(
	ctx context.Context,
	req yacyproto.TransferURLRequest,
) (yacyproto.TransferURLResponse, error) {
	resp := yacyproto.TransferURLResponse{}

	if !e.identity.NetworkMatches(req.NetworkName) {
		return resp, nil
	}
	if req.YouAre != e.identity.Hash {
		resp.Result = yacyproto.ResultWrongTarget

		return resp, nil
	}

	receipt, err := e.intake.Receive(ctx, req.URLs)
	if err != nil {
		return yacyproto.TransferURLResponse{}, fmt.Errorf("receive url: %w", err)
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

	return resp, nil
}
