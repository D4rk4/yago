//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/manticore"
	"github.com/nikitakarpei/yacy-rwi-node/searchdocument"
)

const (
	manticoreAlias = "manticore"
	manticoreTable = "yacy_text"
)

func startManticore(t *testing.T, ctx context.Context, networkName string) string {
	t.Helper()
	return manticore.Start(t, ctx, networkName, manticoreAlias)
}

func manticoreNetworkURL() string {
	return manticore.NetworkURL(manticoreAlias)
}

func manticoreEngineSettings() string {
	return "    search_index_engine: manticore\n" +
		"    manticore_url: " + manticoreNetworkURL() + "\n" +
		"    manticore_table: " + manticoreTable + "\n"
}

func seedManticoreDocument(
	t *testing.T,
	ctx context.Context,
	manticoreURL string,
	doc searchdocument.Document,
) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"table": manticoreTable,
		"id":    1,
		"doc":   doc,
	})
	if err != nil {
		t.Fatalf("marshal seed document: %v", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, manticoreURL+"/replace", bytes.NewReader(body),
	)
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
