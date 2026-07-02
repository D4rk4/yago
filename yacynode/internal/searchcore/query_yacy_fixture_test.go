package searchcore

import (
	"strings"
	"testing"
)

func TestYaCyQueryGoalTermBoundaryFixtures(t *testing.T) {
	got := ParseTextQuery("O'Reily's book")

	upstream := []string{"o'reily's", "book"}
	if len(got.Terms) != len(upstream) {
		t.Fatalf("terms = %v, want %d terms %v", got.Terms, len(upstream), upstream)
	}
	for i, want := range upstream {
		if strings.ToLower(got.Terms[i]) != want {
			t.Errorf("term %d = %q, want case-insensitive %q", i, got.Terms[i], want)
		}
	}
}
