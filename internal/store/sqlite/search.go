package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/rengo/nooma/internal/ports"
)

// Search is the SQLite-backed ports.LexicalSearch over units_fts
// (migration 0002) — recall's lexical leg, design D5.
type Search struct {
	db *sql.DB
}

// NewSearch returns a ports.LexicalSearch backed by v's already-migrated
// vault.
func NewSearch(v *Vault) *Search {
	return &Search{db: v.db}
}

var _ ports.LexicalSearch = (*Search)(nil)

// SearchLexical implements ports.LexicalSearch.
//
// This is where FTS5 query syntax is emitted, and nowhere else: the core
// hands over tokens and never learns SQLite exists (docs/06-harness.md §1,
// design D5). Each token is quoted and the list joined with OR — a candidate
// generator, not a filter, so a unit matching one token is a candidate.
// Narrowing to units matching every token would drop the near-misses fusion
// exists to rank.
//
// ORDER BY bm25(units_fts) ASCENDING. ADR-0010:19-22 records that bm25()
// returns NEGATIVE values with no fixed bound, so more negative is a better
// match and ascending is best-first. That claim was never executed in any
// design session — search_integration_test.go is its first execution.
//
// status = 'pool' is I02's storage half, filtered positively: a negation
// list would silently admit a status added later.
func (s *Search) SearchLexical(ctx context.Context, tokens []string, k int) ([]string, error) {
	// No tokens means the caller tokenized to nothing — a message of pure
	// punctuation, say. It must not become a whole-vault scan, and it must
	// not reach MATCH at all: an empty match expression is an FTS5 syntax
	// error, not an empty result.
	if len(tokens) == 0 {
		return nil, nil
	}

	const q = `
SELECT u.id
FROM units_fts
JOIN units u ON u.rowid = units_fts.rowid
WHERE units_fts MATCH ? AND u.status = 'pool'
ORDER BY bm25(units_fts)
LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, matchExpression(tokens), limitOrAll(k))
	if err != nil {
		return nil, fmt.Errorf("lexical search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning a lexical search result: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating lexical search results: %w", err)
	}
	return ids, nil
}

// matchExpression renders tokens as an FTS5 MATCH expression: each quoted,
// joined with OR.
//
// recall.Tokenize yields only letters and digits, so no token can carry a
// quote today. The escaping is here anyway, because that guarantee lives in
// another package and a widened tokenizer would otherwise turn a user's
// message into a syntax error — or worse, into a different query. FTS5
// escapes a double quote by doubling it.
func matchExpression(tokens []string) string {
	quoted := make([]string, 0, len(tokens))
	for _, t := range tokens {
		quoted = append(quoted, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " OR ")
}

// limitOrAll maps k onto a LIMIT value. k <= 0 means "no bound", matching
// recall.VectorQuery.K's own convention, and -1 is SQLite's way of spelling
// an unbounded LIMIT.
func limitOrAll(k int) int {
	if k <= 0 {
		return -1
	}
	return k
}
