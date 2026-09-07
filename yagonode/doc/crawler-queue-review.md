# Crawler queue review

The node retains crawl orders and authorizes their leases. The crawler retains
page-level frontier state, politeness deadlines, and terminal settlements.
These queues have different ownership and restart requirements.

The review found and corrected a notification defect in the node's order queue:
several publications could collapse into one wakeup, leaving an idle crawler
asleep while another held queued work. Queue changes now wake all current
waiters. Receivers capture the change generation before checking eligibility,
so a publication between the check and sleep cannot be lost. Waiting receivers
still respect cancellation and per-session lease capacity.

The node's unused worker-wide heartbeat and its separate renewal cache were
removed. Live heartbeats already renew explicit lease identities with worker
and session checks and avoid unnecessary durable writes. The crawler's progress
and lease-settlement retries now share one bounded jitter calculation; the
one-line timer wrapper was removed.

The terminal outbox and session fences remain necessary. A terminal settlement
must survive a crash between node acknowledgment, local frontier deletion, and
confirmation. A replacement session must reject stale work while allowing
durable work to resume. Progress reports are coalesced and bounded independently
from these durable terminal decisions.

Validation covers a queued batch with two idle receivers, canceled waits,
session capacity, lease ownership and expiry, frontier/checkpoint operations,
settlement retries, and real node-only and crawler-only restarts. The existing
robots, politeness, private-network denial, and browser trust boundaries remain
part of the crawler test suite.
