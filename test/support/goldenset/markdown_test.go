package goldenset

import (
	"strings"
	"testing"
)

// TestExtractJSONFence covers the happy path (exactly one ```json``` fence)
// and every loud-error shape the doc comment promises: zero fences, more
// than one, and an unterminated one. None of these may be silently skipped
// or silently resolved by picking the first candidate.
func TestExtractJSONFence(t *testing.T) {
	tests := []struct {
		name    string
		md      string
		want    string
		wantErr string // substring expected in the error, "" means no error
	}{
		{
			name: "single fence returns its body",
			md: "# doc\n\n" +
				"```json\n" +
				"{\"id\": \"x\"}\n" +
				"```\n",
			want: `{"id": "x"}`,
		},
		{
			name: "non-json fence is ignored, only json fence counts",
			md: "```go\n" +
				"var x = 1\n" +
				"```\n\n" +
				"```json\n" +
				"{\"id\": \"y\"}\n" +
				"```\n",
			want: `{"id": "y"}`,
		},
		{
			name:    "zero fences is a loud error",
			md:      "# doc\n\nno fenced blocks here at all.\n",
			wantErr: "found 0 fenced ```json blocks",
		},
		{
			name: "two fences is a loud error, never a silent pick of the first",
			md: "```json\n" +
				"{\"id\": \"first\"}\n" +
				"```\n\n" +
				"```json\n" +
				"{\"id\": \"second\"}\n" +
				"```\n",
			wantErr: "found 2 fenced ```json blocks",
		},
		{
			name: "unterminated fence is a loud error, never a silent truncation",
			md: "```json\n" +
				"{\"id\": \"unterminated\"}\n",
			wantErr: "unterminated ```json fence",
		},
		{
			name: "a fence entirely inside an HTML comment is invisible, surfacing as the zero-fence error",
			md: "# doc\n\n" +
				"<!--\n" +
				"```json\n" +
				"{\"id\": \"commented-out\"}\n" +
				"```\n" +
				"-->\n",
			wantErr: "found 0 fenced ```json blocks",
		},
		{
			name: "a commented-out fence does not count toward the ambiguous-fence-count error when a live fence exists",
			md: "<!--\n" +
				"```json\n" +
				"{\"id\": \"commented-out\"}\n" +
				"```\n" +
				"-->\n\n" +
				"```json\n" +
				"{\"id\": \"live\"}\n" +
				"```\n",
			want: `{"id": "live"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractJSONFence([]byte(tt.md))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ExtractJSONFence(...) = %q, nil, want an error containing %q", got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ExtractJSONFence(...) error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtractJSONFence(...) = _, %v, want nil error", err)
			}
			if string(got) != tt.want {
				t.Errorf("ExtractJSONFence(...) = %q, want %q", got, tt.want)
			}
		})
	}
}
