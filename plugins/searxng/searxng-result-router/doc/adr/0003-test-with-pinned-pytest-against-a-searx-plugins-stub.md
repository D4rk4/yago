# 3. Test with pinned pytest against a searx.plugins stub

Date: 2026-07-07

## Status

Accepted

## Context

The plugin imports `searx.plugins` at module load time, but SearXNG itself is a large
application with its own heavy dependency tree; installing it just to unit-test one plugin
file would pull in far more than this module needs. The parts of `searx.plugins` the plugin
actually touches — `Plugin`, `PluginInfo`, `PluginCfg` — are small, stable dataclasses/base
classes confirmed against SearXNG's own source.

## Decision

`requirements-dev.txt` pins `pytest` and `pytest-cov`. `conftest.py` installs a minimal
`searx`/`searx.plugins` stub into `sys.modules`, matching the real classes' fields and
signatures, before any test imports `result_link_router`. Tests exercise the plugin's own
logic (link rewriting, base-URL configuration) against this stub, not against SearXNG itself.
`make test`/`make cover-check` install both into a project-local `.venv` (gitignored, rebuilt
whenever `requirements-dev.txt` changes) rather than using a system-wide install, so the pinned
versions are what actually runs.

## Consequences

Tests run fast and need no SearXNG install, but they do not catch a real SearXNG upgrade that
changes the `Plugin`/`PluginInfo`/`PluginCfg` shape; the stub must be kept in sync with
`searx.plugins` by hand.
