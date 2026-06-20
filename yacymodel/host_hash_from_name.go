package yacymodel

import "strings"

const (
	ftpHostPrefix = "ftp."
	httpScheme    = "http://"
	ftpScheme     = "ftp://"
)

func HostHashFromName(host string) string {
	host = strings.Trim(strings.ToLower(host), ".")
	if host == "" {
		return ""
	}
	scheme := httpScheme
	if strings.HasPrefix(host, ftpHostPrefix) {
		scheme = ftpScheme
	}
	return URLHash(scheme + host).HostHash()
}
