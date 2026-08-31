# 0083. Shuffle tests and smoke fuzz targets

Date: 2026-08-31

## Status

Accepted

Amends [ADR-0003](0003-automated-quality-gate.md).

## Context

Package tests normally run in source order, so hidden process-state dependence
can survive repeated green runs. Existing fuzz targets run only their seed
corpus during ordinary `go test`; active coverage-guided input generation needs
an explicit `-fuzz` invocation and otherwise has no natural stopping time.

An unbounded fuzz campaign is unsuitable for a release gate. A short smoke does
not establish fuzzing depth, but it proves that every target can initialize,
consume generated inputs, and stop inside a controlled budget.

## Decision

Add a `test-shuffle` prerequisite that reruns every module with `-count=1` and a
positive tracked `int64` seed from `tools/test-shuffle-seed`. Print the seed in
the gate log so the order is reproducible. Rotate the seed only as a reviewed
change; never use `-shuffle=on` in the blocking gate.

Discover fuzz targets from module test files, confirm their names through
`go test -list`, and actively run each exact target. Give each target five
seconds and one worker. The runner accepts only integer budgets from one through
30 seconds, fails when no target exists, and propagates target, discovery, and
build failures. Ordinary tests and seed-corpus execution remain separate gate
steps.

These controls use the pinned Go toolchain and add no third-party dependency.
Add policy tests that admit a bounded discovered target and refuse an empty
target set, a failing target, an invalid or excessive fuzz duration, an invalid
shuffle seed, and removal of either direct prerequisite.

## Consequences

Every release exercises one reproducible non-source test order and proves that
all current fuzz targets survive active input generation. `-count=1` prevents a
cached result from satisfying the shuffle run, while a single fuzz worker keeps
CPU demand bounded.

One shuffle order cannot reveal every ordering dependency. A five-second fuzz
smoke is not a sustained campaign, is nondeterministic, and may find no new path
or defect. Longer scheduled or developer-driven fuzzing remains valuable. No
runtime behavior, dependency, setting, environment variable, service, stored
format, contract, or wire shape changes.
