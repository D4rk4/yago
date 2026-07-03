package yagonode

import (
	"context"
	"fmt"
	"net/http"

	"github.com/D4rk4/yago/yacynode/internal/adminauth"
	"github.com/D4rk4/yago/yacynode/internal/crawldispatch"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

// guardAdminSurface provisions the admin authentication service, mounts its
// endpoints on the operations mux, and returns the mux wrapped so that every
// path outside the open set requires a valid admin session. Health and
// readiness stay open for liveness probes, and the login and setup endpoints
// stay open so the first administrator can be created and can sign in.
func guardAdminSurface(
	ctx context.Context,
	config nodeConfig,
	storage *vault.Vault,
	observer adminauth.AuthObserver,
	opsMux *http.ServeMux,
) (http.Handler, error) {
	service, err := adminauth.New(storage, adminauth.Config{Observer: observer})
	if err != nil {
		return nil, fmt.Errorf("build admin auth: %w", err)
	}
	if err := service.BootstrapFromEnv(
		ctx,
		config.Admin.Username,
		config.Admin.Password,
	); err != nil {
		return nil, fmt.Errorf("provision admin: %w", err)
	}
	adminauth.Mount(opsMux, service)

	return service.Guard(
		[]string{pathHealth, pathReady, adminauth.PathLogin, adminauth.PathSetup},
		map[string]adminauth.Scope{
			crawldispatch.PathCrawlDispatch: adminauth.ScopeCrawlWrite,
			pathSearchExplain:               adminauth.ScopeSearchRead,
		},
		opsMux,
	), nil
}
