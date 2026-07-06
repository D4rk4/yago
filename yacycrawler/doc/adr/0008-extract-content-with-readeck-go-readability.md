# 8. Extract page content with readeck/go-readability

Date: 2026-07-06

## Status

Accepted

## Context

The index and page-content outputs carry a page's main readable content, not its boilerplate.
We need a Readability.js-equivalent extractor for text, title, and language.

## Decision

We use `codeberg.org/readeck/go-readability/v2` (pinned in `go.mod`) as the sole source of page
text, title, and language, via `FromDocument` on the already-parsed tree. There is no fallback
extraction path: a page readability cannot extract is disposed as `unextractable` rather than
indexed from raw markup. Language absent from the article stays empty.

## Consequences

The corpus reflects main content, keeping boilerplate out of postings. Pages that are not
article-shaped are intentionally dropped, which callers must expect. The extractor is confined
to the `htmlpage` edge.
