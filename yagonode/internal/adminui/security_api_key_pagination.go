package adminui

import (
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
)

const (
	securityAccessCursorParameter  = "key_cursor"
	securityAccessHistoryParameter = "key_history"
	securityAPIKeyHistoryLimit     = 256
)

type SecurityAPIKeyPagination struct {
	Shown   int
	Total   int
	HasPrev bool
	HasNext bool
	PrevURL string
	NextURL string
}

func buildSecurityAPIKeyPagination(
	view SecurityView,
	cursor string,
	rawHistory string,
) SecurityAPIKeyPagination {
	history := parseSecurityAPIKeyHistory(rawHistory)
	pagination := SecurityAPIKeyPagination{
		Shown:   len(view.Keys),
		Total:   view.APIKeyTotal,
		HasPrev: cursor != "",
		HasNext: view.APIKeyNextCursor != "",
	}
	if pagination.HasPrev {
		previousCursor := ""
		if len(history) > 0 {
			previousCursor = history[len(history)-1]
			history = history[:len(history)-1]
		}
		pagination.PrevURL = securityAPIKeyPageURL(previousCursor, history)
	}
	if pagination.HasNext {
		nextHistory := append([]string(nil), parseSecurityAPIKeyHistory(rawHistory)...)
		if cursor != "" {
			nextHistory = append(nextHistory, cursor)
		}
		if len(nextHistory) > securityAPIKeyHistoryLimit {
			nextHistory = nextHistory[len(nextHistory)-securityAPIKeyHistoryLimit:]
		}
		pagination.NextURL = securityAPIKeyPageURL(view.APIKeyNextCursor, nextHistory)
	}

	return pagination
}

func parseSecurityAPIKeyHistory(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > securityAPIKeyHistoryLimit {
		parts = parts[len(parts)-securityAPIKeyHistoryLimit:]
	}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || !adminauth.ValidAPIKeyPageCursor(part) {
			return nil
		}
		result = append(result, part)
	}

	return result
}

func securityAPIKeyPageURL(cursor string, history []string) string {
	values := url.Values{}
	if cursor != "" {
		values.Set(securityAccessCursorParameter, cursor)
	}
	if len(history) > 0 {
		values.Set(securityAccessHistoryParameter, strings.Join(history, ","))
	}
	target := securityPath
	if query := values.Encode(); query != "" {
		target += "?" + query
	}

	return target + "#api-keys"
}
