package recall

import (
	"reflect"
	"testing"
)

// TestTokenize pins what "a word" means for the lexical leg (design D5 —
// no spec MUST constrains the exact algorithm beyond "what words the
// lexical leg searches for"; this table is what pins the chosen behavior).
func TestTokenize(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "lowercases and splits on punctuation",
			text: "Buy milk, walk the dog!",
			want: []string{"buy", "milk", "walk", "the", "dog"},
		},
		{
			name: "keeps digits inside a token",
			text: "meet Ana at 3pm",
			want: []string{"meet", "ana", "at", "3pm"},
		},
		{
			name: "collapses repeated separators",
			text: "coffee...   with   Ana",
			want: []string{"coffee", "with", "ana"},
		},
		{
			name: "whitespace-only text yields no tokens",
			text: "   ",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Tokenize(tt.text)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Tokenize(%q) = %#v, want %#v", tt.text, got, tt.want)
			}
		})
	}
}
