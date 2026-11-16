# Scoped document retrieval in conversational answers

The chat retrieval pipeline supplements body/vector recall with document-title
matches for short topic queries (2–40 characters after whitespace normalization,
at most six words, without question or sentence delimiters). For example,
`EMC设计`, `emc 设计`, and `EMC设计.pdf` can recall the same document.
This is an internal chat-pipeline option; standalone search APIs and agent tools
keep their existing defaults.

## Title evidence

- Title matching runs only after the existing knowledge-base access checks.
- It uses exact `(tenant, knowledge base)` scopes, intersected with selected
  document IDs and physical chunk tags. Unresolved logical tag scopes fail closed.
- Deleted/disabled knowledge, unfinished parsing, disabled/deleted chunks, and
  chunks whose index is not ready cannot enter this lane.
- The query is literal (SQL wildcards are escaped). Case and common whitespace
  are normalized; common file extensions are ignored when comparing exact titles.
- Recall is bounded to five matching documents and three text chunks per document.
  Exact titles precede partial titles; matching body text precedes opening chunks.
- At most one fifth of the existing candidate budget (minimum one, maximum ten)
  is reserved for title candidates before the body-candidate cut. Duplicate
  chunks retain stronger existing retrieval evidence.
- Title recall has a two-second timeout and degrades to ordinary body recall if
  unavailable. It never suppresses an existing body-retrieval error.
- Actual chunk content still passes through the existing reranker and threshold.
  A matching filename is not sufficient evidence to answer a question.

## Result selection

After relevance filtering, internal evidence and web results receive separate
slots inside the existing top-k budget. With both present, web gets up to 20%
(at least one slot when top-k is two or greater); top-k one prefers internal
evidence. Unused slots are backfilled from the other source.

For short topic queries without explicit document selection, choose one relevant
passage per document before repeated passages. Documents far below the best
composite score do not gain priority solely to increase variety. A relevant exact
title gets a slot first. Explicit document scopes and specific interface, model/standard-number, or filename
queries disable document diversification.
MMR fills remaining slots. Existing images explicitly retained outside top-k and
existing graph fallback behavior are preserved.

The merge fallback and final cut use the same source selection, so unavailable
reranking does not reintroduce competition for all slots between web and KB hits.
No retrieval scope flags, thresholds, expert settings, or database schema change.

## Validation

Run:

```sh
go test -p 2 ./internal/application/repository ./internal/application/service ./internal/application/service/chat_pipeline ./internal/reranking ./internal/searchutil ./internal/modelcontext
```

Regression tests cover normalized titles, exact-title priority, document/tag and
cross-tenant intersections, hidden/unindexed content, literal wildcards, source
budgets and backfill, explicit document scopes, and rejection of an irrelevant
exact-title passage by the unchanged rerank threshold.
