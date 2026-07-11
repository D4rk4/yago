# 0033. Operator-editable public portal via server-side Handlebars with a GrapesJS admin editor

Date: 2026-07-07

## Status

Accepted

## Context

UI-28 asks for a dedicated **Public portal** admin section that (a) gathers the
portal-facing settings already scattered across the flat Configuration sheet —
the OpenSearch external base URL, browser-integration (OpenSearch autodiscovery
and suggestions), HTTPS redirect, robots policy, and greeting — and (b) lets an
operator **redesign the public search and results pages** through a visual editor
plus a code editor, without redeploying the binary.

Today the public portal (ADR-0020) is a fixed, server-rendered, progressively
enhanced surface: `internal/publicportal` builds the search box and results with
Go `html/template`, and the operator can tune only a greeting string and a handful
of toggles. There is no way to change the layout, wording, or markup of the public
pages short of editing the Go templates and rebuilding.

The operator was offered the architecture forks and chose:

1. **Rendering** — server-side. Go renders the public pages from operator-authored
   templates, keeping the portal working without JavaScript and SEO-friendly (the
   ADR-0020 no-JS guarantee for the *public* surface is preserved).
2. **Editable scope** — full page templates. The operator authors the entire
   search-page and results-page markup, not just designated slots.
3. **Editor** — the full GrapesJS visual editor plus a synced Handlebars code
   editor, delivered together rather than code-first.

The templating language is Handlebars: logic-less, familiar, and the same syntax
GrapesJS and its code view speak, so the visual and code editors share one source.

## Decision

Adopt an operator-editable public portal with three new third-party dependencies,
a vault-backed template store, and a safe server-side render path that always
falls back to the built-in default.

### Dependencies (this ADR is the gate)

- **`github.com/mailgun/raymond/v2`**, pinned **v2.0.48**, MIT — the maintained
  fork of `aymerick/raymond`, a pure-Go (no cgo) Handlebars 3 implementation.
  Server-side renderer for the operator templates. Chosen over hand-rolling a
  templating subset because Handlebars is a stable, specified language the editors
  already target; chosen over `aymerick/raymond` (origin, effectively unmaintained,
  last tag v2.0.2) for the live fork. Pinned in `yagonode/go.mod`/`go.sum` through
  the normal module flow.
- **GrapesJS**, pinned **v0.21.13** (recorded in the vendored asset header),
  BSD-3-Clause (corrected from MIT at vendoring time) — the visual page editor,
  vendored self-hosted (`grapes.min.js` + `grapes.min.css` + the webpage preset
  `grapesjs-preset-webpage` v1.0.3). No CDN: the admin CSP is
  `script-src 'self'`, so every asset is committed under `adminui/assets/` and
  served from origin, embedded via `go:embed`. Its remaining legacy icon-font
  controls use the local dependency approved in ADR-0046.
- **CodeMirror 5**, pinned **v5.65.21** (last 5.x line; recorded in the vendored
  asset header), MIT — the Handlebars source editor and its required simple and
  multiplex mode addons, paired with GrapesJS's canvas,
  also vendored self-hosted. CodeMirror 5 ships as standalone files (no bundler),
  which fits the repo's no-build, `go:embed` asset convention (htmx,
  autocomplete.js) far better than CodeMirror 6's npm graph.

The JS/CSS assets are third-party but vendored (not Go modules); their pinned
patch versions and licences are recorded in each committed asset file's header and
the deploy licence manifest, per the Dependency Rule applied to bundled assets.

These libraries load **only** on the admin Public-portal design tabs. The public
portal's CSP is — critically — not touched, and the public pages ship no new
script. On the admin side one narrow, page-scoped relaxation proved necessary at
implementation time: the GrapesJS canvas is an `about:blank` iframe that inherits
the embedding page's CSP and styles its editable content by injecting `<style>`
elements, which `style-src 'self'` blocks. The Public-portal page (and only that
page) therefore serves `style-src 'self' 'unsafe-inline'` and `font-src 'self'`
for the local ADR-0046 icon font; `script-src` stays `'self'` everywhere, every
other console page keeps the strict policy, and the residual (inline styles on
one authenticated admin page whose operator already edits the very markup
being styled) is accepted.

### Template storage and data model

- Two operator templates — **search** and **results** — plus a shared **styles**
  block and a small **settings** blob (e.g. whether the operator theme is active),
  persisted in the durable vault through a new dedicated store
  (`internal/portaltheme`), not the key/value settings store, because template
  bodies are large multi-line documents with their own versioning and reset needs.
