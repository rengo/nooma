# Design — reindex (M6 slice D2)

`nooma reindex [--yes] [vault]`: embed every live unit lacking the configured embedding model's
vector, beside its other models' vectors, and rebuild the word index. Governed by ADR-0003
(an explicit command with progress and confirmation, resumable), ADR-0058 point 4 (adds beside,
complete when no live unit lacks the model, resumable from the data alone) and ADR-0059 point 6
(doctor's FAILs name the real command once it ships). No new ADR: every decision below reads one
of those, the single-writer rule (doc 03) or a precedent in the binary.

## Decisions

- **D1. Layering.** `brain.ReindexService` (reads, provider calls, writes; no decision worth a
  pure function, so nothing in `internal/core`). `cmd/nooma/reindex.go` wires config, lock,
  prompt and wording. One new port method, `EmbeddingRepo.LiveWithoutEmbedding(ctx, model,
  after, limit) []string`: the ids of the live units (`status = 'pool'`, the same filter as
  `CountLiveWithoutEmbedding`) with no row under `model`, ordered by id, after a cursor. A
  cursor, not an offset, so a unit the provider refused is passed rather than met again.
- **D2. Target model = the embedding task's provider model** (`configuredEmbeddingModel`, what
  serve loads and doctor counts). The provider is built from that binding. A provider answering
  under another model stops the run: storing it under the configured name would mix two models
  (I21), storing it under its own would never close the gap and re-pay on every run.
- **D3. Resumable from the data alone.** One provider call and one `Put` per unit, at the
  instant read once from the clock. The work list is the query itself, so an interrupted run
  (Ctrl-C/SIGTERM through `signal.NotifyContext`, bound after the question so Ctrl-C there
  still ends the command; a kill; a crash) keeps every row written and the next run lists only
  what is left; it says "interrupted", not "context canceled". A complete vault lists nothing and calls no provider.
- **D4. Provider failures.** A refused unit is counted and passed; the run goes on. Three
  refusals in a row (`reindexFailureStreak`) stop the run as "the provider looks unavailable",
  so a provider that is down is not called once per unit of a large vault. An operational bound,
  not brain behavior, so it is a named constant in `brain`, not a §13 knob. Either way the
  summary names how many are left and that re-running continues; the exit status is non-zero.
- **D5. Beside serve: refused.** Reindex writes, so it takes the vault lock exactly as
  `consolidate` does (doc 03's single writer). A running serve holds it; reindex refuses before
  any provider call, naming the holder, and says: stop serve, run reindex, start it again. Serve
  reads its index once at start (ADR-0012), so a reindex beside a running serve could not
  reach recall before a restart anyway. Doctor's FAILs say the same three steps.
- **D6. Confirmation (ADR-0003).** When there is something to embed, reindex first states it
  ("N live units lack a M vector; each is one call to provider P") and asks
  `Continue? [y/N]` on stdin. Anything but `y`/`yes` (EOF included, as in a container without
  `-i`) changes nothing and says how to proceed. `--yes` answers for an unattended run. A
  complete vault, or a vault with no embedding model, asks nothing.
- **D7. The word index.** After the answer, before embedding, `Vault.RebuildSearchIndex` (FTS5
  `rebuild`, the one import uses) runs, so doctor's search-index check passes even when the
  embedding stops early. With no embedding model bound, reindex only rebuilds the word index and
  says no vectors were computed; with nothing to embed, it still rebuilds it.
- **D8. Output.** Progress every 25 units and at the end (`reindex: 50 of 120 embedded under
  M`). Summary: embedded, skipped (live units already holding the vector, read before the run
  through `sqlite.EmbeddingRepo.LiveModels`), failed, left; the last failure's reason. Units
  still left after a run with no error or refusal also exit non-zero, so a run that stops short
  (a paging regression, a writer outside the lock) is never silent.
- **D9. Doctor and usage.** Both FAILs drop "(not in this version yet)" and say "stop nooma
  serve, run `nooma reindex`, and start it again", and say one unit in the singular. Serve's boot warning keeps pointing at
  doctor (ADR-0059 point 7). `reindex` leaves `TestUnimplementedCommandsDoNotExist` and joins
  the sandbox test's vaultless commands: with no vault it stops at resolution (PR #430).

## Tests (strict TDD)

- L2 `test/conformance/i21_reindex_test.go` over memrepo: embeds the live units lacking the
  model, once each, by id, keeping the old model's rows; archived never asked; a second run
  asks nothing; interrupted after two, the rerun asks only for the rest; a refused unit is
  passed and retried alone; three refusals in a row stop; another model's answer is refused.
- Repo contract `RunEmbeddingRepo`: `LiveWithoutEmbedding` (model filter, cursor, limit) on
  memrepo and SQLite; L3 archived exclusion in the store's integration test.
- `cmd/nooma/reindex_test.go` through the real wiring, a temp vault and an `httptest` Ollama:
  embeds and rebuilds the word index, re-run says nothing to embed; declined prompt writes
  nothing; refuses while the lock is held, calling no provider; no embedding model rebuilds
  only the word index; a failing provider leaves the summary and a non-zero exit.
