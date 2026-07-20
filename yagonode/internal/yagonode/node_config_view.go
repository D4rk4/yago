package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

type configSource struct {
	view adminui.ConfigView
}

// newConfigSource snapshots the effective configuration into a display view. The
// configuration is fixed at startup, so the view is built once.
func newConfigSource(config nodeConfig) *configSource {
	return &configSource{view: buildConfigView(config)}
}

func (s *configSource) Config(context.Context) adminui.ConfigView {
	return s.view
}

func buildConfigView(config nodeConfig) adminui.ConfigView {
	return adminui.ConfigView{Groups: []adminui.ConfigGroup{
		{Title: "Node", Settings: []adminui.ConfigSetting{
			{Name: "Peer name", Value: config.Name},
			{Name: "Network", Value: config.NetworkName},
			{Name: "Announce interval", Value: config.AnnounceInterval.String()},
			{Name: "Greets per cycle", Value: fmt.Sprintf("%d", config.GreetsPerCycle)},
		}},
		{Title: "Listeners", Settings: []adminui.ConfigSetting{
			{Name: "Peer listener", Value: config.PeerAddr},
			{Name: "Operations listener", Value: config.OpsAddr},
			{Name: "Public search listener", Value: publicListenerDisplay(config.PublicAddr)},
			{
				Name:  "Advertised endpoint",
				Value: fmt.Sprintf("%s:%d", config.AdvertiseHost, config.AdvertisePort),
			},
		}},
		{Title: "Storage", Settings: []adminui.ConfigSetting{
			{Name: "Data directory", Value: config.DataDir},
			{Name: "Storage path", Value: config.StoragePath},
			{Name: "Search index path", Value: config.SearchIndexPath},
			{Name: "Storage quota", Value: storageQuota(config.StorageQuotaByte)},
		}},
		{Title: "Search", Settings: []adminui.ConfigSetting{
			{Name: "Scoped-only API keys", Value: yesNo(config.SearchRequireAPIKey)},
			{Name: "Legacy static search API key", Value: redactedSecret(config.SearchAPIKey)},
			{Name: "Public search portal", Value: enabledDisabled(config.PublicSearchUIEnabled)},
			{
				Name:  "Web fallback",
				Value: webFallbackPrivacyDisplay(effectiveWebFallbackPrivacy(config.WebFallback)),
			},
		}},
		{Title: "Network policy", Settings: []adminui.ConfigSetting{
			{
				Name:  "YaCy network authentication",
				Value: string(config.NetworkAuthenticationMode),
			},
			{
				Name:  "Shared network secret",
				Value: redactedSecret(config.NetworkAuthenticationSecret),
			},
			{
				Name:  "Remote crawl delegation",
				Value: enabledDisabled(config.RemoteCrawl.Enabled),
			},
			{
				Name:  "Remote crawl trusted peers",
				Value: fmt.Sprintf("%d", len(config.RemoteCrawl.TrustedPeers)),
			},
			{
				Name:  "Remote crawl destinations",
				Value: fmt.Sprintf("%d", len(config.RemoteCrawl.AllowedDestinations)),
			},
			{Name: "Egress allows LAN", Value: yesNo(config.EgressAllowLAN)},
			{
				Name:  "Egress allow-list entries",
				Value: fmt.Sprintf("%d", len(config.EgressAllowedCIDRs)),
			},
			{Name: "Trusted proxies", Value: fmt.Sprintf("%d", len(config.TrustedProxies))},
			{Name: "Seed lists", Value: fmt.Sprintf("%d", len(config.SeedlistURLs))},
		}},
		{Title: "Crawler", Settings: []adminui.ConfigSetting{
			{Name: "Crawling", Value: enabledDisabled(config.Crawl.Enabled())},
		}},
		{Title: "Administrator", Settings: []adminui.ConfigSetting{
			{Name: "Username", Value: usernameOrUnset(config.Admin.Username)},
			{Name: "Password", Value: redactedSecret(config.Admin.Password)},
		}},
	}}
}

func webFallbackPrivacyDisplay(privacy webFallbackPrivacy) string {
	switch privacy {
	case webFallbackPrivacyExplicit:
		return "Only when requested"
	case webFallbackPrivacyEnabled:
		return "Enabled on search miss"
	case webFallbackPrivacyAlways:
		return "Always"
	default:
		return "Disabled"
	}
}

func publicListenerDisplay(addr string) string {
	if addr == "" {
		return "Disabled"
	}

	return addr
}

func redactedSecret(secret string) string {
	if secret == "" {
		return "Not set"
	}

	return "Configured"
}

func yesNo(value bool) string {
	if value {
		return "Yes"
	}

	return "No"
}

func enabledDisabled(value bool) string {
	if value {
		return "Enabled"
	}

	return "Disabled"
}

func usernameOrUnset(username string) string {
	if username == "" {
		return "Not configured"
	}

	return username
}

func storageQuota(quota int64) string {
	if quota <= 0 {
		return "Unlimited"
	}

	return fmt.Sprintf("%d bytes", quota)
}
