# yacytextindexer configuration

The indexer is configured entirely through environment variables.

## Broker

| Variable | Default | Meaning |
|---|---|---|
| `NATS_URL` | required | NATS server the indexer consumes crawled pages from. |
| `NATS_CRAWLED_PAGE_SUBJECT` | `yacy.crawl.pages` | Subject the indexer consumes crawled pages from. |
| `NATS_CRAWLED_PAGE_MAX_MSGS` | `1024` | Bound on the crawled-page stream. |
| `NATS_CRAWLED_PAGE_DURABLE` | `yacytextindexer` | Durable queue-consumer name shared across instances. |

## Indexing

| Variable | Default | Meaning |
|---|---|---|
| `YACYTEXTINDEXER_CONCURRENCY` | `4` | Documents indexed concurrently. |
| `SEARCH_INDEX_ENGINE` | `elasticsearch` | Which search index to write to: `elasticsearch` or `manticore`. |

When `SEARCH_INDEX_ENGINE` is `elasticsearch`:

| Variable | Default | Meaning |
|---|---|---|
| `ELASTICSEARCH_URL` | required | Elasticsearch endpoint documents are indexed into. |
| `ELASTICSEARCH_INDEX` | `yacy-text` | Elasticsearch index documents are indexed into. |

When `SEARCH_INDEX_ENGINE` is `manticore`:

| Variable | Default | Meaning |
|---|---|---|
| `MANTICORE_URL` | required | Manticore endpoint documents are indexed into. |
| `MANTICORE_TABLE` | `yacy-text` | Manticore table documents are indexed into. |
