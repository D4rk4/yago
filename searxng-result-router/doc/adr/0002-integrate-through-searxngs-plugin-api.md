# 2. Integrate through SearXNG's plugin API

Date: 2026-07-07

## Status

Accepted

## Context

Rewriting result links requires running inside SearXNG's request/response cycle. SearXNG
exposes exactly one supported extension point for this: a `Plugin` subclass, discovered by
`searx.plugins.PluginStorage.load_settings` from a fully-qualified class name in
`settings.yml`, whose `on_result(request, search, result)` hook runs once per result and may
modify the result in place before it reaches the page. `Result.filter_urls(filter_func)` is
the API's own mechanism for rewriting URL-shaped fields; `filter_func` receives the result,
the field name, and the URL, and returns either a replacement URL or a boolean keep/drop.

## Decision

`result_link_router.py` defines a single `SXNGPlugin(searx.plugins.Plugin)` class. Its
`on_result` calls `result.filter_urls` with a callback that rewrites only the `url` field
(the result's primary destination link, not thumbnails or embedded media) when it is an
absolute `http(s)` URL, and leaves every other field and every non-`http(s)` URL unchanged.

An operator wires the plugin in by adding `result_link_router.SXNGPlugin` (with the module on
SearXNG's Python path) to `settings.yml`'s `plugins:` section — no SearXNG rebuild required.

## Consequences

The plugin's shape is dictated by SearXNG's API, not chosen freely: the class name, method
signatures, and `filter_urls` field-name set (`url`, `iframe_src`, `audio_src`, `img_src`,
`thumbnail_src`, `thumbnail`, plus infobox fields) come from `searx.plugins`/`searx.result_types`
and change only if SearXNG changes them. Restricting rewriting to the `url` field is a
deliberate scope choice, not an API constraint: `yacyvisitcrawl` treats a visit as "a person
went to this page", which only the primary result link represents.
