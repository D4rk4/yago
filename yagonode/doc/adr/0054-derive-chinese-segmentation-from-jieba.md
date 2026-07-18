# 54. Derive Chinese segmentation from the Jieba dictionary

Date: 2026-07-17

## Status

Accepted

## Context

Reliable Chinese word boundaries require language tailoring or a dictionary; Unicode word boundaries
alone deliberately do not provide lexical segmentation for Chinese. The local index needs useful word
terms for ranking without making a large runtime tokenizer or a network download part of node startup.
The source data must permit redistribution inside generated project data.

## Decision

Derive the Chinese segmentation lexicon from Jieba tag `v0.42.1`, commit
`1e20c89b66f56c9301b0feed211733ffaa1bd72a`, under the MIT license. The exact `dict.txt` source has
SHA-256 `7197c3211ddd98962b036cdf40324d1ea2bfaa12bd028e68faa70111a88e12a8`; its license has SHA-256
`18ba0984839f85853b29fadaf992f7dba8fd0ca0fbeae34de2b8735222dc7a37`.

Generation retains unique terms of two through sixteen Unicode code points containing only Han
characters. The resulting 337,394-term vellum transducer is 2,487,101 bytes and has SHA-256
`2ee466411a50c6c9f2039896e5d8bb64b706215c5abae2cce2abe07cc7ebce9d`. Frequencies and part-of-speech
labels are not copied. The exact upstream MIT notice is distributed in
`yagonode/internal/searchindex/CJK_DICTIONARY_NOTICES.txt`.

## Considered alternatives

CC-CEDICT was rejected before release because CC BY-SA terms embedded in generated project source create
avoidable redistribution ambiguity. GSE and Jiebago runtime dictionaries were rejected for
measured initialization cost. An unsupervised segmenter was rejected because it adds training state and
does not provide deterministic startup behavior.

## Consequences

Chinese documents gain optional longest-path dictionary terms alongside mandatory unigrams and bigrams.
The generated lexicon is immutable, deterministic, and lazy-loaded. Updating Jieba requires a new pinned
snapshot review, source and output checksums, regenerated notices, recall tests, and fresh startup/RSS
measurements. Dictionary coverage affects ranking quality but cannot veto retrieval.
