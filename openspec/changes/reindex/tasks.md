# Tasks — reindex (M6 slice D2)

One PR, `feat/reindex`.

- [x] 1. RED: `RunEmbeddingRepo` cases for `LiveWithoutEmbedding`; L3 archived exclusion. GREEN:
      port method, SQLite and memrepo.
- [x] 2. RED: `TestI21_Reindex*` conformance (D2-D4). GREEN: `brain.ReindexService`.
- [x] 3. RED: `cmd/nooma/reindex_test.go` (D5-D8), doctor wording, dispatch and sandbox lists.
      GREEN: `nooma reindex`, doctor FAILs.
- [x] 4. Mutations: each guard removed turns a test red.
- [x] 5. Docs: doc 02 §5, doc 03, doc 06 I21 row, doc 07, doc 05 M6 table, doc 01, doc 08.
- [x] 6. `make check-all`; throwaway-vault walkthrough; PR.

Rework after review (CHANGES REQUESTED on d8c50388):

- [x] 7. RED: `TestI21_AReindexReachesEveryPageOfTheWorkList` (250 units, a refusal on the page
      boundary); the one-page mutant turns it red. Progress every 25 pinned
      (`TestReindex_SaysItsProgressEvery25Units`).
- [x] 8. RED then GREEN: units left after a clean run exit non-zero
      (`TestReindex_UnitsLeftAfterACleanRunAreAnError`).
- [x] 9. Doc 05 and doc 07 both state D2 as of merge (Shipped, #451).
