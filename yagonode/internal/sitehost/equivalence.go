package sitehost

import "strings"

func Matches(host string, constraint string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return false
	}
	for _, equivalent := range Equivalents(constraint) {
		if host == equivalent {
			return true
		}
	}

	return false
}

func Equivalents(constraint string) []string {
	constraint = strings.ToLower(strings.Trim(strings.TrimSpace(constraint), "."))
	if constraint == "" {
		return nil
	}
	if strings.HasPrefix(constraint, "www.") {
		return []string{constraint, strings.TrimPrefix(constraint, "www.")}
	}

	return []string{constraint, "www." + constraint}
}
