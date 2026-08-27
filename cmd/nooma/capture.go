package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/httpapi"
	"github.com/rengo/nooma/internal/store/vaultlock"
)

// captureTimeout bounds how long `nooma capture` waits for POST /capture to
// answer. A real capture can involve an LLM classify call and an embedding
// call, both already bounded server-side (doctor.go's own
// qualityGateTimeout); this is the CLI's own ceiling so an unresponsive
// server does not hang the terminal forever.
const captureTimeout = 30 * time.Second

// runCapture is design D11: `nooma capture` is an HTTP client of a running
// `nooma serve`, never a second direct-vault writer. It resolves the vault
// only to read nooma.yml — no lock taken (spec R3.1's own MUST NOT) — builds
// the address `serve` itself would bind, and posts the capture.
//
// The order below is the whole safety of this command, the same shape
// runServe's own doc comment states for its four steps: the token is
// decided BEFORE anything is sent (captureAuthHeader), because a client
// that sends first and discovers the 401 afterwards has already put the
// user's memory on the wire unauthenticated (DecideBinding's own
// decide-first-act-second shape, binding.go:24-27).
func runCapture(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { _, _ = fmt.Fprint(errOut, "usage: nooma capture <text> [vault]\n") }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("capture requires the text to capture")
	}
	if fs.NArg() > 2 {
		return fmt.Errorf("capture takes the text and at most one vault path, got %d arguments", fs.NArg())
	}
	text := fs.Arg(0)

	vault, err := config.ResolveVault(fs.Arg(1))
	if err != nil {
		return err
	}
	cfg, err := loadVaultConfig(vault)
	if err != nil {
		return err
	}

	header, err := captureAuthHeader(cfg)
	if err != nil {
		return err
	}

	addr := dialAddress(cfg)
	resp, err := postCapture(addr, header, text)
	if err != nil {
		// A transport-level failure (connection refused, no route, timeout)
		// is exactly the case design D11's three-way diagnosis exists for:
		// "connection refused" alone does not tell a user whether they
		// forgot to start the server, started it on another port, or are
		// looking at a stale lock.
		return diagnoseUnreachable(vault, addr)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("the server rejected the request as unauthorized (401) — check the token")
	}
	if resp.StatusCode >= http.StatusBadRequest {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		if errBody.Error != "" {
			return fmt.Errorf("the server rejected the capture: %s (status %d)", errBody.Error, resp.StatusCode)
		}
		return fmt.Errorf("the server rejected the capture with status %d", resp.StatusCode)
	}

	var body captureCLIResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("decoding the server's response: %w", err)
	}
	return renderCaptureResponse(out, body)
}

// dialAddress is design D11's own translation: a wildcard bind (0.0.0.0,
// ::) is what a server LISTENS on, never what a client connects to — a
// literal `http://0.0.0.0:7777` works on some stacks and not others, which
// is the worst kind of bug. So a wildcard dials 127.0.0.1 instead of the
// literal; any other configured host is dialed as given.
func dialAddress(cfg *config.Config) string {
	host := *cfg.Server.Bind
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(*cfg.Server.HTTPPort))
}

// captureAuthHeader resolves the Authorization header this CLI sends, from
// the same place `serve` and its own middleware do —
// httpapi.ResolveToken (design D10/D11, ADR-0017's "one function, three
// readers": Deps.Token, the middleware, and the CLI all read "is a token
// configured" from here, so none of the three can disagree).
//
// When server.auth_token_env names a variable the environment does not
// hold, this refuses BEFORE returning, rather than letting the caller send
// the request and discover the 401 afterwards.
func captureAuthHeader(cfg *config.Config) (string, error) {
	token, configured := httpapi.ResolveToken(cfg, os.LookupEnv)
	if configured {
		return "Bearer " + token, nil
	}
	if cfg.Server.AuthTokenEnv != "" {
		return "", fmt.Errorf(
			"server.auth_token_env names $%s, which is not set — refusing to send before checking (design D11)",
			cfg.Server.AuthTokenEnv)
	}
	return "", nil
}

