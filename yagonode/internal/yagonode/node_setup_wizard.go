package yagonode

import (
	"context"
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

// configureSetupWizard arms the first-run setup page with the node-mode
// wizard: prefilled critical settings, an applier that persists the choices
// through the same validated runtime-settings path the console uses, and a
// mandatory restart once they are saved — several of them (portal listener,
// seedlist bootstrap, web-fallback assembly) only take effect at boot.
func configureSetupWizard(
	service *adminauth.Service,
	settings adminui.SettingsSource,
	config nodeConfig,
	restart func(),
) {
	if settings == nil {
		return
	}
	service.ConfigureSetupWizard(adminauth.SetupDefaults{
		PeerName:      config.Name,
		AdvertiseHost: config.AdvertiseHost,
		Seedlists:     wizardSeedlistDefault(config.SeedlistURLs),
		WebFallback:   string(config.WebFallback.Privacy),
	}, setupWizardApplier(settings))
	if restart != nil {
		service.ConfigureSetupRestart(restart)
	}
}

// defaultWizardSeedlistURLs seeds the first-run wizard when the node has no
// seedlist configured yet, so an operator setting up a public node starts from
// the canonical YaCy public seed servers instead of an empty box (they can edit
// or clear them before applying). These mirror docker-compose.yml.example.
var defaultWizardSeedlistURLs = []string{
	"http://sixcooler.de/yacy/seed.txt",
	"https://sonst.mifritscher.de/yacy/seed.txt",
	"http://5.45.105.16/yacyseed",
	"http://yacy.v16.de/seed/seed.txt",
	"https://frank-siebert.de/seed.txt",
	"http://seedlist.wertewesten.net/seed.txt",
}

// wizardSeedlistDefault prefills the wizard's seedlist field: the node's
// configured seeds when it has any, otherwise the canonical public defaults so
// the field is never empty on a fresh install (which would leave the node with
// no peers to bootstrap from).
func wizardSeedlistDefault(configured []string) string {
	if len(configured) > 0 {
		return strings.Join(configured, ",")
	}

	return strings.Join(defaultWizardSeedlistURLs, ",")
}

// setupWizardApplier maps the wizard's choices onto runtime settings. The
// local-only mode keeps the public portal off (the default); public modes
// record the advertise host and seedlists, and the search-node mode also
// switches the portal on.
func setupWizardApplier(settings adminui.SettingsSource) adminauth.SetupApplier {
	return func(ctx context.Context, choices adminauth.SetupChoices) error {
		changes := []adminui.SettingsChange{
			{Key: "web.fallback.privacy", Value: choices.WebFallback},
		}
		if choices.PeerName != "" {
			changes = append(changes,
				adminui.SettingsChange{Key: "peer.name", Value: choices.PeerName})
		}
		if choices.Mode != adminauth.SetupModeLocal {
			changes = append(changes,
				adminui.SettingsChange{Key: "network.advertise.host", Value: choices.AdvertiseHost},
				adminui.SettingsChange{Key: "network.seedlists", Value: choices.Seedlists},
			)
		}
		changes = append(changes, adminui.SettingsChange{
			Key:   settingKeyPublicSearchPortal,
			Value: formatSettingBool(choices.Mode == adminauth.SetupModeSearchNode),
		})
		for _, change := range changes {
			result, err := settings.Update(ctx, change)
			if err != nil {
				return fmt.Errorf("apply %s: %w", change.Key, err)
			}
			if !result.OK {
				return fmt.Errorf("apply %s: %s", change.Key, result.Message)
			}
		}

		return nil
	}
}
