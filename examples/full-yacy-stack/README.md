# Full YaCy stack

Runs every currently available piece of the project together, giving you both a YaCy peer
on the DHT and your own self-hosted search engine: results blend the pages you crawl with the
web, and opening a result crawls that page, growing your own corpus from what you open.

Use this deployment when you want to operate on the YaCy network and run your own
self-hosted search engine over pages you crawl yourself.

```mermaid
flowchart LR
    You([You])
    Web([Web])
    Net([YaCy network])

    You -- search --> searxng[SearXNG]
    searxng -- query crawled pages --> elasticsearch[(Elasticsearch)]
    searxng -- query web engines --> Web
    searxng -- results, links routed to --> yacyvisitcrawl[yacyvisitcrawl]

    You -- open a result --> yacyvisitcrawl
    yacyvisitcrawl -- crawl order --> nats{{NATS}}
    yacyvisitcrawl -- redirect --> Web

    nats -- crawl order --> yacycrawler[yacycrawler]
    yacycrawler -- fetch --> smokescreen[smokescreen] --> Web
    yacycrawler -- crawled page --> nats

    nats -- crawled page --> yacytextindexer[yacytextindexer]
    yacytextindexer -- index --> elasticsearch

    nats -- crawled page --> node[yacy-rwi-node]
    node -- DHT traffic --> smokescreen
    node <-- share and serve results --> Net
```

## Run it

1. Copy `.env.example` to `.env` and set `YACY_PEER_HASH`, `YACY_PEER_NAME`,
   `YACY_ADVERTISE_HOST`, and `YACYVISITCRAWL_PUBLIC_URL`.
2. Copy `searxng/settings.yml.example` to `searxng/settings.yml` and set `server.secret_key`.
3. Copy `docker-compose.yml.example` to `docker-compose.yml`.
4. Start the stack: `docker compose up -d`.

See each Go service's `doc/configuration.md` for its environment variables, and
`searxng-crawled-text-search/doc/` and `searxng-result-router/doc/` for the SearXNG engine
and plugin the search UI runs.
