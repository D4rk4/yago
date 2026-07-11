package searchlocal

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestCoreResultCarriesContentClusterAssignment(t *testing.T) {
	result := coreResult(searchcore.Request{}, searchindex.SearchResult{
		URL:               "https://a.example",
		ClusterID:         "cluster-a",
		RepresentativeURL: "https://canonical.example",
	})
	if result.ClusterID != "cluster-a" ||
		result.RepresentativeURL != "https://canonical.example" {
		t.Fatalf("core cluster assignment = %+v", result)
	}
}
