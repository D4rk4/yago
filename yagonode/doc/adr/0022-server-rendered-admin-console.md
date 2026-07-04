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
  no font is fetched from an external host.
- Interactivity is layered on with a single vendored, pinned `htmx.min.js`
  embedded via `go:embed` — partial updates and navigation without a full page
  reload — and every page degrades to a working plain-HTML form/link when
  JavaScript is unavailable. htmx is the only frontend dependency and ships in the
  binary; there is no npm, Vite, or React.
- All assets (CSS, htmx, templates) are embedded in the Go binary; the console is
  served from the same process that owns the data, so handlers read internal state
  directly instead of round-tripping through a JSON API.
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
