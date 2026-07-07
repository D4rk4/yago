# 1. Record architecture decisions

Date: 2026-07-07

## Status

Accepted

## Context

yacyvisitcrawl introduces several third-party dependencies and structural choices. We want a
durable, reviewable record of why each was made.

## Decision

We record architecture decisions as short Markdown files in `doc/adr/`, one per decision,
numbered sequentially and written in the Nygard format. Every new third-party dependency gets
its own ADR before it is used.

## Consequences

Each dependency and significant structural choice is traceable to a dated rationale.
