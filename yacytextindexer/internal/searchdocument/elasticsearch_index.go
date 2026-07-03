package searchdocument

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

type ElasticsearchIndex struct {
	endpoint string
	index    string
	client   *http.Client
}

func NewElasticsearchIndex(endpoint, index string, client *http.Client) *ElasticsearchIndex {
	return &ElasticsearchIndex{
		endpoint: strings.TrimRight(endpoint, "/"),
		index:    index,
		client:   client,
	}
}

func (idx *ElasticsearchIndex) Index(ctx context.Context, text yacycrawlcontract.ExtractedText) error {
	body, err := json.Marshal(FromExtractedText(text))
	if err != nil {
		return fmt.Errorf("marshal search document %s: %w", text.DocumentID, err)
	}
	target := fmt.Sprintf("%s/%s/_doc/%s", idx.endpoint, idx.index, url.PathEscape(text.DocumentID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build index request %s: %w", text.DocumentID, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := idx.client.Do(req)
	if err != nil {
		return fmt.Errorf("index document %s: %w", text.DocumentID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("index document %s: status %d: %s", text.DocumentID, resp.StatusCode, detail)
	}
	return nil
}