// postCapture sends text to addr's own POST /capture route. This call IS
// design D11's connectivity probe: no separate check runs before it,
// because by the time it runs the auth decision has already been made
// (captureAuthHeader, above) — sending is the last step, never the first.
func postCapture(addr, authHeader, text string) (*http.Response, error) {
	payload, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/capture", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	client := &http.Client{Timeout: captureTimeout}
	return client.Do(req)
}

// diagnoseUnreachable is design D11's three-way diagnosis. Reading
// vaultlock.ReadHolder costs nothing — it is the same free read `nooma
// status` already performs — and it is what tells a user whose `bind`
// moved that a server IS running, rather than sending them looking for one
// that is not.
func diagnoseUnreachable(vault, addr string) error {
	pid, held, lockErr := vaultlock.ReadHolder(vault)
	if lockErr == nil && held {
		return fmt.Errorf(
			"a process (pid %d) holds vault %s but nothing answered at http://%s — check server.bind and server.http_port in nooma.yml",
			pid, vault, addr)
	}
	return fmt.Errorf(
		"no nooma server is running for vault %s (expected at http://%s) — start one with 'nooma serve'",
		vault, addr)
}

// captureCLIResponse mirrors internal/httpapi's own captureResponse wire
// shape (design D10 §5.1) — the CLI decodes the identical JSON serve.go's
// Handler(Deps) encodes, just as an httptest client for that route would.
// Only the fields this command renders are declared; Candidates and the
// correction shape are not part of the human-readable summary R3.1 asks
// for, and an unrendered field is never silently dropped — the default
// branch in renderCaptureResponse names the outcome even when this struct
// carries nothing else to say about it.
type captureCLIResponse struct {
	Outcome  string `json:"outcome"`
	UnitID   string `json:"unit_id,omitempty"`
	Embedded bool   `json:"embedded,omitempty"`
	Armed    string `json:"armed,omitempty"`
	ArmedID  string `json:"armed_id,omitempty"`
	FireAt   string `json:"fire_at,omitempty"`
	Why      string `json:"why,omitempty"`
	Message  string `json:"message,omitempty"`
	Units    []struct {
		Content string `json:"content"`
	} `json:"units,omitempty"`
}

// renderCaptureResponse prints the human-readable summary spec R3.1's own
// MUST asks for. The text is the product here (16a-i's own lesson: a test
// pass that asserts a typed struct and never the rendered text lets a
// wording regression through) — every outcome renders to a distinguishable
// line, never a raw JSON dump.
func renderCaptureResponse(out io.Writer, resp captureCLIResponse) error {
	switch brain.CaptureOutcome(resp.Outcome) {
	case brain.OutcomeStored:
		state := "not embedded — no embedding provider bound"
		if resp.Embedded {
			state = "embedded"
		}
		_, err := fmt.Fprintf(out, "captured: unit %s stored (%s)\n", resp.UnitID, state)
		return err

	case brain.OutcomeArmed:
		_, err := fmt.Fprintf(out, "captured: armed %s %s — fires at %s\n", resp.Armed, resp.ArmedID, resp.FireAt)
		return err

	case brain.OutcomeArmRefused:
		_, err := fmt.Fprintf(out, "captured: not scheduled — %s\n", resp.Message)
		return err

	case brain.OutcomeConversed:
		// Not prefixed "captured:", because nothing was. The other
		// non-capturing outcome in this switch, recall, already prints
		// under its own label for the same reason.
		if resp.Message == "" {
			_, err := fmt.Fprintln(out, "chat: no answer — the chat task did not respond")
			return err
		}
		_, err := fmt.Fprintf(out, "chat: %s\n", resp.Message)
		return err

	case brain.OutcomeOutOfScope:
		_, err := fmt.Fprintln(out, "captured: nothing to do — that is not something Nooma does")
		return err

	case brain.OutcomeRecalled:
		if len(resp.Units) == 0 {
			_, err := fmt.Fprintln(out, "recall: nothing found")
			return err
		}
		if _, err := fmt.Fprintln(out, "recall:"); err != nil {
			return err
		}
		for _, u := range resp.Units {
			if _, err := fmt.Fprintf(out, "  - %s\n", u.Content); err != nil {
				return err
			}
		}
		return nil

	default:
		_, err := fmt.Fprintf(out, "captured: %s\n", resp.Outcome)
		return err
	}
}
