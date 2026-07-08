package urlmeta

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodeidentity"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
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

	if !e.identity.Addresses(req.NetworkName, req.YouAre) {
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
