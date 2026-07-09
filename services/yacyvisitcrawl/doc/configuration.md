# yacyvisitcrawl configuration

The visit intake service is configured entirely through environment variables.

## Broker

| Variable | Default | Meaning |
|---|---|---|
| `NATS_URL` | required | NATS server the service places crawl orders on. |
| `NATS_ORDERS_SUBJECT` | `yacy.crawl.orders` | Subject crawl orders are placed on. |

## Placement

| Variable | Default | Meaning |
|---|---|---|
| `YACYVISITCRAWL_ORDER_TIMEOUT` | `5s` | Time bound on a single placement attempt. |
| `YACYVISITCRAWL_MAX_IN_FLIGHT` | `256` | Concurrent placement attempts allowed before new visits are skipped. |
| `YACYVISITCRAWL_MAX_BODY_BYTES` | `4096` | Largest request body accepted on `/visit`. |

## Crawl profile

Every placed order carries the same crawl profile, built once from these variables.

| Variable | Default | Meaning |
|---|---|---|
| `YACYVISITCRAWL_CRAWL_SCOPE` | `domain` | One of `domain`, `wide`, `subpath`. |
| `YACYVISITCRAWL_CRAWL_NAME` | empty | Human-readable profile name. |
| `YACYVISITCRAWL_CRAWL_MAX_DEPTH` | `1` | Link depth the crawl follows from the visited page. |
| `YACYVISITCRAWL_CRAWL_URL_MUST_MATCH` | match all | Regular expression a URL must match to be crawled. |
| `YACYVISITCRAWL_CRAWL_URL_MUST_NOT_MATCH` | none | Regular expression that excludes a URL from the crawl. |
| `YACYVISITCRAWL_CRAWL_MAX_PAGES_PER_HOST` | `100` | Pages per host the crawl may fetch; `-1` is unlimited. |
| `YACYVISITCRAWL_CRAWL_DELAY` | `0s` | Delay the crawl observes between requests to the same host. |
| `YACYVISITCRAWL_CRAWL_ALLOW_QUERY_URLS` | `false` | Whether URLs with a query string may be crawled. |

## Operations

| Variable | Default | Meaning |
|---|---|---|
| `YACYVISITCRAWL_LISTEN_ADDR` | `:8091` | Address serving `/visit`. |
| `YACYVISITCRAWL_OPS_ADDR` | `:9091` | Address serving `/metrics`. |
| `LOG_LEVEL` | `INFO` | Log level. |
