Lightweight Go senior YaCy node for DHT RWI storage and serving. Spec: `doc/specification.md`.

## Code structure

Follow the Open/Closed Principle: add features in new files and wire them in with the minimal seam,
rather than growing existing files.

## Logging

Message is a stable constant, variable data goes in key/value fields. Happy paths log at `DEBUG`;
sad paths at a severity matching operational impact (`WARN` recoverable, `ERROR` needs attention).

## Comments

No comments are allowed without explicit user approval. Let naming and structure explain the code, and
put any prose that knowledge needs in `doc/` (godoc package doc comments are permitted as docs). If you
believe a comment is unavoidable, ask the user first rather than adding it.

## Single source of truth

Comments, error messages, logs, and similar text must not duplicate a fact that already lives
elsewhere (a constant, config var, or doc), as the copy becomes a second source of truth that drifts.

## Documentation

Each document must be self-contained, concise, and written in plain language for the user. Link to
related docs for navigation only; avoid cross-document dependencies, duplicate facts, internal jargon,
implementation details, and rationale.

## Naming

Every name — package, file, type, interface, port, function, method, field, variable —
states a single bounded responsibility, so a reader can predict from the name alone what
does and does not belong behind it, and the named thing cannot quietly absorb unrelated
members. Prefer long and bounded over short and generic; never `util.go`, `helpers.go`,
`handler.go`, or `types.go`. Reject vague umbrella nouns that name a topic or layer rather
than a role (e.g. `Store`, `Manager`, `Service`, `Handler`, `Util`, or a domain topic such
as `Distribution*` used as a catch-all). If a name's boundary cannot be stated in one
sentence, the abstraction is wrong — fix the abstraction, not just the name.

## Dependencies

Every newly introduced third-party dependency is recorded in its own ADR before use.

## Version pinning

All dependency versions are pinned: runtime dependencies through `go.mod`, build and lint tools through
Go `tool` directives in `go.mod`. `make verify` runs only those pinned tool versions, never whatever is
on `PATH`.

## Testing

Code lands with tests. `make verify` runs the test suite and a coverage scan; a change is not done
until both are green and total coverage stays at or above the configured threshold.

When coverage falls below the threshold, the default response is to remove or refactor code, not to
add tests. First find the uncovered statements and branches and ask whether they should exist at all:
delete dead or defensive-only code, collapse a branch that no caller exercises, or restructure so one
covered path replaces several. Reduce code first; write tests only for behaviour that genuinely must
exist and is not yet exercised. A test written solely to lift the number, asserting behaviour no caller
depends on, is a filler test and counts as a failure, not a fix.

## Feedback loop

`make verify` is the single gate; a change is not done until it is green.