- Each template records the built-in default it was cloned from, so "reset to
  default" and "diff against default" are always available.
- Bounded size (a per-template byte cap) and a stored parse status guard against
  runaway or broken bodies.

### Render path

- A new renderer in `internal/portaltheme` compiles the operator template with
  raymond against a **fixed, safe view model** — the same struct the Go default
  already builds (query, results with escaped fields, pagination, greeting,
  base URL, feature flags) — and a **curated helper allowlist** (formatting,
  URL-encode, pluralize, truncate). No helper touches the filesystem, network,
  environment, or process.
- When no operator template is active, or the active one fails to parse **or**
  fails at render time, the portal renders the existing Go `html/template`
  default. A bad operator template can never blank or break the public portal.
- Result fields derived from crawled content stay HTML-escaped: templates
  interpolate them with `{{field}}` (raymond auto-escapes), and the renderer keeps
  passing the already-sanitized snippet/title values the Go path uses. `{{{raw}}}`
  is available for operator-authored chrome only.

### Admin surface

- A new **Public portal** nav entry with three tabs, reusing the UI-22 ARIA
  tablist + `tabs.js` progressive-enhancement machinery:
  - **Configuration** — the portal-facing settings (the existing "Public portal"
    `settingCategory` group: OpenSearch base URL, autodiscovery/suggestions,
    HTTPS redirect, robots policy, greeting), rendered with the same per-setting
    forms the Configuration page uses.
  - **Portal design** — GrapesJS + CodeMirror editing the search-page template.
  - **Results design** — GrapesJS + CodeMirror editing the results-page template.
- Saves are admin-authenticated and CSRF-protected like every other console write,
  and recorded as config events.
- The visual editor uses the light console palette. Operator-authored shared CSS
  is applied directly to its iframe, while only styles created by GrapesJS are
  stored between the `grapes:start` and `grapes:end` markers. Switching between
  visual and code modes therefore preserves cascade order, inherited colors,
  CSS variables, formatting outside the marker, and the public page background
  without duplicating stale rules.

## Consequences

### Security

- **Trust boundary.** Templates are authored only by an authenticated operator
  behind CSRF — the same actor who already sets every other config value and could
  already replace the binary. Editing the portal's own markup is a theme edit by a
  fully-trusted principal, not an untrusted-input path.
- **No SSTI-to-RCE.** raymond is logic-less: templates interpolate the bounded view
  model and call only allowlisted helpers. There is no expression evaluation that
  reaches Go code, the filesystem, or the environment.
- **Bounded output XSS.** Full-template editing means an operator *can* place markup
  on the public page, but the public portal keeps its existing CSP, so operator
  inline scripts do not execute unless the operator also, separately, loosens the
  portal CSP. The design surface is layout, markup, and CSS — not arbitrary JS.
  Crawled-content fields stay auto-escaped. This residual (an operator defacing
  their own portal) is accepted and documented.
- **Availability.** The default-fallback render path means a malformed or failing
  operator template degrades to the built-in portal instead of taking the public
  surface down. Template bodies are size-capped.

### Engineering

- Three new dependencies, isolated: one Go module used only by the new
  `portaltheme` renderer, two vendored JS/CSS asset sets loaded only on two admin
  tabs. `go.mod`/`go.sum` and `deploy` SBOM/licences grow accordingly (all MIT/
  compatible).
- The Go `html/template` portal is **retained**, now doing double duty as the
  fallback renderer and the source of the "reset to default" bodies, so the
  existing `internal/publicportal` behaviour and its tests remain the safety net.
- Delivered in vertical slices behind this ADR: (1) the Public-portal admin section
  with the Configuration tab and scaffolded design tabs (no new deps); (2) the
  `portaltheme` store + raymond render path with fallback; (3) the vendored GrapesJS
  + CodeMirror design tabs wired to the store.

### Rejected alternatives

- **Client-side Handlebars.js** — would make the public portal require JavaScript,
  breaking the ADR-0020 no-JS guarantee, and ship a templating runtime to every
  visitor. Rejected per the operator's rendering choice.
- **Slot/block-only editing inside a fixed skeleton** — smaller XSS surface, but the
  operator chose full-template control; the CSP-bounded output and default fallback
  make full templates acceptable.
- **A hand-rolled templating subset** — avoids a dependency but reinvents a
  specified language the editors already emit, and would desync the visual and code
  editors from what the server understands.
