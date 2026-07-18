# 55. Derive Japanese segmentation from SudachiDict Small

Date: 2026-07-17

## Status

Accepted

## Context

Japanese text mixes Han, Hiragana, and Katakana without mandatory spaces. Unicode segmentation exposes
script units but does not provide reliable lexical boundaries. The search index needs a compact,
redistributable lexicon without loading a complete morphological runtime or the much larger Core and Full
proper-name dictionaries.

## Decision

Derive the Japanese segmentation lexicon from SudachiDict Small tag `v20260428`, commit
`3ae9201a0ab8ccdc9d048904f0902cd162f22d19`. The exact official raw source archive
`sudachidict-raw/20260428/small_lex.zip` has SHA-256
`0c8cc6febab6beac3bb3d7374d74e0123fc16bd93024759808358f63d74b3c09` and contains
`small_lex.csv`.

SudachiDict is Apache-2.0. The Small dictionary contains UniDic data under its permissive three-clause
notice; the exact upstream `LEGAL` file has SHA-256
`725a8776b38e058b185e905594bc9a2437dbf3787df022fffeefedb9a84e4665`. Both notices and the Apache-2.0
license are distributed in `yagonode/internal/searchindex/CJK_DICTIONARY_NOTICES.txt`.

Generation retains unique surface forms of two through twenty-four Unicode code points containing only
Han, Hiragana, Katakana, or the prolonged sound mark. The resulting 561,859-term vellum transducer is
1,920,456 bytes and has SHA-256
`38b3f4e6d3531381508b4a0483384ce7675bf523cb53b7f091dc2c92952ab954`.

## Considered alternatives

JMdict was rejected before release because CC BY-SA terms embedded in generated project source create
avoidable redistribution ambiguity. SudachiDict Core and Full were rejected because their
additional proper nouns increase the generated and resident footprint beyond this lexical-boundary need.
A Sudachi runtime dependency was rejected because Yago needs prefix membership, not full morphological
attributes or lattice output.

## Consequences

Japanese documents gain optional dictionary segments spanning mixed Han and Kana while mandatory
unigrams and bigrams retain containment recall. The lexicon loads only when Japanese document analysis or
dictionary-aware ranking first needs it. Updating the snapshot requires license review, checksum and
notice updates, regenerated data, adversarial containment tests, and repeated startup/RSS measurements.
