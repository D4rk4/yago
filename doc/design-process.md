# Design process

Design a module before code: boundaries and vocabulary settled, every weakness fixed or accepted on record.

**Standing rule — challenge everything.** Push back on all decisions, including accepted ones and requester claims; resolve on evidence, not deference. Reopening a recorded decision requires evidence the record did not weigh.

## 1. Ground every claim

No facts from memory or guess. Resolve each unknown against a source of truth — codebase, protocol, or cited external reference. Mark what stays unresolved.

## 2. Design abstractions, not algorithms

Decide boxes and lines: one bounded responsibility per element, domain-expert vocabulary, what crosses each boundary. Put replaceable methods behind narrow interfaces — the interface is the design decision; its internals are deferred.

Deferring ≠ skipping: deep-dive the implementation reality first (protocol, data structures, actual value semantics). A boundary must reflect how the thing behaves.

## 3. Draft

Write a short record: boxes, boundary contracts, decisions with reasons.

## 4. Stress

Independent adversarial reviewer — separate person, or clean-context agent. Instruction: attack, do not summarize. Reviewer sees only the current design, never prior rounds. Stressors are arbitrary and cross-category: technical, operational, adversarial, economic, human.

Attack the frame before the content:

- **Altitude** — is each named element a boundary/contract, or an algorithm internal posing as a design decision? Deletion test: if replacing the enumerated mechanisms with a single port contract strengthens the design, they were never design.
- **Symmetry** — for each foregrounded option, what makes it load-bearing while siblings are deferred? No principled criterion → state the admission predicate instead.
- **Premises** — which constraints treated as fixed are contingent (who operates a dependency, where it runs, whether a store is rebuildable)? Name the deployment where each cost collapses.
- **Excluded classes** — name the solution category the framing silently removed.

Every finding names a concrete failure path (§1 applies). Classify each:

- **Residue** — what breaks, silently degrades, or keeps working.
- **Structural weakness** — missing component, wrong boundary, trust hole, unbounded resource, observability gap.
- **Abstraction-level defect** — right thing at wrong altitude: internal frozen as decision, arbitrary selection posed as designed, contingent premise treated as fixed.
- **Attractor** — stable bad state reached under sustained stress through individually reasonable steps.

Reviewer also states what could not be broken.

## 5. Iterate

Owner triages each finding: absorb into the design, accept as named residual (§6), or reject as implausible. Re-stress with a fresh clean-context reviewer. Owner — not reviewer — judges structural breakage vs refinement and owns the stop: end when rounds yield only refinements and accepted residuals.

## 6. Fixes vs limits

Apply boundary-fixable findings. For structural limits, never fake a defense: record as a named residual risk with its cost stated honestly to the consumer.

## 7. Land

Move settled decisions and residual risks into the durable record (RFC or ADR) as the single source of truth. Delete working notes once the record carries their conclusions.
