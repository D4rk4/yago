# Clearing crawl queues

Open **Admin → Crawler**. The monitor separates crawler page queues from node crawl orders:

- **URLs waiting in active crawls** sums pending URLs from each active run's latest worker report. Workers report about every five seconds. Each active crawl shows its report age; when a worker disconnects, its last reported total may be stale.
- **Orders waiting for a crawler** are crawl tasks waiting on the node. The monitor shows a bounded sample of their URLs, oldest orders first. **Orders leased to crawlers** have already been handed to a worker.
- **Active crawls** are listed separately with their pending URLs and per-run controls. Each card shows up to eight current URLs with a readable stage and elapsed time, including when a page is waiting for the node to store and index it. Each card also shows up to four next waiting URLs with their queued or deferred state, wait reason, and eligibility time. The sample updates with the worker heartbeat, about every five seconds. The collapsed recent history keeps the ten latest finished or cancelled runs, including their Restart action.

Outcome totals on the monitor cover active runs and up to 256 finished runs kept in recent history. They can decrease when an older finished run leaves that history. Use **Admin → Performance → Crawler pulse** for current fetched and indexed pages per minute and per-worker activity.

Runs whose queued pages are all waiting for host backoff release their crawler slot and keep their checkpoint. The node holds their order until the earliest host retry deadline, for up to 15 minutes. The crawler's `yacy_crawler_backoff_run_deferrals_total` metric records how often this happens. The order returns to the queue when its deadline expires.

## Clear pending orders

Select **Clear pending orders** in the node order queue tile. Review the confirmation page, type `CLEAR PENDING ORDERS` in the confirmation field, and submit the form.

After preparing the queue, the node fixes a deletion boundary. The action removes orders already pending at that boundary that have not since been claimed. It works without a connected crawler. Leased orders and orders accepted after that boundary remain. Running crawls continue, and indexed documents, crawl schedules and settings are preserved.

The result reports how many orders were removed. If the operation stops partway through, completed removals remain in effect and the page reports the partial result. Run the action again to clear the remaining pending orders. Refreshing the monitor shows the current queue, which can also change as crawlers claim work or new orders arrive.

## Clear a run's pending URLs

Find the running crawl in the **Active crawls** section and select **Cancel** in its card. The crawler removes that run's waiting URLs and rejects further discovered links for the cancelled run. In-flight page processing finishes before the run settles as cancelled. Other runs and indexed documents remain.

This control is delivered to the run's crawler. If that worker is disconnected, cancellation waits for it to reconnect. **Pause** retains pending URLs for **Resume**; it does not clear them. A cancelled or finished run remains in the collapsed recent history, and **Restart** submits a new run.

## Prevent the queues from filling again

Pause or delete unwanted recurring schedules on the Crawler page. Review **Greedy learning** and **Web-discovery crawling** under **Admin → Configuration → Crawler** if automatic discovery is producing the work. Disabling a producer affects future submissions; it does not remove orders already queued. Previously indexed pages may also have recrawl schedules.

Stopping or restarting either service preserves its durable queue. Queue clearing is an explicit operator action, separate from service restart and index deletion.
