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
	return recordPhrasings(context.Background(), recordOptions{
		dir: dir, force: force, provider: provider,
		providerName: binding.Type, model: binding.Model, now: time.Now(),
	}, out)
}

type recordOptions struct {
	dir          string
	force        bool
	provider     ports.LLMProvider
	providerName string // written as "provider": the type, as the other recordings
	model        string // written as "model" when the provider does not report one
	now          time.Time
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
		if c.Task != "classify" {
			return fmt.Errorf("%s: only classify phrasings are recorded, not %q", entry.Name(), c.Task)
		}
		now := opts.now
		if c.Now != "" {
			if now, err = time.Parse(time.RFC3339, c.Now); err != nil {
				return fmt.Errorf("%s: now: %w", entry.Name(), err)
			}
		}
		prompt := reading.PromptAt(c, now)

		callCtx, cancel := context.WithTimeout(ctx, recordTimeout)
		resp, err := opts.provider.Complete(callCtx, ports.LLMRequest{Prompt: prompt, Task: "capture_processing", JSONOnly: true})
		cancel()
		if err != nil {
			return fmt.Errorf("%s: %w (recorded %d before it; run again to continue)", c.ID, err, recorded)
		}
		model := resp.Model
		if model == "" {
			model = opts.model
		}
		for key, value := range map[string]string{
			"provider": opts.providerName, "model": model, "response": resp.Text,
			"now": now.Format(time.RFC3339), "prompt_sha256": reading.Hash(prompt),
		} {
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
