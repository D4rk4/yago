# 1. Use golang.org/x/sync/errgroup for server and worker lifecycle

Date: 2026-07-09

## Status

Accepted

## Context

Every service runs a set of HTTP servers alongside background workers and must
stop them all when the process is signalled or when any one of them exits. Each
service had reimplemented this with hand-rolled goroutines, channels, and
`sync.WaitGroup`, and the versions had drifted apart. The shared `servergroup`
package unifies them behind one primitive that waits on many goroutines,
captures the first error, and cancels the rest.

## Decision

Use `golang.org/x/sync/errgroup` as the concurrency primitive inside
`servergroup`. It provides exactly the needed behaviour: run a group of
functions, cancel a derived context when the first returns an error, and return
that first error from `Wait`.

## Consequences

`golang.org/x/sync` becomes a direct runtime dependency of `serviceruntime`. It
was already an indirect dependency in the tree, so no new module enters the
build graph. The lifecycle logic lives in one tested place instead of five.
