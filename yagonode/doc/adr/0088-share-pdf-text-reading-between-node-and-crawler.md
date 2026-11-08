# 0088. Share PDF text reading between node and crawler

Date: 2026-11-08

## Status

Accepted

## Context

The crawler reads the embedded text of PDF and PostScript files with its own
bounded reader. The node's fetch-on-extract path for `/extract`, `/crawl`, and
`/map` admitted only HTML and plain text, so a direct PDF URL failed even
though the crawler could read the same file.

The reader lived inside the crawler module. The node must not import crawler
runtime code.

## Decision

Move the PDF and PostScript text reader into a new workspace module,
`yagopdf`. It depends only on the Go standard library. The crawler and the node
both import it. The crawler keeps its own file-type routing and its first-line
title fallback; the module returns the document-info title and the text.

`yagopdf` is shared inside this workspace only. Like the application modules,
it is outside the reusable-module API promise.

Raise the default fetch-on-extract response limit from 2 MiB to the existing
4 MiB ceiling so common manuals and data sheets fit.

## Considered alternatives

Add a third-party PDF library to the node. The node and crawler would read the
same file differently, and the build would gain a dependency.

Hand PDF URLs to the crawler and fail the request. The first `/extract` call
would still fail.

## Consequences

The node and crawler read PDF text identically, with the same per-document
stream, decode, and text bounds. The node now reads a PDF body of up to the
configured response limit before parsing it.

The workspace has one more module to build, lint, scan, and copy into the
container build context.
