package config

// The schema below mirrors docs/01-architecture.md §"Configuration — nooma.yml"
// field for field. That is not a coincidence to be maintained by hand: the L2
// gate of spec R9.1 decodes the document's own example into these types and
// compares the two key sets in both directions, so a field added here without a
// documented counterpart fails CI, and so does the reverse.
//
// Nothing here interprets a provider or a task. M0 decodes and shape-checks
// them; `providers` and `tasks` acquire meaning in M1 (spec R3.1). The types
// exist now because the loader cannot reject an unknown key without knowing
// every known one.

// Config is a decoded nooma.yml.
type Config struct {
	Server    Server                 `yaml:"server"`
	Database  Database               `yaml:"database"`
	Providers map[string]Provider    `yaml:"providers"`
	Tasks     map[string]TaskBinding `yaml:"tasks"`
	Channels  Channels               `yaml:"channels"`
}

// Server is the HTTP surface. Bind, HTTPPort and UI are pointers because
// spec R3.4 lets them be absent: a pointer distinguishes "the user said nothing"
// from "the user chose the value that happens to be the default", and only the
// first of those may be overwritten by ApplyDefaults.
//
// There is deliberately no field that can hold a token. AuthTokenEnv names an
// environment variable; ADR-0007 requires it when Bind is not loopback, and
// spec R4.1 makes the absence of a literal-credential field the mechanism rather
// than a review rule.
type Server struct {
	Bind         *string `yaml:"bind"`
	HTTPPort     *int    `yaml:"http_port"`
	UI           *bool   `yaml:"ui"`
	AuthTokenEnv string  `yaml:"auth_token_env"`
}

// Database locates the SQLite file. Path is relative to the vault root unless
// absolute, and spec R5.3 forbids it from escaping the vault — validation, not
// decoding, enforces that.
type Database struct {
	Path *string `yaml:"path"`
}

// Provider is one reusable model endpoint. The fields are the union of what the
// documented provider types need — `anthropic` uses APIKeyEnv and Model,
// `ollama` uses Endpoint and Model, `whisper_cpp` uses BinaryPath and ModelPath —
// so no single entry populates all of them. The config↔doc gate compares the
// union across every entry for exactly this reason (spec R9.1).
type Provider struct {
	Type       string `yaml:"type"`
	APIKeyEnv  string `yaml:"api_key_env"`
	Model      string `yaml:"model"`
	Endpoint   string `yaml:"endpoint"`
	BinaryPath string `yaml:"binary_path"`
	ModelPath  string `yaml:"model_path"`
}

// TaskBinding points one brain task at one provider by name.
type TaskBinding struct {
	Provider string `yaml:"provider"`
}

// Channels holds the channel adapters. Telegram is the only one in v1
// (ADR-0006); WhatsApp is documented as present-but-disabled and has no schema
// until it has an adapter.
type Channels struct {
	Telegram Telegram `yaml:"telegram"`
}

// Telegram is ADR-0006's channel. AllowedChatIDs is not optional when Enabled is
// true — validation refuses that combination (spec R5.1), which is what makes
// docs/01-architecture.md's "without that list, the channel does not start" a
// structural property instead of a warning.
type Telegram struct {
	Enabled        bool    `yaml:"enabled"`
	BotTokenEnv    string  `yaml:"bot_token_env"`
	AllowedChatIDs []int64 `yaml:"allowed_chat_ids"`
}

// There is deliberately no Schedules type. It existed here from M0 until
// 2026-08-31, holding two cron expressions that nothing in this repository ever
// read — no code parses a cron expression at all. ADR-0025 retires the keys
// rather than building the parser: the schedule is
// `internal/scheduler.ConsolidationHour` and `ProactiveCheckInterval`, both
// carried as calibration rows in docs/02-cognitive-core.md §13.
//
// A field here with no reader is the same defect as a port method with no
// caller, and this repository refuses to ship those. See decode.go's
// retiredKeys for what a vault that still carries the block is told.

// The defaults themselves, the validator's list of documented provider types and
// task names, and the resolution of `database.path` all arrive in the next two
// slices. This slice deliberately stops at the schema and the decoder: the keys
// of spec R3.4 are pointers here so that absence survives decoding, and nothing
// yet decides what an absent key should become.
