# 20. Public search portal as a progressively-enhanced surface separate from the admin SPA

Date: 2026-07-03

## Status

Accepted

## Context

`yago` needs a public, anonymous search front door for search-portal and intranet
deployments: the page an end user reaches on the node's public HTTP port (`80` in
appliance mode) to run a query and read results. This is distinct from the
authenticated operator console (UI-02..UI-10), which is a Carbon React SPA.

Two forces are in tension. First, the operator wants one visual language, so the
public portal should be built on IBM Carbon like the admin UI. Second, the public
portal must render and be usable in old browsers and on mobile, deliberately
evoking the minimal early-2000s Yandex search page — a centered wordmark, one
prominent search box and button, and a plain results list. `@carbon/react`
targets evergreen browsers and assumes a modern JavaScript runtime, so serving the
public portal as the same React SPA would break the legacy-browser and
no-JavaScript requirement.

The portal always searches local and peer sources. Web mode `enabled` adds
external search after a true miss, while `always` runs it alongside local and peer
retrieval. `explicit` does not grant the anonymous portal request-level consent.

## Decision

Serve the public search portal as a **separate, progressively-enhanced surface**
rather than as part of the admin React SPA:

- It is served on the node's public HTTP listener (port `80` in appliance mode),
  separate from the admin listener, and is off by default — gated by the
  "Public search enabled" runtime setting and the `YAGO_PUBLIC_SEARCH_UI_ENABLED`
  toggle.
- It is server-rendered semantic HTML styled with `@carbon/styles` design tokens,
  so it shares Carbon's visual language and theming without depending on the full
  `@carbon/react` runtime. Search works with a plain form submission and no client
  JavaScript; interactive Carbon enhancements are layered on only where they
  degrade cleanly in older browsers.
- The layout is responsive down to small phone widths and does not scroll
  horizontally.
- It exposes only search (and, when enabled, the OpenSearch description and
  suggestions); it never exposes admin APIs and it honors the SEC-05 privacy mode
  for query logging.
- It renders DDGS results with visible web provenance and explains that the
  external provider received the query (ADR-0019).

## Consequences

There are two Carbon-based frontends with different runtime budgets: the admin SPA
for evergreen browsers, and this progressively-enhanced public portal for the
broadest possible reach. Sharing Carbon tokens keeps them visually consistent, but
the portal cannot lean on the full `@carbon/react` component set, so some
components are hand-authored against Carbon tokens or chosen for their graceful
degradation. This is the cost of the legacy-browser and no-JavaScript guarantees.
Keeping the portal search-only and admin-free bounds its attack surface. The
operator's web fallback mode controls whether a query can leave the node and
whether it runs after a miss or alongside local and peer retrieval.
