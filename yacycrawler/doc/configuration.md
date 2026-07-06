# yacycrawler configuration

The crawler is configured entirely through environment variables.

## Broker

| Variable | Default | Meaning |
|---|---|---|
| `NATS_URL` | required | NATS server the crawler connects to. |
| `NATS_ORDERS_SUBJECT` | `yacy.crawl.orders` | Subject the crawler consumes orders from. |
| `NATS_ORDERS_DURABLE` | `yacycrawler` | Durable queue-consumer name shared across instances. |
| `NATS_PAGE_INDEX_SUBJECT` | `yacy.crawl.page-index` | Subject the index output publishes to. |
| `NATS_PAGE_INDEX_MAX_MSGS` | `1024` | Bound on the index output stream. |
| `NATS_PAGES_SUBJECT` | `yacy.crawl.pages` | Subject the page-content output publishes to. |
| `NATS_PAGES_MAX_MSGS` | `1024` | Bound on the page-content output stream. |

## Fetching

| Variable | Default | Meaning |
|---|---|---|
| `YACYCRAWLER_PROXY_URL` | required | Egress proxy every outbound fetch passes through. |
| `YACYCRAWLER_MAX_BODY_BYTES` | `2097152` | Largest response body accepted; larger is disposed. |
| `YACYCRAWLER_FETCH_DEADLINE` | `30s` | Deadline for a single fetch. |
| `YACYCRAWLER_CONTENT_TYPES` | all | Comma-separated media types to crawl. Empty crawls every supported type; a list that matches none fails startup. |
| `YACYCRAWLER_USER_AGENT` | `yacycrawler (+https://yacy.net)` | User-Agent sent with every fetch. |

## Run limits

| Variable | Default | Meaning |
|---|---|---|
| `YACYCRAWLER_WORKERS` | `1` | Orders processed concurrently. |
| `YACYCRAWLER_RUN_PAGE_BUDGET` | `1000` | Pages a single run may fetch before it stops. |
| `YACYCRAWLER_FRONTIER_CAP` | `10000` | Largest frontier a single run may hold. |

## Outputs

| Variable | Default | Meaning |
|---|---|---|
| `YACYCRAWLER_INDEX_OUTPUT_ENABLED` | `true` | Publish page references and postings. |
| `YACYCRAWLER_PAGE_OUTPUT_ENABLED` | `false` | Publish page text content. |

At least one output must be enabled, or startup fails.

## Operations

| Variable | Default | Meaning |
|---|---|---|
| `YACYCRAWLER_OPS_ADDR` | `:9090` | Address serving `/health` and `/metrics`. |
| `LOG_LEVEL` | `INFO` | Log level. |
