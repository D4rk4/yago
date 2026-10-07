# Clearing crawl queues

Open **Admin → Crawler**. The crawl monitor shows two kinds of waiting work:

- **Pending orders** are crawl tasks waiting on the node for a crawler to claim them. **Leased orders** have already been handed to a crawler.
- **Pending URLs** are pages waiting inside one crawl run on a crawler.

## Clear pending orders

Select **Clear pending orders** beside the order queue status. Review the confirmation page, type `CLEAR PENDING ORDERS` in the confirmation field, and submit the form.

After preparing the queue, the node fixes a deletion boundary. The action removes orders already pending at that boundary that have not since been claimed. It works without a connected crawler. Leased orders and orders accepted after that boundary remain. Running crawls continue, and indexed documents, crawl schedules and settings are preserved.

The result reports how many orders were removed. If the operation stops partway through, completed removals remain in effect and the page reports the partial result. Run the action again to clear the remaining pending orders. Refreshing the monitor shows the current queue, which can also change as crawlers claim work or new orders arrive.

## Clear a run's pending URLs

Find the running crawl in the monitor and select **Cancel** in that row. The crawler removes that run's waiting URLs and rejects further discovered links for the cancelled run. In-flight page processing finishes before the run settles as cancelled. Other runs and indexed documents remain.

This control is delivered to the run's crawler. If that worker is disconnected, cancellation waits for it to reconnect. **Pause** retains pending URLs for **Resume**; it does not clear them. A cancelled or finished row remains in recent history, and **Restart** submits a new run.

## Prevent the queues from filling again

Pause or delete unwanted recurring schedules on the Crawler page. Review **Greedy learning** and **Web-discovery crawling** under **Admin → Configuration → Crawler** if automatic discovery is producing the work. Disabling a producer affects future submissions; it does not remove orders already queued. Previously indexed pages may also have recrawl schedules.

Stopping or restarting either service preserves its durable queue. Queue clearing is an explicit operator action, separate from service restart and index deletion.
