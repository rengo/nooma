package prospection

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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

// ValidEventLeads reports whether minutes are leads ParseEventReminderLeads
// accepts.
func ValidEventLeads(minutes []int) bool {
	_, err := eventLeads(minutes)
	return err == nil
}

// WithEventLeads is p with an event's own leads (I40, ADR-0038) in place of the
// user's, when ValidEventLeads accepts them; otherwise p.
func (p ReminderPrefs) WithEventLeads(minutes []int) ReminderPrefs {
	if leads, err := eventLeads(minutes); err == nil {
		p.TimedLeads, p.ownLeads = leads, true
	}
	return p
}

// AddEventLeads is current with added joined to it (ADR-0045): each lead once,
// the furthest first, the order a preference lists them in. A follow-up that
// adds a lead ("también 1 hora antes") keeps every reminder the item had.
func AddEventLeads(current, added []int) []int {
	out := slices.Clone(current)
	for _, m := range added {
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b int) int { return b - a })
	return out
}

func eventLeads(minutes []int) ([]time.Duration, error) {
	raw, err := json.Marshal(minutes)
	if err != nil {
		return nil, err
	}
	return ParseEventReminderLeads(string(raw))
}

// TimedEvent reports whether at, read in zone, names a time of day rather than
// a day alone, and so is reminded at leads (ADR-0029 points 1-2).
func TimedEvent(at time.Time, zone *time.Location) bool { return !isDateOnly(at.In(zone)) }

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

// ResolveReminderPrefs is the preferences a vault's config row states
// (ADR-0029's leads and date-only time, ADR-0030's quiet end and evening
// time), each falling back to its own default when absent or invalid — the
// posture of every other Resolve* over that row: Load never sanitizes,
// this does.
func ResolveReminderPrefs(leads, dateOnlyAt, quietEnd, eveningAt *string, remindMeLead *int) ReminderPrefs {
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
	if quietEnd != nil {
		if v, err := ParseQuietEnd(*quietEnd); err == nil {
			p.QuietEnd = v
		}
	}
	// Judged against the quiet end just resolved, not the stored one: a
	// corrupt quiet end is not the night the evening has to stay out of.
	if eveningAt != nil {
		if v, err := ParseEveningAt(*eveningAt, p.QuietEnd); err == nil {
			p.EveningAt = v
		}
	}
	if remindMeLead != nil {
		if v, err := ParseRemindMeLead(*remindMeLead); err == nil {
			p.RemindMeLead = v
		}
	}
	return p
}

// ParseRemindMeLead reads config.remind_me_lead_minutes: whole minutes from 1
// to MaxEventLeadDays days (ADR-0031 point 2).
func ParseRemindMeLead(minutes int) (time.Duration, error) {
	if minutes < 1 || minutes > MaxEventLeadDays*24*60 {
		return 0, fmt.Errorf("%w: remind-me lead %d is not a whole number of minutes from 1 to %d days", ErrInvalidReminderPref, minutes, MaxEventLeadDays)
	}
	return time.Duration(minutes) * time.Minute, nil
}

// MaxQuietEndHour is the latest quiet end a user may set, on the hour
// (ADR-0030 point 5): a night that runs past noon is not a night.
const MaxQuietEndHour = 12

// before reports whether d is earlier in the day than o.
func (d TimeOfDay) before(o TimeOfDay) bool {
	return d.Hour*60+d.Minute < o.Hour*60+o.Minute
}

// ParseQuietEnd reads config.quiet_hours_end: HH:MM from 00:01 to
// MaxQuietEndHour:00. Quiet hours are [00:00, quiet end), so 00:00 would be no
// night at all.
func ParseQuietEnd(s string) (TimeOfDay, error) {
	d, err := ParseTimeOfDay(s)
	if err != nil {
		return TimeOfDay{}, err
	}
	if d == (TimeOfDay{}) || (TimeOfDay{Hour: MaxQuietEndHour}).before(d) {
		return TimeOfDay{}, fmt.Errorf("%w: quiet end %q, want 00:01 to 12:00", ErrInvalidReminderPref, s)
	}
	return d, nil
}

// ParseEveningAt reads config.evening_reminder_at: HH:MM from quietEnd to
// 23:59, so the evening a reminder is moved to is never in quiet hours.
func ParseEveningAt(s string, quietEnd TimeOfDay) (TimeOfDay, error) {
	d, err := ParseTimeOfDay(s)
	if err != nil {
		return TimeOfDay{}, err
	}
	if d.before(quietEnd) {
		return TimeOfDay{}, fmt.Errorf("%w: evening time %q is before quiet end %02d:%02d", ErrInvalidReminderPref, s, quietEnd.Hour, quietEnd.Minute)
	}
	return d, nil
}
