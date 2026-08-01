package memrepo

import (
	"context"
	"sync"
	"testing"

	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/ports"
)

// Assert at compile time — see memrepo.Embeddings for why.
var _ ports.LexicalSearch = (*Lexical)(nil)

// Lexical is an in-memory ports.LexicalSearch. The zero value is not usable —
// call NewLexical.
//
// It ranks by how many distinct query tokens a document matches, ties broken
// by insertion order. That is NOT bm25, and it does not try to be: FTS5's
// ranking is the store's, and a fake reimplementing bm25 would be a second
// scoring function to keep in sync with the first. What the fake exists to
// pin is the port's contract — which ids come back, that k bounds them, that
// no match is not an error — and ranking quality is the recall corpus's job
// at L3 (PR 9c).
type Lexical struct {
	mu sync.Mutex
	// order preserves insertion order so ties are deterministic. Map
	// iteration would reorder equal-scoring results run to run, which reads
	// as a ranking bug rather than as the coin-flip it is.
	order  []string
	tokens map[string]map[string]bool
}

// NewLexical returns an empty, ready-to-use in-memory ports.LexicalSearch.
// Every call returns an independent instance.
func NewLexical() *Lexical {
	return &Lexical{tokens: make(map[string]map[string]bool)}
}

// SeedLexical makes id findable under content, tokenized by recall.Tokenize —
// the same function the production caller uses to build a query, so the fake
// cannot be findable under words the real leg would never search for.
//
// It takes *testing.T because it exists only for tests: production never
// seeds a lexical index by hand, since SQLite's units_fts is maintained by a
// trigger. The parameter is what keeps this method out of production code
// paths by construction.
func (l *Lexical) SeedLexical(_ *testing.T, id, content string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, exists := l.tokens[id]; !exists {
		l.order = append(l.order, id)
	}
	set := make(map[string]bool)
	for _, tok := range recall.Tokenize(content) {
		set[tok] = true
	}
	l.tokens[id] = set
}

// SearchLexical implements ports.LexicalSearch.
func (l *Lexical) SearchLexical(_ context.Context, tokens []string, k int) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// No tokens means the caller tokenized to nothing. It must not mean
	// "match everything" — that would hand fusion the whole vault.
	if len(tokens) == 0 {
		return nil, nil
	}

	type hit struct {
		id      string
		matched int
	}
	var hits []hit
	for _, id := range l.order {
		matched := 0
		for _, tok := range tokens {
			if l.tokens[id][tok] {
				matched++
			}
		}
		// Tokens are or-ed: one match makes a unit a candidate. Each unit
		// appears once however many tokens it matched.
		if matched > 0 {
			hits = append(hits, hit{id: id, matched: matched})
		}
	}

	// Stable by construction: hits is already in insertion order, and this
	// only promotes better matches above worse ones without disturbing
	// equals.
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].matched > hits[j-1].matched; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}

	if k > 0 && len(hits) > k {
		hits = hits[:k]
	}
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.id)
	}
	return ids, nil
}
