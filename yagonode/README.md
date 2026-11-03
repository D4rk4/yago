# YaGo node

The `yago-node` process serves the local search index, YaCy-compatible peer
endpoints, public search APIs, and authenticated Admin console.

## Crawler pulse

Open **Admin → Performance** to see connected crawler processes, fetched and
indexed page rates, active fetches, admission waits, queued orders, and current
capacity signals. Rates come from fresh worker progress reports and remain
unavailable until the node has enough measurements. The Admin console can be
routed through a trusted HTTPS proxy while the ops listener stays private.
