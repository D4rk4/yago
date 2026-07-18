package adminui

import (
	"strings"
	"testing"
)

func TestSecurityAPIKeyPaginationBoundsAndValidatesHistory(t *testing.T) {
	const (
		firstCursor   = "AAAAAAAAAAAAAAAA"
		currentCursor = "AAAAAAAAAAAAAAAB"
		nextCursor    = "AAAAAAAAAAAAAAAC"
	)
	history := strings.TrimSuffix(strings.Repeat(firstCursor+",", 257), ",")
	parsed := parseSecurityAPIKeyHistory(history)
	if len(parsed) != securityAPIKeyHistoryLimit {
		t.Fatalf("history length = %d", len(parsed))
	}
	if got := parseSecurityAPIKeyHistory("not-a-cursor"); got != nil {
		t.Fatalf("invalid history = %v", got)
	}
	if got := parseSecurityAPIKeyHistory(strings.Repeat("x", 129)); got != nil {
		t.Fatalf("oversized history = %v", got)
	}
	if got := parseSecurityAPIKeyHistory(""); got != nil {
		t.Fatalf("empty history = %v", got)
	}

	view := SecurityView{
		Keys:             []APIKeyItem{{ID: currentCursor}},
		APIKeyTotal:      6000,
		APIKeyNextCursor: nextCursor,
	}
	pagination := buildSecurityAPIKeyPagination(view, currentCursor, history)
	if !pagination.HasPrev || !pagination.HasNext || pagination.Shown != 1 ||
		pagination.Total != 6000 {
		t.Fatalf("pagination = %+v", pagination)
	}
	if !strings.Contains(pagination.PrevURL, "key_cursor="+firstCursor) ||
		!strings.Contains(pagination.NextURL, "key_cursor="+nextCursor) {
		t.Fatalf("navigation URLs = %q %q", pagination.PrevURL, pagination.NextURL)
	}
	encodedHistory := strings.Split(strings.Split(pagination.NextURL, "key_history=")[1], "#")[0]
	if strings.Count(encodedHistory, "%2C") != securityAPIKeyHistoryLimit-1 {
		t.Fatalf("next history was not capped: %q", pagination.NextURL)
	}
}

func TestSecurityAPIKeyPageURLHandlesFirstAndHistoryPages(t *testing.T) {
	if got := securityAPIKeyPageURL("", nil); got != "/admin/security#api-keys" {
		t.Fatalf("first URL = %q", got)
	}
	got := securityAPIKeyPageURL(
		"AAAAAAAAAAAAAAAB",
		[]string{"AAAAAAAAAAAAAAAA"},
	)
	if !strings.Contains(got, "key_cursor=AAAAAAAAAAAAAAAB") ||
		!strings.Contains(got, "key_history=AAAAAAAAAAAAAAAA") {
		t.Fatalf("history URL = %q", got)
	}
}
