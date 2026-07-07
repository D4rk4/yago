# 4. Use google/uuid to mint crawl order identity

Date: 2026-07-07

## Status

Accepted

## Context

Each visit turns into one crawl order that the crawl fleet delivers at least once. The order
needs an identifier that is unique across orders without central coordination, since
yacyvisitcrawl mints one per visit under load with no shared counter state.

`yacynode` and `yacycrawler` already mint and parse crawl order identity as a `google/uuid`
version 4 string (see `yacynode/doc/adr/0012-use-google-uuid-for-crawl-order-identity.md`).

## Decision

Use `github.com/google/uuid` to mint the order identifier as a random (version 4) UUID,
stamped on the order when a visit is turned into a `CrawlOrder`, matching the identity format
the rest of the crawl contract already shares.

## Consequences

`google/uuid` becomes a runtime dependency of yacyvisitcrawl's visit intake, matching the
version the sibling services already pin. Every placed order carries an identity in the same
format the crawl fleet already parses.
