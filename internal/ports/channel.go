package ports

import "context"

// ConversationID is where a reply goes — the transport's own identity for
// one conversation, opaque above the adapter.
//
// A defined string type rather than a bare one so a conversation id cannot
// be passed where a message id is expected, and a string rather than an
// int64 because it is Telegram's chat id today and need not be a number
// for whatever comes next (ADR-0006 settles the first channel, not the
// last).
type ConversationID string

// ChannelMessage is one inbound message that has already been admitted —
// whatever admission rule a channel has, it applied before handing this
// upward. The port declares no allow-list, because an allow-list is a
// property of a specific transport's identity space and internal/ports has
// no way to express "chat id" without naming a vendor.
type ChannelMessage struct {
	// ID is the transport's own identifier for this message, opaque above
	// the adapter. It exists so a caller can tell a redelivery from a new
	// message, and so Confirm has something to name.
	ID string
	// Conversation is where a reply goes. Nothing above the adapter
	// constructs one — a caller only ever echoes back what it received.
	Conversation ConversationID
	// Text is the message body, verbatim. Nothing trims, normalises or
	// interprets it here: classify is what reads it, and it reads what the
	// user actually typed.
	Text string
	// Channel is this channel's own name, and becomes
	// brain.CaptureInput.Channel and therefore units.source — which is NOT
	// NULL, so an empty name would fail at the store rather than here.
	Channel string
}

// Channel is the port over a conversation surface: something a message can
// arrive from and a reply can go back to.
//
// Telegram is the first implementation (ADR-0006) and this interface names
// it nowhere — that is the point of the interface, and
// test/conformance/channel_port_names_no_vendor_test.go is what keeps it
// true rather than aspirational.
//
// Five methods, and three absences that are deliberate:
//
//   - No method whose name begins Delete, Remove, Purge, Drop or Destroy —
//     I03's strengthened prefix set. A channel that could delete a
//     conversation would be "nothing is deleted" failing in a different
//     table.
//   - No Start. The receive/handle/confirm loop is transport-independent
//     and lives one layer up in internal/channels, so a second channel
//     reuses it whole instead of reimplementing it. An adapter that owned
//     the loop would also have to name the handler's return type, and
//     brain.CaptureResult is not something internal/ports can reference
//     without importing the layer above it.
//   - No allow-list, no credentials, no configuration. Those are a
//     transport's own, and a port carrying them would be Telegram's shape
//     with the name filed off.
type Channel interface {
	// Name returns this channel's own name, non-empty — the string that
	// becomes units.source for every capture arriving through it.
	Name() string

	// Receive returns every admitted message the channel has for the
	// caller, blocking up to the transport's own timeout. An empty slice
	// with a nil error is the ordinary quiet case and never an error: a
	// conversation is idle most of the time, and reporting that as a
	// failure would drive a caller's backoff on every poll.
	//
	// A message Receive has returned and the caller has not yet confirmed
	// is returned again by a later call. That is not a defect to be
	// tolerated — it is the contract, and it is what makes losing a
	// capture impossible (see Confirm).
	Receive(ctx context.Context) ([]ChannelMessage, error)

	// Confirm tells the channel that every message up to and including id
	// has been handled durably and need not be delivered again.
	//
	// It is its own method rather than a parameter on the next Receive,
	// and that is this interface's one non-obvious shape. Folding it in —
	// Receive(ctx, confirmedThrough string) — reads perfectly well and
	// hides the thing that matters: **the confirm is the durability
	// boundary**. A caller that never confirmed would re-read the same
	// message forever with no method call missing from the trace, and the
	// bug would look like a transport fault. As its own method it is a
	// line a reviewer can look for and not find.
	//
	// A caller confirms only after the work the message caused is durable.
	// Confirming first loses messages; confirming last duplicates them,
	// and duplicating is the recoverable half.
	Confirm(ctx context.Context, id string) error

	// Send posts text into conversation. The reply is not durable and a
	// caller does not retry it by re-doing the work that produced it.
	Send(ctx context.Context, conversation ConversationID, text string) error

	// Close releases the channel's resources. It does not wait for an
	// in-flight Receive to return on its own — a long poll legitimately
	// holds a connection open for its full timeout, and a caller cancels
	// the context it passed to Receive to interrupt one.
	Close() error
}
