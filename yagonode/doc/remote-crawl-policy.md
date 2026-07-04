# Remote Crawl Policy

Remote crawl is a YaCy compatibility surface that lets peers exchange crawl
work. It is security-sensitive because accepting a peer-supplied URL can make
the node fetch arbitrary network locations.

## Default Behavior

Remote crawl execution is disabled by default.

`/yacy/urls.xml?call=remotecrawl` returns a YaCy-compatible XML response with
`ok` and no crawl items. This tells compatible peers that the endpoint exists
without giving them crawl work.

`/yacy/crawlReceipt.html` accepts the YaCy wire shape. Network-auth failures
produce no delay field. Same-network malformed or wrong target hashes return
YaCy's rejected-receipt retry delay. Addressed receipts return the same delay
while remote crawl execution is disabled. It does not create, schedule,
continue, or acknowledge executable crawl work.

`/yacy/urls.xml?call=urlhashlist` remains available. It only returns URL
metadata already stored by the node for requested URL hashes. It does not fetch
the requested URLs.

## Risk

A remote peer can submit URLs that target private networks, loopback services,
link-local metadata addresses, internal DNS names, or unexpected protocols. If
the node fetched those URLs without policy controls, the remote peer could use
the node as a server-side request forgery path, a port scanner, or an abuse
relay.

## Policy Required Before Enabling

Remote crawl execution must stay disabled unless all of these controls exist:

- only trusted peers can request or receive remote crawl work;
- each peer has bounded URL count, depth, body size, deadline, and rate limits;
- only `http` and `https` schemes are allowed;
- destinations must match positive domain or IP range allowlists;
- loopback, link-local, private, multicast, unspecified, and cloud metadata
  destinations are rejected unless an operator explicitly allowlists them for
  an intranet deployment;
- DNS resolution is checked at validation and fetch time to avoid rebinding and
  time-of-check/time-of-use gaps;
- redirects are disabled or revalidated at every hop;
- crawl receipts cannot extend or create work outside the original accepted
  policy bounds;
- accepted and rejected remote crawl decisions are visible through stable logs
  and metrics.

## Compatibility Target

The disabled default preserves YaCy wire compatibility while staying closed for
execution. A future enabled mode must keep the same endpoint paths and response
shapes, but only after the operator configures the trust and destination policy.
