package telegram

import (
	"fmt"
	"net/http"
	"strings"
)

// redaction is what a token becomes in any string that would have carried
// it.
const redaction = "[REDACTED]"

// APIError is Telegram's own refusal: the request reached the API and the
// API said no. Distinct from a transport failure on purpose — a caller
// deciding whether to retry needs to tell "the connection broke" from
// "Telegram will not do this", and string-matching an error message is not
// a way to tell them apart.
type APIError struct {
	// Code is Telegram's error_code, or the HTTP status when the response
	// carried no envelope.
	Code int
	// Description is Telegram's own words. It is shown to an operator and
	// never parsed.
	Description string
	// Method is which Bot API method was refused.
	Method string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram: %s refused with %d: %s", e.Method, e.Code, e.Description)
}

// Unauthorized reports whether this refusal is the permanent one.
//
// It exists as a predicate rather than as a sentinel reached by errors.Is
// because the distinguishing fact is a field's value, not the error's
// identity: every 401 is the same failure whatever description Telegram
// attaches. A sentinel would need the constructor to decide the identity
// at creation, moving the decision away from the code that acts on it.
//
// A wrong or revoked bot token never becomes right by waiting, so a caller
// must not fold this into the transient path — a channel that retried it
// forever would look alive while being permanently deaf.
func (e *APIError) Unauthorized() bool { return e.Code == http.StatusUnauthorized }

// sanitize replaces token with redaction anywhere in err's message.
//
// **This is the whole answer to a leak the obvious code has.** Telegram
// puts the bot token in the URL PATH — /bot<TOKEN>/getUpdates — and
// net/http returns *url.Error, whose Error() renders the full URL. So:
//
//	return fmt.Errorf("telegram: getUpdates: %w", err)   // leaks the token
//
// writes the token into every transport error, and from there into the
// operator's log. It is the default behaviour of the code anyone would
// write, which is why spec R3.3 makes it a MUST and
// token_leak_test.go asserts it with a sentinel rather than trusting care.
//
// It is a denylist, and that is stated rather than hidden: it redacts the
// token it was given. An error path that formatted the URL some other way
// would still need to come through here. Every path in client.go does, and
// that is the property the test protects.
//
// The wrapper preserves nothing of the original error's identity on
// purpose — errors.Is/As through a sanitized error would hand a caller the
// *url.Error whose message is exactly what was just redacted.
func sanitize(token string, err error) error {
	if err == nil || token == "" {
		return err
	}
	msg := err.Error()
	if !strings.Contains(msg, token) {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(msg, token, redaction))
}
