package yagonode

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
)

type gosecExclusionPolicy struct {
	Boundaries []gosecExclusionBoundary `json:"exclude-rules"`
}

type gosecExclusionBoundary struct {
	Path  string   `json:"path"`
	Rules []string `json:"rules"`
}

func TestReleaseGosecPolicyKeepsExclusionsFileScoped(t *testing.T) {
	contents, err := os.ReadFile("../../../.gosec.json")
	if err != nil {
		t.Fatalf("read gosec policy: %v", err)
	}
	var policy gosecExclusionPolicy
	if err := json.Unmarshal(contents, &policy); err != nil {
		t.Fatalf("decode gosec policy: %v", err)
	}
	if err := validateGosecExclusionPolicy(policy); err != nil {
		t.Fatal(err)
	}

	broadened := policy
	broadened.Boundaries = slices.Clone(policy.Boundaries)
	broadened.Boundaries[0].Path = ".*"
	if err := validateGosecExclusionPolicy(broadened); err == nil {
		t.Fatal("repository-wide gosec exclusion was admitted")
	}

	unknown := policy
	unknown.Boundaries = append(slices.Clone(policy.Boundaries), gosecExclusionBoundary{
		Path:  ".*new_file\\.go",
		Rules: []string{"G101"},
	})
	if err := validateGosecExclusionPolicy(unknown); err == nil {
		t.Fatal("unreviewed gosec exclusion was admitted")
	}
}

func validateGosecExclusionPolicy(policy gosecExclusionPolicy) error {
	expected := map[string]string{
		".*yago-crawler/internal/firefoxfetch/firefox\\.go":           "G204",
		".*yagomodel/yacy_hash_form\\.go":                             "G401,G501",
		".*yagonode/internal/adminauth/admin_credentials\\.go":        "G101",
		".*yagonode/internal/adminauth/api_key_endpoints\\.go":        "G101",
		".*yagonode/internal/adminauth/api_key_store\\.go":            "G101",
		".*yagonode/internal/adminauth/password_hash\\.go":            "G115",
		".*yagonode/internal/adminauth/session\\.go":                  "G124",
		".*yagonode/internal/adminui/performance_history\\.go":        "G203",
		".*yagonode/internal/faviconproxy/faviconproxy\\.go":          "G704",
		".*yagonode/internal/faviconproxy/imageproxy\\.go":            "G704",
		".*yagonode/internal/httpguard/request_invariants\\.go":       "G120",
		".*yagonode/internal/searchindex/bleve_disk_index\\.go":       "G115",
		".*yagonode/internal/searchindex/bounded_term_positions\\.go": "G115",
		".*yagonode/internal/searchindex/stored_hit_evidence\\.go":    "G115",
		".*yagonode/internal/shardvault/manifest\\.go":                "G304",
		".*yagonode/internal/shardvault/shardvault\\.go":              "G115",
		".*yagonode/internal/shardvault/split\\.go":                   "G115",
		".*yagonode/internal/snippetmark/snippetmark\\.go":            "G203",
	}
	remaining := maps.Clone(expected)
	for _, boundary := range policy.Boundaries {
		want, ok := remaining[boundary.Path]
		if !ok {
			return fmt.Errorf("unreviewed gosec exclusion path %q", boundary.Path)
		}
		if got := strings.Join(boundary.Rules, ","); got != want {
			return fmt.Errorf("gosec exclusion %q rules = %q, want %q", boundary.Path, got, want)
		}
		delete(remaining, boundary.Path)
	}
	if len(remaining) != 0 {
		return fmt.Errorf("gosec exclusions missing %v", slices.Sorted(maps.Keys(remaining)))
	}

	return nil
}
