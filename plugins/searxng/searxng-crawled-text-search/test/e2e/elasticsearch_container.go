//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/elasticsearch"
	"github.com/nikitakarpei/yacy-rwi-node/searchdocument"
)

const (
	elasticsearchAlias = "elasticsearch"
	elasticsearchIndex = "yacy-text"
)

func startElasticsearch(t *testing.T, ctx context.Context, networkName string) string {
	t.Helper()
	return elasticsearch.Start(t, ctx, networkName, elasticsearchAlias)
}

func elasticsearchNetworkURL() string {
	return elasticsearch.NetworkURL(elasticsearchAlias)
}

func elasticsearchEngineSettings() string {
	return "    search_index_engine: elasticsearch\n" +
		"    elasticsearch_url: " + elasticsearchNetworkURL() + "\n" +
		"    elasticsearch_index: " + elasticsearchIndex + "\n"
}

func seedElasticsearchDocument(t *testing.T, ctx context.Context, elasticsearchURL, id string, doc searchdocument.Document) {
	t.Helper()
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal seed document: %v", err)
	}

	target := elasticsearchURL + "/" + elasticsearchIndex + "/_doc/" + id + "?refresh=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build seed request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("seed document: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		t.Fatalf("seed document: status %d", resp.StatusCode)
	}
}
