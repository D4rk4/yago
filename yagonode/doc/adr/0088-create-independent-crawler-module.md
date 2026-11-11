# ADR 0088: Create an independent crawler implementation module

## Status

Accepted

The module path decision is superseded: the replacement is now the canonical
`yago-crawler` module. The independent worker and frontier decision remains.

## Context

The existing crawler module has accumulated a large frontier, dispatch,
checkpoint, and retry implementation. The operator has rejected that
implementation as the basis for further crawler development. Its executable
name is already used by the node, packages, systemd, and Compose deployments.

## Decision

Create the replacement in a distinct Go module. Build its command as
`yago-crawler` and keep the node's existing crawl exchange contract.
The new module may depend on shared model, security, and protocol modules but
must not import code from the previous crawler module. It owns a separate
persistent frontier and does not migrate or open the previous checkpoint.

## Consequences

The container build and release build select the independent module as the
source for the existing crawler product. The systemd unit, Compose service, image,
binary, environment prefix, and package names remain unchanged. The previous
module stays out of runtime builds until it can be removed without losing
required compatibility evidence.
