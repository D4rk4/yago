package formatparse

import "github.com/D4rk4/yago/yagocrawlcontract"

var unsupportedContainerExtensions = set(
	"appimage", "deb", "dmg", "exe", "iso", "mpkg", "msi", "pkg", "rpm", "txz", "xz",
)

func URLFetchAllowed(rawURL string, toggles yagocrawlcontract.FormatToggles) bool {
	extension := urlExtension(rawURL)
	if htmlExtensions[extension] {
		return true
	}
	if unsupportedContainerExtensions[extension] {
		return false
	}
	for _, registered := range families() {
		if registered.extensions[extension] {
			return registered.parseable(toggles)
		}
	}

	return true
}
