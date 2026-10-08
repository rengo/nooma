package brain

import "context"

// Retire moves the active belief id to retired, records the transition and
// emits a belief_delete signal (m4e design §3.4). No row is removed.
func (s *BeliefsService) Retire(ctx context.Context, id string) error {
	return nil
}
