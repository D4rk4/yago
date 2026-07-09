package manticoreindex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/searchdocument"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/crawledpagedocument"
)

type ManticoreIndex struct {
	endpoint string
	table    string
	client   *http.Client
}

func NewManticoreIndex(endpoint, table string, client *http.Client) *ManticoreIndex {
	return &ManticoreIndex{
		endpoint: strings.TrimRight(endpoint, "/"),
		table:    table,
		client:   client,
	}
}

type replaceRequest struct {
	Table    string                  `json:"table"`
	Identity int64                   `json:"id"`
	Document searchdocument.Document `json:"doc"`
}

func (idx *ManticoreIndex) Index(
	ctx context.Context,
	page yacycrawlcontract.CrawledPage,
) error {
	identity := documentIdentity(page.CanonicalURL)
	body, err := json.Marshal(replaceRequest{
		Table:    idx.table,
		Identity: identity,
		Document: crawledpagedocument.Of(page),
	})
	if err != nil {
		return fmt.Errorf("marshal search document %d: %w", identity, err)
	}
	target := idx.endpoint + "/replace"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build index request %d: %w", identity, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := idx.client.Do(req)
	if err != nil {
		return fmt.Errorf("index document %d: %w", identity, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf(
			"index document %d: status %d: %s",
			identity,
			resp.StatusCode,
			detail,
		)
	}
	return nil
}
