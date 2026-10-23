package ui

import (
	"testing"

	"github.com/rengo/nooma/internal/core/unit"
)

func TestGraphLabel_PrefersTheTitleAndClipsByRunes(t *testing.T) {
	cases := []struct {
		u           unit.Unit
		full, short string
	}{
		{unit.Unit{Title: "Hotel", Content: "Find a hotel"}, "Hotel", "Hotel"},
		{unit.Unit{Content: "  Pack  "}, "Pack", "Pack"},
		{unit.Unit{Content: "Book flights for March"}, "Book flights for March", "Book flights…"},
		{unit.Unit{Content: "Ñandú añejo de Córdoba"}, "Ñandú añejo de Córdoba", "Ñandú añejo d…"},
		{unit.Unit{Content: "Exactly fourteen"[:14]}, "Exactly fourte", "Exactly fourte"},
	}
	for _, c := range cases {
		if got := graphLabel(c.u); got != c.full {
			t.Errorf("graphLabel(%+v) = %q, want %q", c.u, got, c.full)
		}
		if got := graphShortLabel(c.u); got != c.short {
			t.Errorf("graphShortLabel(%+v) = %q, want %q", c.u, got, c.short)
		}
	}
}
