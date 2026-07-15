package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

type adminPublicSearchStatusSource struct {
	toggles *runtimeToggles
	enabled bool
	baseURL string
}

func newAdminPublicSearchStatusSource(
	toggles *runtimeToggles,
	config nodeConfig,
) adminPublicSearchStatusSource {
	return adminPublicSearchStatusSource{
		toggles: toggles,
		enabled: config.PublicSearchUIEnabled,
		baseURL: config.PublicBaseURL,
	}
}

func (s adminPublicSearchStatusSource) PublicSearchStatus(
	context.Context,
) adminui.PublicSearchStatus {
	if s.toggles == nil {
		return adminui.PublicSearchStatus{Enabled: s.enabled, BaseURL: s.baseURL}
	}

	return adminui.PublicSearchStatus{
		Enabled: s.toggles.PortalEnabled(),
		BaseURL: s.toggles.PublicBaseURL(),
	}
}
