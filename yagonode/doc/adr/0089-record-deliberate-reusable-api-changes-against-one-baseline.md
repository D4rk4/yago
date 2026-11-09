# 0089. Record deliberate reusable API changes against one baseline

Date: 2026-11-09

## Status

Accepted

Amends [ADR-0081](0081-protect-reusable-module-api-compatibility.md).

## Context

The reworked PDF reader changes the text stored for many documents. The crawl
contract marks such a change by incrementing `CurrentExtractionGeneration`, so
stored documents are fetched and extracted again. That constant belongs to the
reusable `yagocrawlcontract` module, and the API gate reports any change of an
exported constant value as incompatible. The gate had no way to accept one
deliberate change without accepting others.

## Decision

Record each deliberate incompatible change in
`tools/api-compatibility-exceptions`, one per line: the baseline tag, the
module, and the exact change line that apidiff reports. The gate removes only
an exactly matching line for that module and still refuses every other change.
It refuses an entry whose tag differs from the current baseline and an entry
that matches no reported change, so every entry expires when the next published
release becomes the baseline.

The first entry accepts the generation change from 1 to 2 against `v0.0.78`.

## Consequences

A deliberate break is visible in review, scoped to one release, and cannot hide
a later break. A mixed-version deployment treats documents from the older side
as stale until both node and crawler run the same generation.
