package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlformats"
)

// crawlFormatsSource adapts the persisted format toggles for the console's
// Crawler section.
type crawlFormatsSource struct {
	store *crawlformats.Store
}

// crawlFormatsAdmin exposes the runtime's format store to the console; a node
// without crawler integration hides the block.
func crawlFormatsAdmin(runtime crawlProcess) adminui.CrawlFormatsSource {
	provider, ok := runtime.(interface{ formatStore() *crawlformats.Store })
	if !ok || provider.formatStore() == nil {
		return nil
	}

	return crawlFormatsSource{store: provider.formatStore()}
}

func (s crawlFormatsSource) CurrentFormats(ctx context.Context) adminui.FormatSettings {
	toggles := s.store.Current(ctx)

	return adminui.FormatSettings{
		Text:     toggles.Text,
		XMLFeeds: toggles.XMLFeeds,
		PDF:      toggles.PDF,
		Office:   toggles.Office,
		Images:   toggles.Images,
		Audio:    toggles.Audio,
		Misc:     toggles.Misc,
		Archives: toggles.Archives,
	}
}

func (s crawlFormatsSource) SaveFormats(
	ctx context.Context,
	settings adminui.FormatSettings,
) error {
	//nolint:wrapcheck // the store already wraps its persistence error.
	return s.store.Set(ctx, yagocrawlcontract.FormatToggles{
		Text:     settings.Text,
		XMLFeeds: settings.XMLFeeds,
		PDF:      settings.PDF,
		Office:   settings.Office,
		Images:   settings.Images,
		Audio:    settings.Audio,
		Misc:     settings.Misc,
		Archives: settings.Archives,
	})
}
