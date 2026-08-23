package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/ports"
)

// Name is this channel's own name, and becomes brain.CaptureInput.Channel
// and therefore units.source for every capture arriving through it.
const Name = "telegram"

// Channel is ports.Channel over the Telegram Bot API.
//
// It holds the allow-list and the read cursor. Admission happens here,
// inside Receive, and nowhere above: an allow-list is a property of one
// transport's identity space, and a second channel added later would
// otherwise need its own copy of a rule that already exists.
type Channel struct {
	client  *Client
	allowed map[int64]bool
	log     io.Writer

	mu sync.Mutex
	// logMu guards every write to log. A polling loop and a shutdown path
	// write from different goroutines, which is m2d's JD-5-01 exactly — a
	// real data race on an unguarded io.Writer, found by a race detector
	// and not by review.
	logMu sync.Mutex
	// seen is the highest update id this channel has read.
	seen int64
	// unconfirmed holds every admitted-but-unconfirmed update id, lowest
	// first. It is what keeps the cursor from advancing past work that is
	// not durable yet.
	unconfirmed []int64
}

var _ ports.Channel = (*Channel)(nil)

// New returns a Channel for cfg, reading the bot token through lookup.
//
// It refuses every configuration internal/config's validator refuses, and
// that duplication is the point: the validator is a check a caller
// performs, this is a rule the channel cannot be made to break. A caller
// that skipped validation is the case it exists for (non-negotiable #7).
//
// lookup rather than os.Getenv so a test injects a token without touching
// the process environment — the shape internal/config's own validator
// already uses.
func New(cfg config.Telegram, lookup func(string) (string, bool), httpClient *http.Client, baseURL string, log io.Writer) (*Channel, error) {
	if !cfg.Enabled {
		return nil, errors.New("telegram: channels.telegram is not enabled; the caller decides whether to construct a channel, and a channel that polls nothing would look healthy while doing nothing")
	}

	var problems []error
	if len(cfg.AllowedChatIDs) == 0 {
		problems = append(problems, errors.New("telegram: channels.telegram is enabled with no allowed_chat_ids; anyone who finds the bot could talk to this brain (ADR-0006)"))
	}
	if cfg.BotTokenEnv == "" {
		problems = append(problems, errors.New("telegram: channels.telegram is enabled without bot_token_env"))
	}

	var token string
	if cfg.BotTokenEnv != "" {
		v, set := lookup(cfg.BotTokenEnv)
		// An empty value is as unusable as an unset one, and reporting it
		// as "not set" is the message that sends a reader to the right
		// place: the variable exists and holds nothing.
		if !set || v == "" {
			problems = append(problems, fmt.Errorf("telegram: channels.telegram is enabled and bot_token_env names $%s, which is not set", cfg.BotTokenEnv))
		}
		token = v
	}

	if err := errors.Join(problems...); err != nil {
		return nil, err
	}

	allowed := make(map[int64]bool, len(cfg.AllowedChatIDs))
	for _, id := range cfg.AllowedChatIDs {
		allowed[id] = true
	}
	if log == nil {
		log = io.Discard
	}

	return &Channel{client: NewClient(baseURL, token, httpClient), allowed: allowed, log: log}, nil
}

// Name implements ports.Channel.
func (c *Channel) Name() string { return Name }

// Receive implements ports.Channel. It polls once and returns every
// admitted message in the batch.
//
// **Admission happens before a ports.ChannelMessage exists.** A message
// from outside the allow-list never becomes one, so nothing above this
// line can accidentally act on it.
func (c *Channel) Receive(ctx context.Context) ([]ports.ChannelMessage, error) {
	updates, err := c.client.getUpdates(ctx, c.nextOffset())
	if err != nil {
		return nil, err
	}

	admitted := make([]ports.ChannelMessage, 0, len(updates))
	for _, u := range updates {
		c.observe(u.UpdateID)

		if u.Message == nil {
			// An update carrying no message — an edit, a reaction, a join
			// — is not something this channel answers. Dropped, and not
			// logged: it is ordinary traffic, not a refusal.
			continue
		}
		if !c.allowed[u.Message.Chat.ID] {
			c.refuse(u.Message.Chat.ID)
			continue
		}

		c.hold(u.UpdateID)
		admitted = append(admitted, ports.ChannelMessage{
			ID:           strconv.FormatInt(u.UpdateID, 10),
			Conversation: ports.ConversationID(strconv.FormatInt(u.Message.Chat.ID, 10)),
			Text:         u.Message.Text,
			Channel:      Name,
		})
	}
	return admitted, nil
}

// Confirm implements ports.Channel.
func (c *Channel) Confirm(_ context.Context, id string) error {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: confirming %q: not an update id: %w", id, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	kept := c.unconfirmed[:0]
	for _, held := range c.unconfirmed {
		if held > n {
			kept = append(kept, held)
		}
	}
	c.unconfirmed = kept
	return nil
}

// Send implements ports.Channel.
func (c *Channel) Send(ctx context.Context, conversation ports.ConversationID, text string) error {
	chatID, err := strconv.ParseInt(string(conversation), 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: sending to %q: not a chat id: %w", conversation, err)
	}
	return c.client.sendMessage(ctx, chatID, text)
}

// Close implements ports.Channel. The client holds no resource of its own
// — an in-flight poll is interrupted by cancelling the context it was
// given, not by this.
func (c *Channel) Close() error { return nil }

// nextOffset is the cursor Telegram is asked to read from, and it is the
// one place spec R4.1's rule becomes arithmetic.
//
// Telegram acknowledges every update below the offset. So the cursor may
// never pass an admitted message that is not confirmed yet — otherwise a
// capture that failed would be gone. It therefore sits at the lowest
// unconfirmed id when there is one, and past everything seen when there is
// not.
//
// One consequence, stated rather than discovered: a refused update that
// arrives AFTER an unconfirmed admitted one is re-read on every poll until
// that admitted one clears. It is refused again each time, costs one map
// lookup, and cannot be avoided with a single cursor — which is what
// Telegram gives.
func (c *Channel) nextOffset() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.unconfirmed) > 0 {
		return c.unconfirmed[0]
	}
	if c.seen == 0 {
		return 0
	}
	return c.seen + 1
}

func (c *Channel) observe(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id > c.seen {
		c.seen = id
	}
}

func (c *Channel) hold(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.unconfirmed = append(c.unconfirmed, id)
}

// refuse records a message from outside the allow-list.
//
// The chat id is logged and **the message text is not**. A message from an
// unknown sender is untrusted input from an unknown party, and writing its
// body into the operator's log turns an access refusal into an injection
// surface for whoever finds the bot.
//
// Not decision_log either: that table is doc 02 §11's glass box for the
// brain's own decisions, and refusing an unknown sender is a
// transport-level access decision the brain never saw.
func (c *Channel) refuse(chatID int64) {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	_, _ = fmt.Fprintf(c.log, "telegram: refused a message from chat %d: not in allowed_chat_ids\n", chatID)
}
