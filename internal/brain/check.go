package brain

import (
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// triggerTransition and timerTransition are the red step's stubs: neither
// writes, whatever the verdict.
func triggerTransition(_ prospection.Verdict) (ports.TriggerStatus, bool) { return "", false }

func timerTransition(_ prospection.Verdict) (ports.TimerStatus, bool) { return "", false }
