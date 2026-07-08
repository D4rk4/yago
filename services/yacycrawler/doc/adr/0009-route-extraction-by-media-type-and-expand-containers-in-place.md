# 9. Route extraction by media type and expand containers in place

Date: 2026-07-06

## Status

Accepted

## Context

The crawler must stay open to content beyond HTML — PDFs, plain text, and archives — without
reshaping the core each time. The fetch and index stages are already content-neutral; only the
extraction stage named a single format.

## Decision

The engine holds one `PageExtract` that returns the documents found in a fetched resource. A
`contentextraction` router dispatches by media type: a registered single-document extractor
(`htmlpage`, and future PDF or text extractors) returns its one document; a registered
`ContainerExpansion` (`archivemember` for zip and tar) yields the contained resources, which the
router re-dispatches, so a container fans out into many documents from a single fetch. Members
are expanded in place from already-fetched bytes, never re-fetched through the frontier. A new
format is a new package plus one registration line.

Which media types are active is an operator concern (`YACYCRAWLER_CONTENT_TYPES`), never the
crawl profile. Nesting depth and documents-per-container are bounded so a decompression bomb
stays bounded; overflow disposes the resource as `container-overflow`.

## Consequences

Adding a content type touches no existing file beyond registration. The core names no format.
Archives introduce a member-addressing scheme (`containerURL!/path`) and per-container bounds.
Containers and frontier expansion stay distinct: links discovered inside a member still flow to
the frontier normally.
