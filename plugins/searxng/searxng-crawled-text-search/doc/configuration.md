# searxng-crawled-text-search configuration

The engine is configured entirely through its `engines:` entry in SearXNG's own `settings.yml` —
no environment variables.

## Enabling the engine

Add the engine's directory to SearXNG's `engines/` folder (or mount `crawled_text_search.py`
there), then add it to `settings.yml`:

```yaml
engines:
  - name: crawled text search
    engine: crawled_text_search
    shortcut: ct
    categories: general
    enable_http: true
    search_index_engine: elasticsearch
    elasticsearch_url: http://elasticsearch:9200
    elasticsearch_index: yacy-text
```

| Key | Meaning |
|---|---|
| `search_index_engine` | Required. Which search index to query: `elasticsearch` or `manticore`. |

When `search_index_engine` is `elasticsearch`:

| Key | Meaning |
|---|---|
| `elasticsearch_url` | Base URL of the Elasticsearch instance to query. |
| `elasticsearch_index` | Name of the index `yacytextindexer` writes documents into. |

When `search_index_engine` is `manticore`:

| Key | Meaning |
|---|---|
| `manticore_url` | Base URL of the Manticore instance to query. |
| `manticore_table` | Name of the table `yacytextindexer` writes documents into. Manticore table names allow letters, digits, and underscores only. |
