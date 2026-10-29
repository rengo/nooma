package brain

import (
	"context"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// TimersService is /ui/timers' entrance (doc 02 §8): the live timers, and a
// cancel through the chat's own transition (cancelListedTimer). A timer is
// read from timers alone, never through units (I04).
type TimersService struct {
	clock  ports.Clock
	ids    ports.IDGen
	timers ports.TimerRepo
	log    ports.DecisionLog
}

// NewTimersService wires a TimersService.
func NewTimersService(clock ports.Clock, ids ports.IDGen, timers ports.TimerRepo, log ports.DecisionLog) *TimersService {
	return &TimersService{clock: clock, ids: ids, timers: timers, log: log}
}

// Live is every pending timer, by fire_at then id, each read in the clock's
// zone. It writes nothing.
func (s *TimersService) Live(ctx context.Context) ([]ListedTimer, error) {
	live, _, err := s.read(ctx)
	return live, err
}

// Cancel cancels the live timer id, counted at its position in the list Live
// reads now, as the chat counts its position in the list it showed. A timer
// no longer pending is ErrTimerNoLongerSet and nothing is written. It returns
// the timer cancelled.
func (s *TimersService) Cancel(ctx context.Context, id string) (ListedTimer, error) {
	live, now, err := s.read(ctx)
	if err != nil {
		return ListedTimer{}, err
	}
	for i, t := range live {
		if t.ID != id {
			continue
		}
		if err := cancelListedTimer(ctx, s.timers, s.log, s.ids, t, i+1, len(live), timerSurfaceUI, now); err != nil {
			return t, fmt.Errorf("timers: %w", err)
		}
		return t, nil
	}
	return ListedTimer{}, ErrTimerNoLongerSet
}

// read is an operation's one clock read and the live timers in its zone.
func (s *TimersService) read(ctx context.Context) ([]ListedTimer, time.Time, error) {
	now := s.clock.Now()
	live, err := readLiveTimers(ctx, s.timers, now)
	if err != nil {
		return nil, now, fmt.Errorf("timers: %w", err)
	}
	return live, now, nil
}
