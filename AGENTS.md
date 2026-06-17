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

## Feedback loop

`make verify` is the single gate; a change is not done until it is green.
