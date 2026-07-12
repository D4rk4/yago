package faviconproxy

import "strings"

const (
	maximumDNSHostBytes  = 253
	maximumDNSLabelBytes = 63
)

func validDNSHost(host string) bool {
	if host == "" || len(host) > maximumDNSHostBytes {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !validDNSLabel(label) {
			return false
		}
	}

	return true
}

func validDNSLabel(label string) bool {
	if label == "" || len(label) > maximumDNSLabelBytes ||
		label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for index := range len(label) {
		character := label[index]
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') && character != '-' {
			return false
		}
	}

	return true
}
