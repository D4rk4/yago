package documentimport

import (
	"errors"
	"net/url"
	"strings"
	"unicode"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	LegacyURLStatusMetadataKey           = "yacy.migration_url_status"
	LegacyURLStatusEncodedHostWhitespace = "percent_encoded_whitespace_in_host"
	LegacyURLStatusInvalidHostCharacter  = "invalid_host_character"
	LegacyURLStatusInvalidHostEscape     = "invalid_percent_escape_in_host"
	LegacyURLStatusInvalidEscape         = "invalid_percent_escape_in_url"
	LegacyURLStatusInvalidAuthority      = "invalid_source_authority"
	LegacyURLStatusControlCharacter      = "control_character_in_source_url"
)

func ValidateURLIdentity(rawURL, legacyStatus string) error {
	if len(rawURL) == 0 || len(rawURL) > MaximumMigrationURLBytes {
		return errors.New("URL identity is empty or exceeds the supported limit")
	}
	parsed, err := url.Parse(rawURL)
	if len(rawURL) <= yagocrawlcontract.MaximumCrawlURLBytes && err == nil &&
		parsed.Host != "" && parsed.User == nil && parsed.Fragment == "" &&
		isSupportedImportScheme(parsed.Scheme) {
		return nil
	}
	if legacyStatus != "" && legacyStatus == PreservableLegacyURLStatus(rawURL) {
		return nil
	}
	return errors.New("URL identity is not supported for document import")
}

func IsPreservableLegacyURL(rawURL string) bool {
	return PreservableLegacyURLStatus(rawURL) != ""
}

func PreservableLegacyURLStatus(rawURL string) string {
	if strings.IndexFunc(rawURL, unicode.IsControl) >= 0 {
		return controlCharacterIdentityStatus(rawURL)
	}
	if status := malformedQueryPercentEscapeStatus(rawURL); status != "" {
		return status
	}
	authority, parseErr, ok := legacyAuthorityError(rawURL)
	if !ok {
		return ""
	}
	var escapeErr url.EscapeError
	if errors.As(parseErr.Err, &escapeErr) {
		if strings.ContainsAny(rawURL, "#\\") {
			return ""
		}
		if status := encodedHostWhitespaceStatus(authority, escapeErr); status != "" {
			return status
		}
		if status := invalidHostPercentEscapeStatus(rawURL, authority); status != "" {
			return status
		}
		return malformedPercentEscapeURLStatus(rawURL, authority)
	}
	var invalidHostErr url.InvalidHostError
	if errors.As(parseErr.Err, &invalidHostErr) {
		return LegacyURLStatusInvalidHostCharacter
	}
	return preservableInvalidAuthorityStatus(rawURL, authority)
}

func controlCharacterIdentityStatus(rawURL string) string {
	if len(rawURL) > MaximumMigrationURLBytes {
		return ""
	}
	separator := strings.Index(rawURL, "://")
	if separator < 0 || !isSupportedImportScheme(rawURL[:separator]) ||
		strings.ContainsAny(rawURL, "#\\") {
		return ""
	}
	sanitized := strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, rawURL)
	parsed, err := url.Parse(sanitized)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" ||
		!isSupportedImportScheme(parsed.Scheme) ||
		strings.Contains(strings.ToLower(parsed.Host), "%40") {
		return ""
	}

	return LegacyURLStatusControlCharacter
}

func preservableInvalidAuthorityStatus(rawURL, authority string) string {
	if authority == "" || strings.Contains(authority, "@") ||
		strings.Contains(strings.ToLower(authority), "%40") ||
		strings.IndexFunc(rawURL, unicode.IsControl) >= 0 ||
		strings.ContainsAny(rawURL, "#\\") {
		return ""
	}
	separator := strings.Index(rawURL, "://")
	if separator < 0 || !isSupportedImportScheme(rawURL[:separator]) {
		return ""
	}
	return LegacyURLStatusInvalidAuthority
}

func malformedQueryPercentEscapeStatus(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" ||
		!isSupportedImportScheme(parsed.Scheme) || !hasMalformedPercentEscape(parsed.RawQuery) {
		return ""
	}
	return LegacyURLStatusInvalidEscape
}

func malformedPercentEscapeURLStatus(rawURL, authority string) string {
	if strings.IndexFunc(rawURL, unicode.IsControl) >= 0 ||
		!hasMalformedPercentEscape(rawURL) || strings.ContainsAny(rawURL, "#\\") {
		return ""
	}
	separator := strings.Index(rawURL, "://")
	if separator < 0 {
		return ""
	}
	authorityEnd := separator + 3 + len(authority)
	if authorityEnd > len(rawURL) || !hasMalformedPercentEscape(rawURL[authorityEnd:]) {
		return ""
	}
	base, err := url.Parse(rawURL[:separator] + "://" + authority + "/")
	if err != nil || base.Host == "" || base.User != nil ||
		!isSupportedImportScheme(base.Scheme) {
		return ""
	}
	return LegacyURLStatusInvalidEscape
}

func invalidHostPercentEscapeStatus(rawURL, authority string) string {
	if strings.IndexFunc(rawURL, unicode.IsControl) >= 0 ||
		!strings.Contains(authority, "%") {
		return ""
	}
	if strings.ContainsAny(rawURL, "#\\") {
		return ""
	}
	separator := strings.Index(rawURL, "://")
	if separator < 0 {
		return ""
	}
	scheme := strings.ToLower(rawURL[:separator])
	if !isSupportedImportScheme(scheme) {
		return ""
	}
	return LegacyURLStatusInvalidHostEscape
}

func hasMalformedPercentEscape(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] != '%' {
			continue
		}
		if index+2 >= len(value) || !isHexByte(value[index+1]) || !isHexByte(value[index+2]) {
			return true
		}
		index += 2
	}
	return false
}

func isHexByte(value byte) bool {
	return (value >= '0' && value <= '9') ||
		(value >= 'a' && value <= 'f') || (value >= 'A' && value <= 'F')
}

func legacyAuthorityError(rawURL string) (string, *url.Error, bool) {
	if len(rawURL) == 0 || len(rawURL) > yagocrawlcontract.MaximumCrawlURLBytes ||
		strings.ContainsAny(rawURL, "\r\n\t") {
		return "", nil, false
	}
	separator := strings.Index(rawURL, "://")
	if separator < 0 {
		return "", nil, false
	}
	scheme := strings.ToLower(rawURL[:separator])
	if !isSupportedImportScheme(scheme) {
		return "", nil, false
	}
	authority := rawURL[separator+3:]
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	if authority == "" || strings.Contains(authority, "@") {
		return "", nil, false
	}
	_, parseErr := url.Parse(rawURL)
	if parseErr == nil {
		return "", nil, false
	}
	return authority, &url.Error{Err: parseErr}, true
}

func encodedHostWhitespaceStatus(authority string, escapeErr url.EscapeError) string {
	if !strings.Contains(strings.ToLower(authority), "%20") ||
		!strings.Contains(string(escapeErr), "%20") {
		return ""
	}
	decodedAuthority := strings.ReplaceAll(strings.ToLower(authority), "%20", " ")
	if strings.Contains(decodedAuthority, "%") {
		return ""
	}
	for _, r := range decodedAuthority {
		if unicode.IsSpace(r) {
			continue
		}
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') &&
			r != '.' && r != '-' {
			return ""
		}
	}
	return LegacyURLStatusEncodedHostWhitespace
}

func isSupportedImportScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "http", "https", "ftp", "smb":
		return true
	default:
		return false
	}
}
