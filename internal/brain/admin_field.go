package brain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/rengo/nooma/internal/core/weight"
)

// ConfigField names one setting the admin page may change: the closed set of
// m4e2-admin's Q5 ruling, each value being the config column it writes.
// relation_thresholds and consolidation_last_run_at are not members.
type ConfigField string

// The five settings the admin page writes.
const (
	ConfigWeightThreshold      ConfigField = "weight_threshold"
	ConfigHysteresisMargin     ConfigField = "hysteresis_margin"
	ConfigGoalStagnationDays   ConfigField = "goal_stagnation_days"
	ConfigMentalLoadThreshold  ConfigField = "mental_load_threshold"
	ConfigConsolidationEnabled ConfigField = "consolidation_enabled"
)

// ErrUnknownConfigField is ParseConfigField's answer to a name outside the
// five. It is returned before anything is read or written.
var ErrUnknownConfigField = errors.New("admin: unknown config field")

// ParseConfigField reads one of the five field names, and nothing else.
func ParseConfigField(name string) (ConfigField, error) {
	switch f := ConfigField(name); f {
	case ConfigWeightThreshold, ConfigHysteresisMargin, ConfigGoalStagnationDays,
		ConfigMentalLoadThreshold, ConfigConsolidationEnabled:
		return f, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownConfigField, name)
}

// ConfigRangeError is a submitted value refused: it does not parse, or it
// is outside the set the runtime's Resolve function accepts. Nothing was
// written. Message is fit to show next to the form field.
type ConfigRangeError struct {
	Field   ConfigField
	Raw     string
	Message string
}

// Error implements error.
func (e *ConfigRangeError) Error() string {
	return fmt.Sprintf("admin: %s %q refused: %s", e.Field, e.Raw, e.Message)
}

// expected is what a field accepts, in words, for the refusal message. It
// describes the set; the set itself is whatever Resolve returns unchanged.
func (f ConfigField) expected() string {
	switch f {
	case ConfigWeightThreshold:
		return fmt.Sprintf("a number from 0 to %v", weight.WeightCeiling)
	case ConfigHysteresisMargin:
		return "a number, 0 or more"
	case ConfigGoalStagnationDays, ConfigMentalLoadThreshold:
		return "a whole number, 1 or more"
	}
	return "true or false"
}

// refuse builds the ConfigRangeError for raw.
func (f ConfigField) refuse(raw string) error {
	return &ConfigRangeError{Field: f, Raw: raw, Message: "enter " + f.expected()}
}

// parseFloatField, parseIntField and parseBoolField read the raw form text.
// Booleans are exactly "true" or "false", not strconv.ParseBool's eight
// spellings. A failure is reported as the field's own refusal.
func parseFloatField(f ConfigField, raw string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, f.refuse(raw)
	}
	return v, nil
}

func parseIntField(f ConfigField, raw string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, f.refuse(raw)
	}
	return v, nil
}

func parseBoolField(f ConfigField, raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, f.refuse(raw)
}
