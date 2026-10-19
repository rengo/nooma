package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/channels/telegram"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/vaultlock"
)

func init() {
	commands["try-chat"] = command{
		summary: "replay chat messages through a vault COPY and print each reply (development)",
		run:     runTryChat,
	}
}

// runTryChat is `nooma try-chat <vault> <message>...`: a developer's way to
// see what the chat would answer, against the vault's real provider, before
// asking anyone to type the same thing into Telegram.
//
// It assembles what serve assembles for an inbound message — wireBrain, one
// in-memory conversation, channels.CaptureHandler and its reply rendering —
// and hands that handler each message as if from the vault's allowed chat.
// What differs is the channel: an in-memory one that records the reply, so
// nothing reaches a transport, and no poller, scheduler or HTTP surface runs.
//
// It writes to the vault it is given, which is why it takes the vault lock
// (a running serve refuses it) and why the vault path is required rather than
// resolved to the default: point it at a copy.
func runTryChat(args []string, out, errOut io.Writer) error {
	if len(args) < 2 || strings.HasPrefix(args[0], "-") {
		return errors.New("usage: nooma try-chat <vault-copy> <message>... — the vault is required, and it is written to")
	}
	vault, messages := args[0], args[1:]

	cfg, err := loadVaultConfig(vault)
	if err != nil {
		return err
	}

	lock, err := vaultlock.Acquire(vault)
	if err != nil {
		var inUse *vaultlock.InUseError
		if errors.As(err, &inUse) {
			return err
		}
		return fmt.Errorf("taking the vault lock: %w", err)
	}
	defer func() { _ = lock.Release() }()

	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		return err
	}
	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	capture, _, err := wireBrain(ctx, db, cfg, os.LookupEnv)
	if err != nil {
		return fmt.Errorf("wiring the capture pipeline: %w", err)
	}
	if capture == nil {
		return errors.New("this vault cannot capture: bind capture_processing, relation_evaluation, chat and embedding to a provider under tasks: in nooma.yml")
	}
	conv := brain.NewConversation()
	capture.WithConversation(conv)

	triggers := sqlite.NewTriggerRepo(db)
	before, err := armedTriggers(ctx, triggers)
	if err != nil {
		return err
	}

	chat := &transcriptChannel{}
	handle := channels.CaptureHandler(capture, chat, conv, errOut)
	conversation := tryChatConversation(cfg.Channels.Telegram.AllowedChatIDs)
	for i, text := range messages {
		if _, err := fmt.Fprintf(out, "← %s\n", text); err != nil {
			return err
		}
		chat.last = ""
		if err := handle(ctx, ports.ChannelMessage{
			ID:           strconv.Itoa(i + 1),
			Conversation: conversation,
			Text:         text,
			Channel:      telegram.Name,
		}); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "→ %s\n", strings.ReplaceAll(chat.last, "\n", "\n  ")); err != nil {
			return err
		}
	}

	after, err := armedTriggers(ctx, triggers)
	if err != nil {
		return err
	}
	return writeTriggerChanges(out, before, after)
}

// tryChatConversation is the conversation a message arrives in: the vault's
// first allowed chat, as the Telegram adapter would name it, so a vault copy
// sees the same conversation id its live chat has.
func tryChatConversation(allowed []int64) ports.ConversationID {
	if len(allowed) == 0 {
		return "try-chat"
	}
	return ports.ConversationID(strconv.FormatInt(allowed[0], 10))
}

// transcriptChannel is the in-memory ports.Channel the handler replies
// through. It only records: it receives nothing and sends nowhere.
type transcriptChannel struct{ last string }

func (*transcriptChannel) Name() string { return telegram.Name }
func (*transcriptChannel) Receive(context.Context) ([]ports.ChannelMessage, error) {
	return nil, nil
}
func (*transcriptChannel) Confirm(context.Context, string) error { return nil }
func (c *transcriptChannel) Send(_ context.Context, _ ports.ConversationID, text string) error {
	c.last = text
	return nil
}
func (*transcriptChannel) Close() error { return nil }

// armedTriggers reads every armed trigger with a fire time, by id. The due
// read at the end of time is every armed one: TriggerRepo has no list read
// (its own doc comment says why), and this needs no new port method.
func armedTriggers(ctx context.Context, repo *sqlite.TriggerRepo) (map[string]ports.DueTrigger, error) {
	due, err := repo.Due(ctx, time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return nil, fmt.Errorf("reading the armed triggers: %w", err)
	}
	byID := make(map[string]ports.DueTrigger, len(due))
	for _, d := range due {
		byID[d.ID] = d
	}
	return byID, nil
}

// writeTriggerChanges lists the triggers this run armed or moved, in fire
// order and the process's local zone (the zone serve's clock reads, so set TZ
// to see another), and the ones it disarmed.
func writeTriggerChanges(out io.Writer, before, after map[string]ports.DueTrigger) error {
	var armed []ports.DueTrigger
	for id, t := range after {
		if old, existed := before[id]; !existed || !old.FireAt.Equal(t.FireAt) || old.Payload.ActionText != t.Payload.ActionText {
			armed = append(armed, t)
		}
	}
	sort.Slice(armed, func(i, j int) bool {
		if !armed[i].FireAt.Equal(armed[j].FireAt) {
			return armed[i].FireAt.Before(armed[j].FireAt)
		}
		return armed[i].ID < armed[j].ID
	})
	var gone []ports.DueTrigger
	for id, t := range before {
		if _, still := after[id]; !still {
			gone = append(gone, t)
		}
	}
	sort.Slice(gone, func(i, j int) bool { return gone[i].ID < gone[j].ID })

	var b strings.Builder
	b.WriteString("\narmed or moved by this run:\n")
	if len(armed) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, t := range armed {
		fmt.Fprintf(&b, "  %s  %s\n", t.FireAt.In(time.Local).Format("Mon 2006-01-02 15:04 MST"), t.Payload.ActionText)
	}
	if len(gone) > 0 {
		b.WriteString("no longer armed:\n")
		for _, t := range gone {
			fmt.Fprintf(&b, "  %s  %s\n", t.FireAt.In(time.Local).Format("Mon 2006-01-02 15:04 MST"), t.Payload.ActionText)
		}
	}
	_, err := io.WriteString(out, b.String())
	return err
}
