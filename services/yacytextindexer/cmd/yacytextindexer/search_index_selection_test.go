package main

import (
	"net/http"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/elasticsearchindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/manticoreindex"
)

func TestLoadServiceConfigManticoreRequiresURL(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL:           "nats://localhost:4222",
		EnvSearchIndexEngine: SearchIndexEngineManticore,
	})); err == nil {
		t.Fatal("expected error when MANTICORE_URL is unset")
	}
}

func TestLoadServiceConfigManticoreDefaults(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL:           "nats://localhost:4222",
		EnvSearchIndexEngine: SearchIndexEngineManticore,
		EnvManticoreURL:      "http://localhost:9308",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ManticoreURL != "http://localhost:9308" {
		t.Errorf("manticore url = %q", cfg.ManticoreURL)
	}
	if cfg.ManticoreTable != DefaultManticoreTable {
		t.Errorf("manticore table = %q", cfg.ManticoreTable)
	}
}

func TestLoadServiceConfigRejectsUnknownEngine(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL:           "nats://localhost:4222",
		EnvSearchIndexEngine: "sphinx",
	})); err == nil {
		t.Fatal("expected error for unknown engine")
	}
}

func TestSelectSearchIndexElasticsearch(t *testing.T) {
	index, name, err := selectSearchIndex(ServiceConfig{
		SearchIndexEngine:  SearchIndexEngineElasticsearch,
		ElasticsearchURL:   "http://localhost:9200",
		ElasticsearchIndex: "yacy-text",
	}, http.DefaultClient)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, ok := index.(*elasticsearchindex.ElasticsearchIndex); !ok {
		t.Errorf("index = %T", index)
	}
	if name != "yacy-text" {
		t.Errorf("name = %q", name)
	}
}

func TestSelectSearchIndexManticore(t *testing.T) {
	index, name, err := selectSearchIndex(ServiceConfig{
		SearchIndexEngine: SearchIndexEngineManticore,
		ManticoreURL:      "http://localhost:9308",
		ManticoreTable:    "yacy-text",
	}, http.DefaultClient)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, ok := index.(*manticoreindex.ManticoreIndex); !ok {
		t.Errorf("index = %T", index)
	}
	if name != "yacy-text" {
		t.Errorf("name = %q", name)
	}
}

func TestSelectSearchIndexRejectsUnknownEngine(t *testing.T) {
	if _, _, err := selectSearchIndex(ServiceConfig{
		SearchIndexEngine: "sphinx",
	}, http.DefaultClient); err == nil {
		t.Fatal("expected error for unknown engine")
	}
}
