package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/reading"
	"github.com/rengo/nooma/testdata/llm"
)

// recordTimeout bounds one recording call. It is longer than the quality
// gate's: a recording is a deliberate session, and a slow answer is still a
// good one.
const recordTimeout = 60 * time.Second

// runRecord is `nooma doctor --record <dir> [vault]`: it asks the vault's
// capture_processing provider to read every phrasing awaiting its recording
// (design D1c), once, in one session.
//
// **It refuses under CI.** A recording is a human decision to spend real calls
// against a real model; a pipeline must replay, never record.
func runRecord(dir string, force bool, vaultArg string, out io.Writer) error {
	if v, set := os.LookupEnv("CI"); set && v != "" {
		return errors.New("--record makes real provider calls and refuses to run under CI: record on a maintainer's machine, then commit the files")
	}
	vault, err := config.ResolveVault(vaultArg)
	if err != nil {
		return err
	}
	cfg, err := loadVaultConfig(vault)
	if err != nil {
		return err
	}
	provider, ok := qualityGateProviders(cfg, os.LookupEnv)["capture_processing"]
	if !ok {
		return errors.New("no provider is bound to capture_processing in this vault: configure one first (`nooma init`)")
	}
	binding := cfg.Providers[cfg.Tasks["capture_processing"].Provider]
	opts := recordOptions{
		dir: dir, force: force, provider: provider,
		providerName: binding.Type, model: binding.Model, now: time.Now(),
	}
	// The judge set (testdata/llm/judge) is recorded by the provider that
	// judges relations in production, which need not be the classifier.
	if judge, bound := qualityGateProviders(cfg, os.LookupEnv)[reading.TaskJudge]; bound {
		jb := cfg.Providers[cfg.Tasks[reading.TaskJudge].Provider]
		opts.judge = &recordProvider{provider: judge, name: jb.Type, model: jb.Model}
	}
	return recordPhrasings(context.Background(), opts, out)
}

// recordProvider is a provider a recording is made with, and the names the
// recording is written under.
type recordProvider struct {
	provider ports.LLMProvider
	name     string // written as "provider": the type, as the other recordings
	model    string // written as "model" when the provider does not report one
}

type recordOptions struct {
	dir          string
	force        bool
	provider     ports.LLMProvider
	providerName string // written as "provider": the type, as the other recordings
	model        string // written as "model" when the provider does not report one
	now          time.Time
	// judge records the relation_evaluation cases; nil when the vault binds
	// nothing to that task, and then a judge case is refused unsent.
	judge *recordProvider
}

// recordPhrasings records every `"record": true` file of opts.dir, one at a
// time, writing each file as soon as it is answered so a failure keeps what
// was done. The prompt is the production one with beliefs nil
// (reading.PromptAt), at the case's own `now` or else opts.now, which it
// stores beside the prompt's hash.
//
// A file is edited by key: only provider, model, response, now and
// prompt_sha256 are set and record is dropped, so the expected reading, which
// a person wrote, is never rewritten by the thing it judges.
func recordPhrasings(ctx context.Context, opts recordOptions, out io.Writer) error {
	entries, err := os.ReadDir(opts.dir)
	if err != nil {
		return err
	}
	recorded, skipped := 0, 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(opts.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fields, err := splitFields(data)
		if err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if string(fields["record"]) != "true" {
			continue
		}
		if _, answered := fields["response"]; answered && !opts.force {
			skipped++
			continue
		}
		var c llm.Case
		if err := json.Unmarshal(data, &c); err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}
		req, rec, err := recordRequest(c, opts)
		if err != nil {
			return fmt.Errorf("%s: %w (nothing was sent for it)", entry.Name(), err)
		}

		callCtx, cancel := context.WithTimeout(ctx, recordTimeout)
		resp, err := rec.via.provider.Complete(callCtx, req)
		cancel()
		if err != nil {
			return fmt.Errorf("%s: %w (recorded %d before it; run again to continue)", c.ID, err, recorded)
		}
		model := resp.Model
		if model == "" {
			model = rec.via.model
		}
		written := map[string]string{
			"provider": rec.via.name, "model": model, "response": resp.Text,
			"prompt_sha256": reading.Hash(req.Prompt),
		}
		if rec.now != "" {
			written["now"] = rec.now
		}
		for key, value := range written {
			if fields[key], err = marshalString(value); err != nil {
				return err
			}
		}
		delete(fields, "record")
		body, err := joinFields(fields)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return err
		}
		recorded++
	}
	_, err = fmt.Fprintf(out, "recorded %d, skipped %d already recorded (use --force to record them again)\n", recorded, skipped)
	return err
}

