package channels

// dedupWindow is how many recently captured message ids the runner
// remembers.
//
// Derived from what redelivery actually is here: the runner confirms after
// every message it captures, so the only ids that can come back are the
// ones in flight when something failed — one batch, plus whatever was
// behind the failure in it. Telegram's own batches are small on a personal
// vault. 256 is that with generous headroom, and the headroom is cheap:
// the ring holds ids, not messages.
//
// **It does not survive a restart**, and that is the owner's 2026-08-23
// ruling rather than an oversight. A restart between a capture and its
// confirm duplicates one message. The durable alternative — persisting the
// last confirmed id — needs a store surface m3c's scope excludes, and it
// slots into ports.Channel's Confirm later without changing the port.
const dedupWindow = 256

// dedupRing remembers the last N ids it was told about, evicting oldest
// first.
//
// A ring rather than a map, because a polling loop runs for the life of
// the process and an unbounded set of every id ever seen is a slow leak
// with no ceiling. The map beside it is an index into the ring, not a
// second store — both are bounded by the same capacity.
//
// Not safe for concurrent use: the runner's loop is the only caller and it
// is single-goroutine by construction.
type dedupRing struct {
	ids   []string
	index map[string]bool
	next  int
}

func newDedupRing(capacity int) *dedupRing {
	if capacity < 0 {
		capacity = 0
	}
	return &dedupRing{ids: make([]string, capacity), index: make(map[string]bool, capacity)}
}

// seen reports whether id was marked and has not yet been evicted.
func (r *dedupRing) seen(id string) bool { return r.index[id] }

// mark records id, evicting the oldest if the ring is full. Marking an id
// already present is a no-op, so a redelivery cannot consume a slot and
// push out something still worth remembering.
func (r *dedupRing) mark(id string) {
	if len(r.ids) == 0 || r.index[id] {
		return
	}

	if old := r.ids[r.next]; old != "" {
		delete(r.index, old)
	}
	r.ids[r.next] = id
	r.index[id] = true
	r.next = (r.next + 1) % len(r.ids)
}

// len reports how many ids the ring currently remembers — test-only, and
// it exists because "bounded" is not observable through seen alone.
func (r *dedupRing) len() int { return len(r.index) }
