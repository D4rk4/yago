package main

import (
	"fmt"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/elasticsearchindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/manticoreindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/pageintake"
)

func selectSearchIndex(
	cfg ServiceConfig,
	client *http.Client,
) (pageintake.SearchIndex, string, error) {
	switch cfg.SearchIndexEngine {
	case SearchIndexEngineElasticsearch:
		return elasticsearchindex.NewElasticsearchIndex(
			cfg.ElasticsearchURL,
			cfg.ElasticsearchIndex,
			client,
		), cfg.ElasticsearchIndex, nil
	case SearchIndexEngineManticore:
		return manticoreindex.NewManticoreIndex(
			cfg.ManticoreURL,
			cfg.ManticoreTable,
			client,
		), cfg.ManticoreTable, nil
	default:
		return nil, "", fmt.Errorf(
			"%s: unknown engine %q", EnvSearchIndexEngine, cfg.SearchIndexEngine,
		)
	}
}
