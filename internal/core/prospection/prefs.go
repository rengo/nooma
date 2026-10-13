package prospection

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The bounds a reminder preference must respect (ADR-0029 point 5): at most
// MaxEventReminderLeads reminders per timed event, none further ahead than
// MaxEventLeadDays.
const (
	MaxEventReminderLeads = 5
	MaxEventLeadDays      = 30
)

// ErrInvalidReminderPref is every reason a stored or submitted reminder
// preference is refused.
var ErrInvalidReminderPref = errors.New("invalid reminder preference")

// ParseEventReminderLeads reads config.event_reminder_leads: a JSON array of
// 1 to MaxEventReminderLeads whole minutes, each from 1 minute to
// MaxEventLeadDays days, no two equal.
func ParseEventReminderLeads(s string) ([]time.Duration, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, fmt.Errorf("%w: leads %q are not a JSON array", ErrInvalidReminderPref, s)
	}
	if len(raw) == 0 || len(raw) > MaxEventReminderLeads {
		return nil, fmt.Errorf("%w: %d leads, want 1 to %d", ErrInvalidReminderPref, len(raw), MaxEventReminderLeads)
	}
	leads := make([]time.Duration, 0, len(raw))
	for _, r := range raw {
		minutes, err := strconv.Atoi(strings.TrimSpace(string(r)))
		if err != nil || minutes < 1 || minutes > MaxEventLeadDays*24*60 {
			return nil, fmt.Errorf("%w: lead %s is not a whole number of minutes from 1 to %d days", ErrInvalidReminderPref, r, MaxEventLeadDays)
		}
		lead := time.Duration(minutes) * time.Minute
		for _, seen := range leads {
			if seen == lead {
				return nil, fmt.Errorf("%w: lead %d minutes given twice", ErrInvalidReminderPref, minutes)
			}
		}
		leads = append(leads, lead)
	}
	return leads, nil
}

// ParseTimeOfDay reads config.date_only_reminder_at: HH:MM, 00:00 to 23:59.
func ParseTimeOfDay(s string) (TimeOfDay, error) {
	bad := fmt.Errorf("%w: time of day %q, want HH:MM from 00:00 to 23:59", ErrInvalidReminderPref, s)
	if len(s) != 5 || s[2] != ':' {
		return TimeOfDay{}, bad
	}
	h, herr := strconv.ParseUint(s[:2], 10, 8)
	m, merr := strconv.ParseUint(s[3:], 10, 8)
	if herr != nil || merr != nil || h > 23 || m > 59 {
		return TimeOfDay{}, bad
	}
	return TimeOfDay{Hour: int(h), Minute: int(m)}, nil
}

// ResolveReminderPrefs is the preferences a vault's config row states, each
// falling back to its own default when absent or invalid — the posture of
// every other Resolve* over that row: Load never sanitizes, this does.
func ResolveReminderPrefs(leads, dateOnlyAt *string) ReminderPrefs {
	p := DefaultReminderPrefs()
	if leads != nil {
		if v, err := ParseEventReminderLeads(*leads); err == nil {
			p.TimedLeads = v
		}
	}
	if dateOnlyAt != nil {
		if v, err := ParseTimeOfDay(*dateOnlyAt); err == nil {
			p.DateOnlyAt = v
		}
	}
	return p
}
