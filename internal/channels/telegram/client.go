// Package telegram is the Bot API adapter behind ports.Channel — ADR-0006's
// first-class channel for v1, over ADR-0014's transport.
//
// Long polling only. Nooma reaches Telegram; Telegram never reaches Nooma.
// The package opens outbound connections and nothing else: no inbound
// port, no DNS name, no certificate, no public address. That is ADR-0014's
// decision and this package does not reopen it — there is no webhook
// handler here to be enabled later.
package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// defaultBaseURL is Telegram's own Bot API host. It is the single
// permitted occurrence of that literal outside documentation —
// test/conformance/telegram_host_literal_test.go asserts exactly that, so
// a second code path building its own URL fails the build rather than
// quietly escaping every test's httptest redirect.
const defaultBaseURL = "https://api.telegram.org"

// pollTimeoutSeconds is what getUpdates asks Telegram to hold a quiet
// connection open for.
//
// Thirty seconds, and the number is bounded from both sides. Below it, a
// quiet brain reconnects constantly for nothing. Above it, shutdown waits
// longer than serve's grace for a connection that is doing nothing — and
// while ctx cancellation interrupts the call (see Client.getUpdates), a
// timeout longer than the grace would make correctness depend on that
// interruption always working rather than merely usually.
const pollTimeoutSeconds = 30

// Client speaks the Telegram Bot API. It holds the bot token and is the
// only thing in this package that does.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient returns a Client for token, talking to baseURL. Passing "" for
// baseURL uses Telegram's own host; every test passes an httptest server's
// URL instead, which is why the parameter exists at all.
//
// ollama.NewClient established this shape (client.go:30-36) and this
// follows it rather than inventing a second convention for the same need.
func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if httpClient == nil {
		// A poll holds a connection for pollTimeoutSeconds by design, so
		// the client's own timeout must exceed it or every quiet poll
		// would fail. The margin covers connection setup and Telegram's
		// own slack around its timeout.
		httpClient = &http.Client{Timeout: (pollTimeoutSeconds + 15) * time.Second}
	}
	return &Client{baseURL: baseURL, token: token, http: httpClient}
}

// update is one Telegram update, narrowed to what this adapter reads. The
// API sends considerably more; decoding only these fields means a new
// field upstream cannot change how a message is understood here.
type update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64  `json:"message_id"`
		Text      string `json:"text"`
		Chat      struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

// apiEnvelope is the shape every Bot API response carries.
type apiEnvelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
}

// getUpdates asks for every update after offset, holding the connection up
// to pollTimeoutSeconds. An offset of 0 means "whatever is pending".
//
// The request carries ctx, so cancelling it interrupts an in-flight poll
// rather than waiting out the timeout — which is what makes shutdown
// prompt (spec R4.3).
func (c *Client) getUpdates(ctx context.Context, offset int64) ([]update, error) {
	q := url.Values{}
	q.Set("timeout", strconv.Itoa(pollTimeoutSeconds))
	if offset > 0 {
		q.Set("offset", strconv.FormatInt(offset, 10))
	}

	raw, err := c.call(ctx, "getUpdates", q)
	if err != nil {
		return nil, err
	}

	var updates []update
	if err := json.Unmarshal(raw, &updates); err != nil {
		return nil, sanitize(c.token, fmt.Errorf("telegram: decoding updates: %w", err))
	}
	return updates, nil
}

// sendMessage posts text into chat.
func (c *Client) sendMessage(ctx context.Context, chatID int64, text string) error {
	q := url.Values{}
	q.Set("chat_id", strconv.FormatInt(chatID, 10))
	q.Set("text", text)

	_, err := c.call(ctx, "sendMessage", q)
	return err
}

// call performs one Bot API method and returns its result payload.
//
// Every error path leaves through sanitize, and that is not defensive
// habit: the token is in the URL's PATH, and net/http puts the URL into
// *url.Error's own message. The obvious wrapper therefore writes the bot
// token into every transport error. See sanitize.
func (c *Client) call(ctx context.Context, method string, q url.Values) (json.RawMessage, error) {
	endpoint := c.baseURL + "/bot" + c.token + "/" + method + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, sanitize(c.token, fmt.Errorf("telegram: %s: building the request: %w", method, err))
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, sanitize(c.token, fmt.Errorf("telegram: %s: %w", method, err))
	}
	defer func() { _ = resp.Body.Close() }()

	var env apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, sanitize(c.token, fmt.Errorf("telegram: %s: decoding the response: %w", method, err))
	}

	if !env.OK {
		// The status code is the fallback: Telegram sends error_code in
		// the envelope, but a proxy or a gateway in front of it may answer
		// with a bare status and no envelope at all, and a 401 arriving
		// that way must still be recognised as permanent (§3.7).
		code := env.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return nil, &APIError{Code: code, Description: env.Description, Method: method}
	}
	return env.Result, nil
}
