# 4. Keep the YaCy models in a standalone, reusable module

Date: 2026-06-17

## Status

Accepted

## Context

The YaCy data model — seed strings, word hashes, RWI postings — and the rules that validate it
are the heart of this node, but they are not unique to it: any project that speaks YaCy needs
the same models and the same validation. We do not want to copy them around.

## Decision

The YaCy models, their validation rules, and their codecs live in their own Go module,
`github.com/nikitakarpei/yacy-rwi-node/yacywire`, joined to the node by a `go.work` workspace.
It depends on the standard library only. Every layer of the node builds on it, `core` included:
these are the domain's models, not a transport detail to hide at the edges.

## Consequences

Other projects can `go get` `yacywire` unchanged, and the node shares one definition of each
model and its validation across all layers. Because the module depends on nothing of the
node's, it can never import inward. The cost is a second module to version and a `go.work` file
to keep in sync (`go work sync`).
