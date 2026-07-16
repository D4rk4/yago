# 22. Server-rendered admin console with htmx instead of a React SPA

Date: 2026-07-04

## Status

Accepted

Supersedes the "Vite + React + Carbon SPA" frontend choice recorded in the UI-01
task and the "admin React SPA" framing in ADR-0020. ADR-0020's decision about the
public portal (server-rendered, progressively enhanced) still stands; only its
assumption that the admin console is a React SPA changes.

## Context

The operator console (UI-02..UI-10) and the public search portal (UI-11, ADR-0020)
both need to be built on IBM Carbon's visual language. The original plan (UI-01)
chose a `Vite + React + @carbon/react` single-page app for the admin console.

Several forces push against that for this project:

- The repository rule requires an ADR for every new third-party dependency. A
  React + Vite + Carbon + TypeScript + ESLint + Vitest toolchain pulls in hundreds
  of transitive npm packages — a large supply-chain surface to review and justify.
- `yago` surfaces are self-contained with a strict CSP and no runtime internet
  dependency; a bundled SPA is workable but adds a whole build pipeline.
- `make verify` is Go-only today; a JS SPA means adding lint/typecheck/test/build
  frontend gates to it.
- The public portal is already server-rendered for legacy-browser and
  no-JavaScript reach (ADR-0020), so a second, entirely different frontend runtime
  would exist only for the admin console.

The operator chose a server-rendered admin console enhanced with htmx.

## Decision

Build the admin console as a **server-rendered surface** on the node's operations
listener, behind the existing admin session guard:

- Pages are Go `html/template` output styled with hand-authored CSS that uses IBM
  Carbon design tokens (color, type scale, spacing), so the console shares
  Carbon's visual language and stays consistent with the public portal without the
  `@carbon/react` runtime. IBM Plex is requested via a system-font fallback stack;
  no font is fetched from an external host. An embedded Photon layer gives the
  operator console a compact QNX-style desktop, bevel, titlebar, and control
  treatment while retaining the Carbon-classed semantic markup. Buttons use one
  raised face and a visibly sunken pressed state, and tabbed panels use a compact
  contiguous strip whose selected tab joins the panel. A tab strip that cannot
  fit stays inside its own horizontal scroller and reveals the selected tab, so
  narrow pages do not overflow. Standalone console responses such as node restart
  load that same revisioned layer. The right shelf keeps a flat route list and a
  compact system monitor fed by the existing ten-second bounded metric sampler.
  It shows process CPU, process RSS relative to host RAM, host
  used/total/available memory, vault storage use/quota, and live busy crawler
  fetch-worker slots when the crawler runtime is enabled; unavailable
  observations remain explicit.
  The flat shelf uses the pinned local Haiku icon subset recorded in ADR-0049,
  with accessible text labels and no runtime asset request outside the node.
  Tables with column headers use a
  QNX list treatment, while headerless property grids use row rules. Both share
  one continuous outer bevel instead of per-cell top shadows.
- Interactivity is layered on with a single vendored, pinned `htmx.min.js`
  embedded via `go:embed` — partial updates and navigation without a full page
  reload — and every page degrades to a working plain-HTML form/link when
  JavaScript is unavailable. htmx is the only frontend dependency and ships in the
  binary; there is no npm, Vite, or React.
- All assets (CSS, htmx, templates) are embedded in the Go binary; the console is
  served from the same process that owns the data, so handlers read internal state
  directly instead of round-tripping through a JSON API. Template asset URLs carry
  the current content revision and are immutable only when the canonical path and
  revision match. Unversioned canonical URLs revalidate, while stale revisions and
  path aliases return a non-cacheable not-found response.
- `make verify` stays Go-only; there is no separate frontend toolchain.

## Consequences

The admin console and the public portal share one server-rendered, Carbon-token
approach with a single small frontend dependency (htmx) instead of a full SPA
toolchain. This keeps the dependency and CSP surface minimal, keeps `make verify`
Go-only, and guarantees the console works without a client build step. The cost is
that rich client-side interactions are expressed with htmx and server round-trips
rather than a React component tree, and Carbon components used by the console are
hand-authored against Carbon tokens rather than imported from `@carbon/react`. If a
future surface genuinely needs a heavy client runtime, that can be revisited with
its own ADR.
