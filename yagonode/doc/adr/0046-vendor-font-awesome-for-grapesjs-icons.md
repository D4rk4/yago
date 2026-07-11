# 46. Vendor Font Awesome for GrapesJS legacy controls

Date: 2026-07-11

## Status

Accepted

## Context

GrapesJS 0.21.13 renders most editor controls as inline SVG, but some controls
still use Font Awesome classes. The portal editor's block-category caret is one
such control. Disabling GrapesJS's default Font Awesome CDN stylesheet prevents
network egress but leaves that caret with zero width and no visible icon.

The editor must remain complete without a runtime CDN, external API, sidecar,
or separately installed browser asset.

## Decision

Vendor Font Awesome 4.7.0 from the official FortAwesome release. Keep its CSS
under the embedded admin assets and its WOFF2 webfont under the matching local
font path. The CSS is licensed under MIT and the font under SIL OFL 1.1.
GrapesJS receives the same-origin stylesheet URL through `cssIcons`, and the
portal-only content security policy permits fonts from `self` while retaining
`default-src 'none'`.

Only the WOFF2 source is retained in the local `@font-face` rule. Current
supported browsers use that format, so legacy EOT, WOFF, TTF, and SVG font
copies would add unused files and fallback requests.

## Considered alternatives

Leaving `cssIcons` empty was rejected because it produces blank legacy
controls. Replacing individual Font Awesome classes with first-party CSS or
SVG was rejected because GrapesJS can create those controls dynamically and a
local patch list would drift from the pinned library. Keeping the upstream CDN
default was rejected because the editor must have no runtime internet
dependency.

## Consequences

The complete GrapesJS control set renders from embedded same-origin assets and
works offline. The admin image grows by the bounded CSS and WOFF2 files. A
future GrapesJS upgrade may remove the legacy classes; the dependency can then
be removed after a browser-level icon audit.
