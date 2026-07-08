# 2. Use chromedp to drive the CDP browser

Date: 2026-07-09

## Status

Accepted

## Context

renderproxy contacts no origin itself; it navigates an external CDP-compatible browser to a
target URL and returns the settled page. It needs a Go client for the Chrome DevTools
Protocol that can attach to a remote browser endpoint without launching or supervising a
browser process, matching the service's Non-Goal of running the browser itself.

## Decision

Use `github.com/chromedp/chromedp` (with its `github.com/chromedp/cdproto` companion) as the
CDP client. It attaches to a remote CDP endpoint via `chromedp.NewRemoteAllocator`, drives
navigation and DOM serialization, and exposes the network events needed to read the main
frame's response status and content type.

## Consequences

`chromedp` and `cdproto` become runtime dependencies of renderproxy, pinned in `go.mod`.
CDP-specific vocabulary is confined to the `internal/cdprender` package; the rest of the
service speaks only in terms of a page renderer.
