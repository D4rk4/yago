package urlmeta

import (
	"context"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type Module struct {
	Directory URLDirectory
	Evictor   URLEvictor
	Endpoint  http.Handler
	intake    urlIntake
}

func (m Module) Intake(ctx context.Context, rows []yacymodel.URIMetadataRow) (Receipt, error) {
	return m.intake.Receive(ctx, rows)
}

func New(
	vault *boltvault.Vault,
	guard httpguard.RequestGuard,
	status RuntimeStatus,
) (Module, error) {
	collection, err := registerCollection(vault)
	if err != nil {
		return Module{}, err
	}

	intake := urlIntake{vault: vault, collection: collection}

	// FIXME: register the transferURL handler with a shared router here (mirroring
	// registerCollection) instead of returning it in Module.Endpoint for cmd to mount.
	return Module{
		Directory: urlDirectory{vault: vault, collection: collection},
		Evictor:   urlEvictor{vault: vault, collection: collection},
		Endpoint:  transferURLEndpoint{guard: guard, status: status, intake: intake},
		intake:    intake,
	}, nil
}
