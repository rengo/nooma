package ports

import "context"

// LLMProvider and EmbeddingProvider are the two provider ports M1
// Phase A defines — design D7. Neither interface names a specific vendor:
// internal/providers (Phase A PR6) is where anthropic/openai/ollama
// adapters exist; test/support/fakeprovider is where every test in this
// codebase gets one instead (CLAUDE.md non-negotiable #5).

// LLMRequest is a call to LLMProvider.Complete.
type LLMRequest struct {
	// Prompt is the exact text sent to the provider.
	Prompt string
	// Task names which pipeline call this request feeds (e.g. "classify"),
	// matching testdata/llm/format.md's task field.
	Task string
	// JSONOnly says the caller will parse the response as JSON, and asks
	// the provider to guarantee it can.
	//
	// **It states the caller's intent, never a vendor's mechanism.** OpenAI
	// spells that guarantee response_format, Ollama spells it format,
	// Anthropic's Messages API cannot spell it at all — and Anthropic
	// rejects unknown body fields with a 400, so an adapter that cannot
	// honour this must send nothing rather than guess. Each adapter renders
	// the intent in its own dialect or declines; none of that reaches here.
	//
	// This does not weaken LLMResponse.Text's "bytes-as-string, never
	// parsed" rule below. Asking a provider to emit JSON is not parsing it:
	// the degradation rule stays a pure function of the returned bytes, in
	// internal/core, where it is testable at L1 and proved once rather than
	// once per provider.
	//
	// **A prompt sent with this set must contain the word "json".** OpenAI
	// refuses json_object mode without it, and its documented behaviour is
	// not a clean 400 but an unending stream of whitespace until the token
	// budget is exhausted. TestJSONOnlyPromptsSayJSON pins that coupling,
	// which otherwise lives between two files that never mention each other.
	//
	// False for a free-text task. timer_rephrase writes a sentence for a
	// human, and a sentence forced into JSON mode answers in a shape nothing
	// downstream parses.
	JSONOnly bool
}

// LLMResponse is what an LLMProvider.Complete call returns on success.
//
// Text is the provider's raw response — bytes-as-string, never parsed.
// I14's degradation rule (docs/02-cognitive-core.md) is a pure function of
// those bytes and lives in internal/core/classify (Phase B), not here: a
// provider that parsed JSON would move that rule into an adapter, where it
// is untestable at L1 and would have to be re-proved once per provider.
//
// Model is what actually answered, not what the caller asked for — I21
// filters vector search on the model that produced a vector, and keying on
// the requested name instead would be a second source of truth the moment
// a provider substitutes a model.
type LLMResponse struct {
	Text  string
	Model string
}

// LLMProvider completes a prompt, task-agnostic beyond LLMRequest.Task.
type LLMProvider interface {
	Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
}

// EmbedRequest is a call to EmbeddingProvider.Embed.
type EmbedRequest struct {
	Text string
}

// EmbedResponse is what an EmbeddingProvider.Embed call returns on
// success.
//
// Vector is []float32, matching ADR-0012's own memory arithmetic — its
// residency table is only consistent with float32 (10,000 × 768 × 4 B =
// its stated "29 MB"; float64 would silently double that figure).
//
// There is no Dim field: the dimension is len(Vector). A stored dimension
// beside a slice is a second source of truth with nothing keeping them
// equal.
//
// Model is, as in LLMResponse, what actually answered.
type EmbedResponse struct {
	Vector []float32
	Model  string
}

// EmbeddingProvider embeds text into a vector.
type EmbeddingProvider interface {
	Embed(ctx context.Context, req EmbedRequest) (EmbedResponse, error)
}
