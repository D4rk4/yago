package yagonode

import (
	"context"
	"fmt"
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
	"github.com/D4rk4/yago/yagonode/internal/siteicon"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// provisionAdminAuth builds the admin authentication service and applies the
// configured administrator credentials. It runs before the node is assembled so
// the same service (and its API-key store) can also back the public search
// surface's scoped authorization.
func provisionAdminAuth(
	ctx context.Context,
	config nodeConfig,
	storage *vault.Vault,
	observer adminauth.AuthObserver,
) (*adminauth.Service, error) {
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

	return service, nil
}

// guardAdminSurface mounts the admin authentication endpoints on the operations
// mux and returns it wrapped so that every path outside the open set requires a
// valid admin session. Health and readiness stay open for liveness probes, and
// the login and setup endpoints stay open so the first administrator can be
// created and can sign in.
func guardAdminSurface(service *adminauth.Service, opsMux *http.ServeMux) http.Handler {
	adminauth.Mount(opsMux, service)
	adminauth.MountHTML(opsMux, service)

	return service.Guard(
		[]string{
			pathHealth,
			pathReady,
			adminauth.PathLogin,
			adminauth.PathSetup,
			adminauth.PathLoginPage,
			adminauth.PathSetupPage,
			adminauth.PathAuthStylesheet,
			siteicon.Path,
			siteicon.LegacyPath,
		},
		map[string]adminauth.Scope{
			crawldispatch.PathCrawlDispatch: adminauth.ScopeCrawlWrite,
			pathSearchExplain:               adminauth.ScopeSearchRead,
		},
		opsMux,
	)
}
