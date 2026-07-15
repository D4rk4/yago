package publicportal

import "strings"

const openSearchTitleLimit = 16

func openSearchTitle(displayBrand string) string {
	name := strings.TrimSpace(displayBrand)
	if name == "" {
		name = brand
	}
	preferred := name + " search"
	if len([]rune(preferred)) <= openSearchTitleLimit {
		return preferred
	}
	runes := []rune(name)
	if len(runes) > openSearchTitleLimit {
		runes = runes[:openSearchTitleLimit]
	}

	return string(runes)
}
