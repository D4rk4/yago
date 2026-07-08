# Vendored third-party web assets

The admin console serves every browser asset from its own origin (the admin CSP
is `script-src 'self'`; no CDN). Each vendored file carries its pinned version
and license in a `/*! ... */` header on its first line. This manifest is the
deploy-facing summary required by the Dependency Rule (ADR-0001, applied to
bundled assets by ADR-0033).

| Asset | Version | License | Source | Files (`yagonode/internal/adminui/assets/`) |
| --- | --- | --- | --- | --- |
| htmx | 2.0.4 | 0BSD | https://htmx.org | `htmx.min.js` |
| GrapesJS | 0.21.13 | BSD-3-Clause | https://github.com/GrapesJS/grapesjs | `vendor/grapes.min.js`, `vendor/grapes.min.css` |
| grapesjs-preset-webpage | 1.0.3 | BSD-3-Clause | https://github.com/GrapesJS/preset-webpage | `vendor/grapesjs-preset-webpage.min.js` |
| CodeMirror | 5.65.21 | MIT | https://codemirror.net/5/ | `vendor/codemirror.min.js`, `vendor/codemirror.min.css`, `vendor/cm-xml.min.js`, `vendor/cm-javascript.min.js`, `vendor/cm-css.min.js`, `vendor/cm-htmlmixed.min.js`, `vendor/cm-handlebars.min.js`, `vendor/cm-multiplex.min.js` |

First-party asset files in the same tree (`carbon.css`, `photon.css`,
`autocomplete.js`, `tabs.js`, `portal_designer.js`, `portal_designer.css`) are
project code under the repository license (AGPL-3.0).

GrapesJS and CodeMirror load only on the admin Public-portal design tabs
(ADR-0033); no public page references them.

The `assets/vendor/` directory is excluded from Semgrep static analysis via the
repository `.semgrepignore`: the pinned upstream bundles are minified foreign
code whose audit findings are not actionable here. First-party asset files are
scanned as usual.
