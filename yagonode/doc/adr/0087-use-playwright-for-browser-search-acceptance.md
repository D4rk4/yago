# 0087. Use Playwright for browser search acceptance

Date: 2026-10-23

## Status

Accepted for implementation on 2026-10-23.

## Context

The search interface progressively updates results and exposes distinct states
for partial coverage, complete empty answers, and actual failures. Go handler
tests cannot verify that the rendered form, navigation, browser stream, and
repeated search work together in real browsers.

## Decision

Add `@playwright/test` version `1.64.1` as a test-only dependency in
`yagonode/ui-tests`, with an exact version in `package.json` and an npm lockfile.
Run the search scenarios in Chromium and Firefox, and save screenshots from
both projects as test artifacts. Tests use an explicitly configured local
YagoSeek base URL and do not commit screenshots, search responses, or corpus
content.

## Alternatives considered

- Manual browser checks do not provide repeatable regression coverage.
- Selenium and WebdriverIO add different browser-automation stacks without a
  project need that offsets the extra setup.
- Browser tests without an installed, version-matched browser would not provide
  reproducible execution.

## Consequences

The package is isolated from all Go runtime modules and is used only for UI
acceptance. The test runner's license is Apache-2.0. Browser binaries are
provided by the official Playwright version-matched container or installed with
the pinned Playwright CLI. No production image, service, or runtime dependency
changes.
