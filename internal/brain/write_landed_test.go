package brain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// WriteLandedError is the contract between this package and its callers
// (the mirror's handlers now, the admin form later): the act landed, one or
// both follow-up writes did not, and the caller must be able to tell which.

func TestWriteLandedError_IsErrWriteLandedAndNothingElse(t *testing.T) {
	cause := errors.New("disk full")
	err := &WriteLandedError{Record: true, Err: cause}

	if !errors.Is(err, ErrWriteLanded) {
		t.Error("errors.Is(WriteLandedError, ErrWriteLanded) = false, want true")
	}
	if errors.Is(err, errors.New("brain: the write landed but a follow-up write failed")) {
		t.Error("errors.Is matched an unrelated error with the same text, want identity only")
	}
	if errors.Is(cause, ErrWriteLanded) {
		t.Error("errors.Is(plain cause, ErrWriteLanded) = true, want false: a plain error is not a landed write")
	}
}

func TestWriteLandedError_AsGivesThePartsThroughWrapping(t *testing.T) {
	cases := []struct{ record, signal bool }{{true, false}, {false, true}, {true, true}}
	for _, tc := range cases {
		wrapped := fmt.Errorf("retire belief: %w", &WriteLandedError{Record: tc.record, Signal: tc.signal, Err: errors.New("x")})

		var got *WriteLandedError
		if !errors.As(wrapped, &got) {
			t.Fatalf("errors.As(%v) = false, want true", wrapped)
		}
		if got.Record != tc.record || got.Signal != tc.signal {
			t.Errorf("parts = Record %v Signal %v, want Record %v Signal %v", got.Record, got.Signal, tc.record, tc.signal)
		}
	}
}

func TestWriteLandedError_UnwrapsToItsCauses(t *testing.T) {
	recordErr, signalErr := errors.New("record failed"), errors.New("signal failed")
	err := &WriteLandedError{Record: true, Signal: true, Err: errors.Join(recordErr, signalErr)}

	if !errors.Is(err, recordErr) || !errors.Is(err, signalErr) {
		t.Errorf("errors.Is(err, recordErr) = %v, errors.Is(err, signalErr) = %v, want both true", errors.Is(err, recordErr), errors.Is(err, signalErr))
	}
	if errors.Unwrap(&WriteLandedError{Signal: true}) != nil {
		t.Error("Unwrap of an error with no cause is not nil")
	}
}

func TestWriteLandedError_MessageNamesOnlyWhatIsMissing(t *testing.T) {
	cause := errors.New("disk full")
	cases := []struct {
		name         string
		err          *WriteLandedError
		wantRecord   bool
		wantSignal   bool
		wantFallback bool
	}{
		{"record only", &WriteLandedError{Record: true, Err: cause}, true, false, false},
		{"signal only", &WriteLandedError{Signal: true, Err: cause}, false, true, false},
		{"both", &WriteLandedError{Record: true, Signal: true, Err: cause}, true, true, false},
		{"neither flag", &WriteLandedError{Err: cause}, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			if got := strings.Contains(msg, "decision_log row"); got != tc.wantRecord {
				t.Errorf("message %q mentions the decision_log row = %v, want %v", msg, got, tc.wantRecord)
			}
			if got := strings.Contains(msg, "learning signal"); got != tc.wantSignal {
				t.Errorf("message %q mentions the learning signal = %v, want %v", msg, got, tc.wantSignal)
			}
			if got := strings.Contains(msg, "a follow-up write"); got != tc.wantFallback {
				t.Errorf("message %q uses the generic wording = %v, want %v", msg, got, tc.wantFallback)
			}
			if !strings.Contains(msg, "disk full") {
				t.Errorf("message %q does not carry the cause", msg)
			}
		})
	}
	if msg := (&WriteLandedError{Signal: true}).Error(); strings.Contains(msg, "nil") {
		t.Errorf("message %q prints a nil cause", msg)
	}
}
