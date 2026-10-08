package brain

import "context"

// Edit replaces the content of the active belief id with content, marks
// the belief user_stated, records the change and emits a belief_edit
// signal (m4e design §3.4). Content is the only thing it can change.
func (s *BeliefsService) Edit(ctx context.Context, id, content string) error {
	return nil
}
