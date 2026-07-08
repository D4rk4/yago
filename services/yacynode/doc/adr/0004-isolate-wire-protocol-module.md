# 4. Keep the YaCy models and protocol in standalone, reusable modules

Date: 2026-06-17

## Status

Accepted

## Context

The YaCy data model — seed strings, word hashes, RWI postings — and the rules that validate it
are the heart of this node, but they are not unique to it: any project that speaks YaCy needs
the same models and the same validation. The same is true one level up, of the peer-to-peer
message types: the per-endpoint request and response shapes exchanged over the `/yacy/*`
protocol. This node both serves those messages and sends them as a client, so the message types
are shared by more than one layer and must not be owned by any single layer. We do not want to
copy either the models or the messages around.

## Decision

The YaCy types live in two standalone Go modules, joined to the node by a `go.work` workspace.

`github.com/nikitakarpei/yacy-rwi-node/yacymodel` holds the value-level models, their validation
rules, and their codecs. It depends on the standard library only.

`github.com/nikitakarpei/yacy-rwi-node/yacyproto` holds the peer-to-peer message types: the
per-endpoint request and response data transfer objects of the `/yacy/*` protocol. It builds on
`yacymodel` and the standard library, nothing more.

Every layer of the node builds on `yacymodel`, `core` included: these are the domain's models,
not a transport detail to hide at the edges. The message types in `yacyproto` are a transport
concern: `api` and `infrastructure` use them at the boundaries that admit or emit peer messages,
and `core` does not — it operates only on validated models.

## Consequences

Other projects can `go get` either module unchanged, and the node shares one definition of each
model and each message across all the layers that touch it. Because neither module imports
anything of the node's, neither can import inward; `yacyproto` may build on `yacymodel`, but not
the reverse. The cost is two modules to version and a `go.work` file to keep in sync
(`go work sync`).
