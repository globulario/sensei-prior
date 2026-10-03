# sensei-prior — design contract

sensei-prior turns Sensei's own history into a **prior** for the architect:
what to look at, what tends to go wrong, what reviewers have already rejected.
It supplies a prior. It never supplies a decision.

This document is a contract, not a roadmap. Each section below is a rule that
code in this repository must keep true, and most of them have a test that fails
when they stop being true. When a rule and an implementation disagree, the
implementation is wrong.

## 1. No authority

> Learned prediction informs governance; it never becomes governance.

- Everything this repository emits is an advisory artifact:
  `LearnedSuggestion`, `LearnedRisk`, `LearnedContext`. No output is a
  `Decision`, `Finding`, `Evidence`, `Attestation`, `Grant`, or a claim that an
  invariant is satisfied.
- No conversion from an advisory type to an authority type exists, in either
  repository. Sensei's deterministic machinery establishes whether a suggested
  edge exists and whether a plan is legal. A suggestion is at most a reason to
  look.
- sensei-code must never depend on sensei-prior for correctness. If
  sensei-prior is absent, stale, or wrong, every governed outcome is unchanged.
  Only the size and quality of the architect's context may differ.
- sensei-prior **reads** exported Sensei data. It never writes to the Sensei
  graph, the task ledger, review stores, or any repository it reads.
- Enforced by: an import-boundary test in sensei-code once the advisory types
  land there (no authority-producing package may import the advisory package),
  plus a test in this repository asserting it has no write path to any source.

## 2. Point-in-time records only

> A training record represents only information that was available at the
> historical decision point it reconstructs. Future commits, later graph
> repairs, later reviews, later rulings, and later-derived anchors must not
> appear in it.

- `graph_snapshot` means **the graph Sensei could legitimately have known at
  that moment**, never today's graph queried about yesterday. A record whose
  snapshot cannot be reconstructed is kept, marked
  `snapshot: UNRECONSTRUCTABLE`, and excluded from any benchmark that scores
  graph-derived context. It is never filled from a later graph.
- Every field carries the time it became available. The extractor refuses to
  place a fact in a record whose `available_at` is earlier than the fact's.
- The live served store is not a historical source. It is known to have
  drifted from main (115 commits behind on 2026-09-16), so it can be neither
  current nor historical truth.
- Enforced by: an extractor test that plants a fact dated after the decision
  point and requires the record build to refuse it.

## 3. Teacher truth is graded, not assumed

A reviewer's finding is an observation, not ground truth. Findings carry their
lifecycle, and a label's strength comes from that lifecycle:

| label state            | meaning                                                   |
|------------------------|-----------------------------------------------------------|
| `reviewer_observation` | raised, nothing yet followed                              |
| `finding_upheld`       | accepted as a defect by the architect or a ruling         |
| `finding_resolved`     | upheld, a change was made, and a later review accepted it |
| `finding_disputed`     | contested, no ruling yet                                  |
| `finding_withdrawn`    | a ruling or the reviewer retracted it                     |
| `finding_superseded`   | overtaken by a later contract or ruling                   |

The strongest positive label is `finding_resolved`: upheld, change made, later
ACCEPT. A withdrawn finding is evidence *against* its claim. The extractor never
collapses these states. When the lifecycle cannot be determined, the state is
`reviewer_observation`, never promoted.

Failures are first-class. `REVISE → REVISE → ACCEPT` trajectories and
non-convergent objectives stay in the corpus, because the transformation from a
rejected structure to an accepted one is the signal a critic needs.

## 4. Deterministic first

No neural component exists until a deterministic baseline exists and has been
measured. There is no `models/` directory and no Python until then.

Stage 1's baseline is meant to be strong enough to embarrass a learned model.
It scores context candidates from what Sensei already knows: direct anchors,
symbol distance, prior-finding match, same-family objectives, historical
co-change, invariant relations, and review recurrence. Its weights are fixed in
code and recorded with every benchmark run.

## 5. Evaluation

- **Preregistered targets.** Each benchmark objective names its transfer
  targets (the files, laws, prior findings and tests that actually mattered)
  before any retrieval runs, and those targets are frozen with a digest.
  Retrieval volume is not transfer: surfacing related material without the
  target is a miss.
- **Temporal holdout, never random splits.** Train on objectives before T1,
  validate on [T1, T2), test on [T2, T3). The graph itself improves over time,
  so a random split leaks later repairs into earlier answers.
- **Metrics, reported separately:** Recall@1/@5/@10; file, law/invariant,
  prior-finding, and test/evidence recall; context tokens emitted; irrelevant
  nodes emitted; and the headline metric

  ```
  TargetCoveragePerToken = preregistered targets recovered / tokens delivered
  ```

  The goal is to deliver the governing facts in *less* context. Recall@10 = 1.0
  at 40,000 tokens has achieved almost nothing.

## 6. Ship rule

