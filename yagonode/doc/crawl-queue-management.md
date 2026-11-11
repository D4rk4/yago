# Crawler queue and status

Open **Admin → Crawler** to submit focused seed URLs and see the crawler's
reported frontier. The node stores each URL with its topic and records it as
focused demand. The crawler decides when to fetch it, what links to follow, and
when to retry or revisit it.

The pending total and URL samples come from the crawler's persistent frontier.
The live view lists active and waiting URLs reported by the worker. A report
older than three heartbeat intervals is shown as stale and is not counted as a
live queue measurement.

Open **Admin → Performance → Crawler pulse** for fetched and indexed pages per
minute, worker activity, current URLs, and a bounded sample of waiting URLs.
Rates are calculated from cumulative crawler progress reports, not from node
ingest acknowledgements or old crawl-order records.

Submitting the same focused URL repeatedly updates its demand signal. It does
not create another node-side crawl task. Existing indexed documents remain in
the index until a later crawler mutation replaces or removes them. When a new
focus is newer than the URL's last completion, the crawler queues that URL for a
fresh attempt. A fetch completed after the focus is not repeated for that signal.
If the node's demand sequence is restored behind the worker's saved cursor, the
worker restarts its incremental read so recent focus signals remain visible.
