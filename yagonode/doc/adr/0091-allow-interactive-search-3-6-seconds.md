# 0091. Allow interactive search 3.6 seconds

Date: 2026-11-23

## Status

Accepted

Amends the time budget in
[ADR-0084](0084-supplement-incomplete-primary-search-with-bounded-web-results.md).

## Context

ADR-0084 set a total response deadline of 1.8 seconds. Long queries on
production reached the exact-stage deadline before the local index finished.
The node then returned no rows and the Tavily API returned 409. The operator
requested a total budget of 3.6 seconds.

## Decision

The total interactive search deadline is 3.6 seconds. In `Always` web mode, the
exact primary stage stops at 3.2 seconds. The web provider can use up to 3.6
seconds and stops 50 milliseconds before the request deadline.

These values do not change:

- The supplement decision point is 600 milliseconds.
- The assembly reserve is 100 milliseconds.
- The minimum web window is 1 second.

## Consequences

A slow search can take up to 3.6 seconds. A fast search returns as soon as its
work is complete. Callers with a shorter deadline keep their deadline.
