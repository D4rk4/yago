package adminauth

import (
	"context"
	"strings"
)

// Setup wizard modes: how the operator wants this node to face the world.
const (
	SetupModeLocal      = "local"
	SetupModePublicPeer = "peer"
	SetupModeSearchNode = "search"
)

// SetupChoices carries the first-run wizard's node-mode selections alongside
// the administrator account.
type SetupChoices struct {
	Mode          string
	AdvertiseHost string
	Seedlists     string
	WebFallback   string
}

// SetupDefaults prefills the wizard form: the autodetected advertise host, the
// effective seedlists, and the current web-fallback privacy mode.
type SetupDefaults struct {
	AdvertiseHost string
	Seedlists     string
	WebFallback   string
}

// SetupApplier persists the wizard choices as runtime settings. A nil applier
// keeps the plain administrator-only setup page.
type SetupApplier func(ctx context.Context, choices SetupChoices) error

// ConfigureSetupWizard arms the first-run page with the node-mode wizard.
func (s *Service) ConfigureSetupWizard(defaults SetupDefaults, applier SetupApplier) {
	s.wizardDefaults = defaults
	s.wizardApply = applier
}

// wizardChoices reads the wizard fields of the setup form.
func wizardChoices(form func(string) string) SetupChoices {
	mode := strings.TrimSpace(form("mode"))
	switch mode {
	case SetupModePublicPeer, SetupModeSearchNode:
	default:
		mode = SetupModeLocal
	}

	return SetupChoices{
		Mode:          mode,
		AdvertiseHost: strings.TrimSpace(form("advertise_host")),
		Seedlists:     strings.TrimSpace(form("seedlists")),
		WebFallback:   strings.TrimSpace(form("web_fallback")),
	}
}
