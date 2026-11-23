# 0090. Evaluate relaxed minimum match only at complete candidates

Date: 2026-11-23

## Status

Accepted

Amends [ADR-0078](0078-use-requirement-aware-lexical-candidates.md).

## Context

A long query uses a relaxed minimum match. The node first reads a complete
lexical candidate snapshot. Then it scores the ranked query only at those
candidates. The ranked query contains the same minimum match. Bleve moves each
conjunction child forward after every hit. The minimum match then examined every
driver document between two candidates. The scoring pass thus repeated most of
the snapshot work.

On production, this minimum match used 58 percent of the search CPU. Long
queries reached the exact-stage deadline with no rows.

## Decision

When the candidate snapshot is complete, the scoring pass gives its candidate
identities to the minimum match. The minimum match examines only those
identities, in index order. It does not walk its rarest-term drivers.

An incomplete snapshot keeps the previous driver walk. Search without a snapshot
keeps the previous driver walk. Hits, scores, and term locations do not change.

## Consequences

On a local corpus of 105,000 documents and 12 synthetic long queries, the
minimum match examined 170,353 documents instead of 293,918. Hits and scores
were identical. The local wall time did not show a clear change. The production
effect must be measured after deployment.
