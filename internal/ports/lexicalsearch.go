package ports

import "context"

// LexicalSearch is recall's lexical leg — design D5, implemented over FTS5 by
// internal/store/sqlite (PR 9c).
//
// It takes **tokens, not text**, and that split is the design rather than a
// convenience. recall.Tokenize decides what words the lexical leg searches
// for, which is a recall-quality decision the golden corpus pins and which is
// pure. Rendering those tokens as FTS5 MATCH syntax belongs to the adapter,
// because docs/06-harness.md §1 says the cognitive core does not know SQLite
// exists, and emitting FTS5 query syntax is knowing it exists.
//
// The split also removes a whole runtime failure class. Raw user text handed
// to MATCH is an FTS5 *query expression*: `what about "ana"?`, or a message
// ending in AND, is a syntax error rather than a zero-result search. Tokens
// the adapter quotes and joins itself cannot be a syntax error.
//
// Returning ids rather than units is deliberate too. Both recall legs return
// ids and brain resolves them through UnitRepo.LiveByIDs, which filters
// positively on status = 'pool' — so I02 is enforced in exactly one place
// instead of once per leg (design D5).
type LexicalSearch interface {
	// SearchLexical returns the ids of up to k units matching any of tokens,
	// best match first. Fewer than k results, or none, is not an error: a
	// query matching nothing is an ordinary outcome of recall, not a
	// failure.
	SearchLexical(ctx context.Context, tokens []string, k int) ([]string, error)
}
