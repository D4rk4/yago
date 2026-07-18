# 56. Derive equal-width Chinese conversion from OpenCC

Date: 2026-07-17

## Status

Accepted

## Context

Traditional and Simplified Chinese surface forms otherwise produce different unigram and bigram terms.
Running the complete OpenCC conversion library increased local process resident memory by about 26 MiB
and first initialization by 40–60 ms. Full phrase conversion can also change the number of Unicode code
points, which would invalidate source byte offsets used by snippets, proximity, and federated evidence.

## Decision

Derive an immutable conversion transducer from `github.com/longbridgeapp/opencc` tag `v0.3.13`, commit
`eec5c563bc3271ddc2d3a4438d798f560d4187a2`, under Apache-2.0. The source tables and SHA-256 values are:

| Table | SHA-256 |
| --- | --- |
| `TSPhrases.txt` | `b2ef895dd4953b4bb77fc8ef8d26a2a9ca6d43a760ed9a1d767672cfafa6324f` |
| `TSCharacters.txt` | `6b5a0a799bea2bb22c001f635eaa3fc2904310f0c08addbff275477a80ecf09a` |
| `TWPhrasesRev.txt` | `379241d79e784a0488c9f75a20ea4203218a6bc8dfb487fe836102f6bcf3e11d` |
| `TWVariantsRevPhrases.txt` | `bef60ceb4e57b6b062351406cb5d4644875574231d64787e03711317b7e773f3` |
| `TWVariantsRev.txt` | `aed256cd515db700cbd96fae5d2158d6a98c9628ee51e14ac1e1c182f6041c90` |

Taiwan phrase and variant outputs are composed through the Traditional-to-Simplified tables. Generation
retains only non-identity mappings whose input and output contain the same number of Unicode code points.
The 4,715-key transducer is 34,118 bytes with SHA-256
`4d19a0985e9090b6e9ddcea80cae672f412bd0b3f31f891f4dd2b33315353aae`; its 18,422-byte output table has
SHA-256 `ea066a6206949ee35badc5c5bdc82aa0e6c5758760d25e0665c43d1958ca6aa3`. The upstream Apache-2.0
license is distributed in `yagonode/internal/searchindex/CJK_DICTIONARY_NOTICES.txt`.

## Considered alternatives

The complete Go OpenCC runtime was rejected for measured first-use memory and latency. Character-only
conversion was rejected because common regional phrases such as `搜尋` and `軟體` need phrase mappings.
Arbitrary-length conversion was rejected because it cannot preserve a one-to-one mapping from normalized
tokens to original source byte spans. Maintaining a project-specific conversion list was rejected as an
ad hoc language table without authoritative provenance.

## Consequences

Traditional and Simplified queries share canonical Chinese index terms in both directions while emitted
locations continue to reference the original bytes. This is deliberately a bounded canonicalization
subset, not complete OpenCC behavior. A source phrase whose conversion changes code-point count remains
unchanged. Updating OpenCC requires new table checksums, generated output hashes, offset invariants,
script-direction retrieval tests, and repeated first-use measurements.
