# ADR 0089: Parse robots rules in the independent crawler

## Status

Accepted

## Context

The replacement crawler must honor site-published robots rules. Correct
handling includes partial parsing, user-agent group selection, and different
policy for unavailable and unreachable responses. Implementing the file format
ad hoc would create a second parser with avoidable interoperability risk.

## Decision

Use the pinned `github.com/temoto/robotstxt` v1.1.2 parser in `yago-crawler2`.
Apply RFC 9309 response handling in the crawler: parse successful responses,
allow unavailable 4xx responses, and disallow access when robots is
unreachable. Cache rules by origin for at most 24 hours.

## Consequences

The dependency is isolated to the crawler module. It adds no service, wire
protocol, or deployment setting. Robots policy remains part of the worker's
fetch admission path.
