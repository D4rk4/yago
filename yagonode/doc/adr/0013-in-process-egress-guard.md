# 13. Screen outbound connections with an in-process dial-time guard

Date: 2026-07-03

## Status

Accepted

## Context

Both the node and the crawler open connections to hosts they learn at runtime:
the node greets peers, fetches seed lists, probes and transfers to DHT targets;
the crawler fetches arbitrary web pages. Any of those hosts can resolve to a
private, loopback, link-local, or cloud-metadata address, so the process needs
server-side request forgery (SSRF) protection on every outbound connection.

The prototype stack delegated that protection to an external forward proxy
(Smokescreen): the node had no in-process guard and routed all outbound traffic
through the proxy, and the crawler routed through it as well while additionally
screening hosts before fetch. This added a mandatory sidecar service to every
deployment, made the node non-functional without a configured proxy URL, and
still left a gap for the crawler's headless browser, whose own DNS resolution
happened after the pre-fetch host check (a DNS-rebinding window).

## Decision

Screen outbound targets in-process at dial time. A shared `yagoegress` module
provides a `Guard` that admits or rejects a connection by the resolved IP
address the operating system is about to connect to. The guard is installed as
the `net.Dialer.Control` hook of each component's outbound HTTP client, so the
check runs on the concrete address after name resolution and a name that
resolved to a public address at admission time cannot be rebound to a private
one before the dial.

The node builds its single outbound client from the guard. The crawler builds
its fast-path HTTP client from the guard, keeps its pre-fetch `publicweb`
admission (which now delegates the per-address decision to the same guard), and
routes the headless browser through a loopback-bound in-process forward proxy
that resolves and dials every target through the guard. No external forward
proxy remains in the stack.

Private networks (RFC 1918 and unique-local) are blocked by default because a
public swarm peer or web origin never lives behind one. Deployments on a LAN or
a private YaCy network opt back in with `YAGO_EGRESS_ALLOW_PRIVATE_NETWORKS`
(node) or `YAGOCRAWLER_ALLOW_PRIVATE_NETWORKS` (crawler). Loopback, link-local
(including the cloud-metadata range), carrier-grade NAT, multicast, and reserved
ranges stay blocked in either mode.

## Considered alternatives

Keeping an external forward proxy was rejected: it is an extra always-on service
on Raspberry-Pi-class hosts, it makes the node depend on a correctly configured
proxy URL to function at all, and its allow-list is managed separately from the
code that knows which destinations are legitimate.

A pre-fetch host admission check alone (resolve the name, screen the addresses,
then fetch) was rejected as the sole mechanism because the actual dial re-runs
name resolution, leaving a DNS-rebinding gap. The pre-fetch check is retained in
the crawler as defense in depth, but the authoritative decision is made at dial
time on the connected address.

## Consequences

`yagoegress` becomes a shared dependency of the node and the crawler and the
single source of truth for outbound address screening. Operators run one fewer
service and no longer configure a proxy URL. Because the headless browser cannot
use a Go dialer directly, the crawler runs a small in-process forward proxy on
loopback purely to interpose the guarded dialer; it terminates with the browser
allocator. The default-deny stance on private networks means LAN and
private-network operators must set the opt-in environment variable, matching the
external proxy's previous default-deny behaviour.