// recording is how one case is recorded: the provider it is sent to, and the
// instant its prompt was built at, empty for a prompt that reads no clock.
type recording struct {
	via recordProvider
	now string
}

// recordRequest builds the production request for c: a classify phrasing goes
// to capture_processing through reading.PromptAt at the case's now, and a
// judge case to relation_evaluation through brain.JudgePrompt, which reads no
// clock. A case that cannot be sent is refused before any call.
func recordRequest(c llm.Case, opts recordOptions) (ports.LLMRequest, recording, error) {
	switch c.Task {
	case "classify":
		now := opts.now
		if c.Now != "" {
			var err error
			if now, err = time.Parse(time.RFC3339, c.Now); err != nil {
				return ports.LLMRequest{}, recording{}, fmt.Errorf("now: %w", err)
			}
		}
		if err := reading.ValidateCase(c); err != nil {
			return ports.LLMRequest{}, recording{}, err
		}
		req := ports.LLMRequest{Prompt: reading.PromptAt(c, now), Task: "capture_processing", JSONOnly: true}
		via := recordProvider{provider: opts.provider, name: opts.providerName, model: opts.model}
		return req, recording{via: via, now: now.Format(time.RFC3339)}, nil
	case reading.TaskJudge:
		if err := reading.ValidateJudgeCase(c); err != nil {
			return ports.LLMRequest{}, recording{}, err
		}
		if opts.judge == nil {
			return ports.LLMRequest{}, recording{}, fmt.Errorf("no provider is bound to %s in this vault", reading.TaskJudge)
		}
		req := ports.LLMRequest{Prompt: qualityGatePrompt(reading.TaskJudge, c, opts.now), Task: reading.TaskJudge, JSONOnly: true}
		return req, recording{via: *opts.judge}, nil
	}
	return ports.LLMRequest{}, recording{}, fmt.Errorf("only classify and %s cases are recorded, not %q", reading.TaskJudge, c.Task)
}

// splitFields reads a case file as its top-level keys and their raw values,
// refusing a key llm.FileFields does not list.
func splitFields(data []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for key := range fields {
		if !slices.Contains(llm.FileFields, key) {
			return nil, fmt.Errorf("unknown field %q", key)
		}
	}
	return fields, nil
}

// joinFields writes fields back as an indented object in llm.FileFields' order.
func joinFields(fields map[string]json.RawMessage) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("{\n")
	first := true
	for _, key := range llm.FileFields {
		raw, present := fields[key]
		if !present {
			continue
		}
		if !first {
			b.WriteString(",\n")
		}
		first = false
		name, _ := marshalString(key)
		b.WriteString("  " + string(name) + ": ")
		var compact bytes.Buffer
		if err := json.Compact(&compact, raw); err != nil {
			return nil, err
		}
		if err := json.Indent(&b, compact.Bytes(), "  ", "  "); err != nil {
			return nil, err
		}
	}
	b.WriteString("\n}\n")
	return b.Bytes(), nil
}

// marshalString is a JSON string without HTML escaping: a response holds
// angle brackets and ampersands a reader should see as they are.
func marshalString(s string) (json.RawMessage, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}
