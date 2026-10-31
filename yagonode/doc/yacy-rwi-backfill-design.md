# YaCy RWI backfill design

## Source and destination

The source is Yago's existing document store after the YaCy JSONL import. The
backfill reads each stored document through the document-store owner and writes
YaCy-compatible URL metadata and reverse-word postings through the node's
existing URL-metadata and RWI intake owners. It does not contact YaCy, read
YaCy files, write document content, update Bleve, or publish crawl work.

## Field mapping

The normalized URL supplies the URL identity and hash. Extracted text is
tokenized by a deterministic tokenizer kept equivalent to the crawler's
tokenizer by regression tests; the stored title is tokenized separately for URL
metadata word counts. The
stored language, text byte and word counts, same-host and external link counts,
fetch time, modified time, title, and supported `yacy.fresh_until` metadata
populate the existing YaCy-compatible RWI properties and URL metadata fields.
The ordinary shared artifact builder is used by both this backfill and the
crawler.

The original YaCy RWI postings were not part of the document export. Therefore
this recreates postings from the retained text; it cannot reproduce YaCy's
original tokenization, morphology, ranks, or exact posting bytes. Typed policy
and collection fields are also not added to the RWI protocol. Their imported
document metadata remains intact in the document store. Local search continues
to use the document store and Bleve, not RWI.

The source export does not provide exact per-URL URLDB outcomes or a reliable
next-visit schedule for every record. The backfill writes the existing
URL-metadata fields but does not create or alter Yago recrawl schedules. It
does not infer that a document should be fetched again.

## Bounded recovery

The document scanner captures a high-water key for each existing storage
partition and walks bounded pages with a versioned continuation token. A
checkpoint records the cursor only after the matching URL metadata and postings
have both been accepted. URL metadata is written first because posting intake
validates URL references. If the process stops between those writes, or after
both writes but before the checkpoint, resuming repeats the page through the
existing idempotent receivers. It never re-fetches or re-enqueues the URL.

The controller yields its document-scan admission at each bounded write batch,
then resumes from the durable cursor. This leaves the scan slot available to
other bounded maintenance scans during a large backfill.

The controller starts dormant. An explicit authenticated admin action starts a
bounded pilot or a full pass; pause and resume retain the checkpoint. A pass
that reaches its captured storage boundary is marked complete. An operator may
start another complete pass to reconcile documents added during the earlier
scan; existing postings and URL metadata remain idempotent. A running checkpoint
resumes after process restart.

The reported generated-byte estimate is the encoded row size, not measured disk
growth. Before a production pass, back up the Yago data directory, verify the
backup, and measure a bounded write pilot against free space, memory, storage
growth, local search latency, RWI counts, and URL-metadata counts. Keep YaCy's
data untouched. This feature does not change the DHT transfer gate; peer
distribution remains a separate operator decision.
