package yagocrawlcontract

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func ParseCrawlerBrowserPath(raw string) (string, error) {
	value := strings.Trim(raw, " ")
	if value == "" {
		return "", nil
	}
	if len(value) > MaximumCrawlerBrowserPathBytes || !filepath.IsAbs(value) ||
		filepath.Clean(value) != value || filepath.Base(value) == string(filepath.Separator) ||
		!validFirefoxLauncherName(filepath.Base(value)) || invalidCrawlerFacilityText(value) {
		return "", fmt.Errorf(
			"browser path must be an absolute clean Firefox or Firefox ESR launcher path no longer than %d bytes",
			MaximumCrawlerBrowserPathBytes,
		)
	}

	return value, nil
}

func validFirefoxLauncherName(name string) bool {
	return name == "firefox" || name == "firefox-esr"
}

func ParseCrawlerMetricsAddress(raw string) (string, error) {
	value := strings.Trim(raw, " ")
	if value == "" {
		return "", nil
	}
	if len(value) > MaximumCrawlerMetricsAddressBytes || invalidCrawlerFacilityText(value) {
		return "", fmt.Errorf(
			"metrics address must be an IP listen address no longer than %d bytes",
			MaximumCrawlerMetricsAddressBytes,
		)
	}
	host, rawPort, err := net.SplitHostPort(value)
	if err != nil || host == "" {
		return "", fmt.Errorf("metrics address must use loopback IP-literal host:port syntax")
	}
	address := net.ParseIP(host)
	if address == nil || !address.IsLoopback() {
		return "", fmt.Errorf("metrics address must use a loopback IP literal")
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65_535 {
		return "", fmt.Errorf("metrics address port must be between 1 and 65535")
	}

	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func invalidCrawlerFacilityText(value string) bool {
	if !utf8.ValidString(value) {
		return true
	}

	return strings.ContainsFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.In(
			character,
			unicode.Cf,
			unicode.Zl,
			unicode.Zp,
		)
	})
}
