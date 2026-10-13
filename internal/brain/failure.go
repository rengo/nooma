package brain

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/rengo/nooma/internal/ports"
)

// ErrModelOutput marks a capture that failed because the model's answer
// could not be turned into a unit. It wraps the decoder's own error, and it
// is raised before anything is written, so "nothing was saved" is true of it.
var ErrModelOutput = errors.New("the model's answer could not be understood")

// Failure is a capture error in the words, code and HTTP status every
// surface shares. Code is the stable machine-readable name (documented in
// docs/07-functional.md); Message is for a person and never carries a
// credential, a response body or the captured text.
type Failure struct {
	Code     string
	Message  string
	Status   int
	Provider string
}

// The statuses are the numbers, not net/http's names, for the reason ports
// uses them: this package does not speak HTTP.
const (
	statusBadGateway         = 502
	statusServiceUnavailable = 503
	statusGatewayTimeout     = 504
)

// providerName is how a person says an adapter's type; an unknown type is
// said as it is.
func providerName(adapter string) string {
	switch adapter {
	case "openai":
		return "OpenAI"
	case "anthropic":
		return "Anthropic"
	case "ollama":
		return "Ollama"
	}
	return adapter
}

// Describe says whether err is a provider or model-output failure and, if so,
// how to tell the user. Anything else is not described: it stays an internal
// error, logged in full and answered without detail.
func Describe(err error) (Failure, bool) {
	if errors.Is(err, ErrModelOutput) {
		return Failure{
			Code:    "model_output_unusable",
			Message: "The model's answer could not be understood; nothing was saved.",
			Status:  statusBadGateway,
		}, true
	}
	var pe *ports.ProviderError
	if !errors.As(err, &pe) {
		return Failure{}, false
	}
	name := providerName(pe.Provider)
	f := Failure{Provider: pe.Provider, Status: statusBadGateway}
	switch pe.Kind {
	case ports.FailureKeyMissing:
		f.Code, f.Status = "provider_key_missing", statusServiceUnavailable
		f.Message = fmt.Sprintf("The %s API key is missing: %s is not set. Nooma looks for it in the vault's .env file and in the environment; set it and restart nooma serve.", name, pe.EnvVar)
	case ports.FailureUnreachable:
		f.Code = "provider_unreachable"
		f.Message = fmt.Sprintf("%s could not be reached. Check the network connection and the provider's endpoint.", name)
	case ports.FailureKeyRejected:
		f.Code = "provider_key_rejected"
		f.Message = fmt.Sprintf("%s rejected the API key. Check the key in the vault's .env file.", name)
	case ports.FailureRateLimited:
		f.Code, f.Status = "provider_rate_limited", statusServiceUnavailable
		f.Message = fmt.Sprintf("%s is rate limiting requests. Try again in a moment.", name)
	case ports.FailureTimeout:
		f.Code, f.Status = "provider_timeout", statusGatewayTimeout
		f.Message = fmt.Sprintf("%s took too long to answer. Try again in a moment.", name)
	default:
		f.Code = "provider_failed"
		f.Message = fmt.Sprintf("%s failed to answer (status %d).", name, pe.Status)
	}
	return f, true
}

// LogFailure writes the one line serve owes a failed request. It takes no
// error and no text, so the line cannot hold a secret or the user's words.
func LogFailure(path string, f Failure) {
	slog.Error("request failed", "class", f.Code, "provider", f.Provider, "path", path)
}