A learned component ships only if, on the temporal test split, it recovers
preregistered targets that the deterministic baseline misses **while emitting
equal or fewer tokens**. "It retrieves relevant files" is not a result.

The model family (HGT, R-GCN, edge scorer, learning-to-rank, logistic
regression, or nothing) is chosen after Stage 0 reports how many clean
trajectories actually exist, not before.

## 7. Stages

| stage | output                                   | gate to start                   |
|-------|------------------------------------------|---------------------------------|
| 0     | point-in-time corpus + census            | this contract                   |
| 1     | deterministic context compiler + bench   | Stage 0 census                  |
| 2     | learned ranker                           | Stage 1 baseline measured       |
| 3     | learned critic (predicts review findings)| enough `finding_resolved` labels|
| 4     | learned plan skeleton                    | Stage 3 beats its baseline      |

## 8. Stage 0 record

```text
TrainingRecord
  record_id, objective_id, available_at
  base_commit
  graph_snapshot { source, commit, build_version, digest | UNRECONSTRUCTABLE }
  objective, architecture_request
  plan_attempts[] { attempt_id, plan, candidate_commit, candidate_diff, available_at }
  reviews[]       { request_id, verdict, findings[] { text, class, label_state }, available_at }
  rulings[]       { question, answer, authority, available_at }
  outcome         { ACCEPT | FAILED | BLOCKED_EXTERNAL | NONCONVERGENT | OPEN }
  provenance[]    { source, locator, digest }
```

Every value traces to a `provenance` entry: a source, a locator inside that
source, and a digest of the bytes read. A value with no provenance is not
emitted.

Stage 0 also emits a **census**: counts of records, reconstructable snapshots,
REVISE→ACCEPT transitions, findings at each label state, and exclusions with
their reasons. A census lists its subjects. A bare count does not count as one.

## 9. Sources

Inventory taken 2026-10-03 against one workstation. All sources are read-only
to this repository.

| source | what it gives | notes |
|--------|---------------|-------|
| `<repo>/.sensei-code/sessions/<id>/events.jsonl` | the episode: objective (`task.created` summary), plans (`plan.attempt.started`, `plan.proposed`), candidates, diff audits, review verdicts and findings, authority decisions, terminal, `run.receipt` | **primary**; append-only JSONL; one task per session; resumes append to the same file |
| `run.receipt` payload | `base_commit`, `graph_digest`, `candidate_commit`, `outcome`, each with a known/unknown state | best per-run summary; schema versions v10–v12 seen |
| git (`sensei-code/task-*` branches) | candidate commits and diffs | sampled receipt commits all resolve |
| `~/.sensei/graph/<store>/generations/<marker>/` | the graph generation a run was served, by marker digest | metadata and per-file digests only, **no triples** |
| `git show <commit>:docs/awareness/**` | the authored graph source at any commit | rebuilding triples needs the builder at the run's `graph_build_commit` |
| GitHub `globulario/sensei-code#157` | numbered rulings | not on disk; out of scope for Stage 0 v1 |

Not sources: `.sensei-code/tasks/*.json` and `candidates/*.json` are snapshots
overwritten in place, so they are current state, not history. The live
Oxigraph store holds only the present graph.

### What the inventory changes

- **Point-in-time graph = the graph that was served, not main's seed.**
  `run.receipt.graph_digest` comes from the live store's digest at run time.
  That store was often stale relative to main, and that is exactly the right
  snapshot: it is what Sensei actually knew. Stage 0 records the snapshot as a
  reference (`marker digest` + `graph_build_commit`), state `REFERENCED`.
  Rebuilding triples from a reference is Stage 1 work, and a reference that
  cannot be rebuilt becomes `UNRECONSTRUCTABLE`.
- **No finding lifecycle exists.** Findings carry no upheld, withdrawn or
  resolved state, and the contradiction/reconciliation events have zero
  occurrences. Under §3 every finding therefore extracts as
  `reviewer_observation`. "A later review on a new candidate accepted" is
  recorded as a separate observable fact (`followed_by_accept`), not as a
  promotion to `finding_resolved`. Stage 3 stays gated until Sensei records a
  lifecycle, which belongs on sensei-code's queue and is not something to infer
  here.
- **Synthetic runs are mixed in with real ones.** Commissioning runs use stub
  agents (`stub-architect`) and synthetic objectives, and their reviews can
  complete within the same millisecond they start. A first count found 9 of 31
  REVISE→ACCEPT sessions were stub runs. Every record carries a `synthetic`
  classification with the predicate that produced it, and benchmarks exclude
  synthetic records by default.
- **Sessions inside removed worktrees are lost.** Session directories without
  an `events.jsonl` (84 of 411 on first count) are reported in the census as
  exclusions, never silently skipped.
- **The corpus is never committed.** Objective text quotes rulings and
  internal detail. Extracted output goes to a gitignored directory.
