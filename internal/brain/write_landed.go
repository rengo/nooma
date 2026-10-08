package brain

import "errors"

// ErrWriteLanded reports that a user's write landed in the vault but a
// follow-up write (its decision_log row, its learning signal) did not. The
// act is done, so a caller must treat it as success and surface what is
// missing; retrying would only conflict with the write that already
// landed. Match it with errors.Is, and read which part is missing with
// errors.As to a *WriteLandedError.
var ErrWriteLanded = errors.New("brain: the write landed but a follow-up write failed")

// WriteLandedError is the error behind ErrWriteLanded. Record is true when
// the decision_log row is missing, Signal when the learning signal is, and
// both when neither could be written. Err is the underlying cause (the
// joined causes when both failed).
//
// internal/brain has no logger, so the caller (internal/ui) logs Err and
// chooses a notice that names only what actually failed.
type WriteLandedError struct {
	Record bool
	Signal bool
	Err    error
}

// Error implements error. It names only what is missing, so a message
// built from it never claims the record failed when only the signal did.
func (e *WriteLandedError) Error() string {
	what := "a follow-up write"
	switch {
	case e.Record && e.Signal:
		what = "the decision_log row and the learning signal"
	case e.Record:
		what = "the decision_log row"
	case e.Signal:
		what = "the learning signal"
	}
	msg := "brain: the write landed but " + what + " failed"
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Is reports whether target is ErrWriteLanded.
func (e *WriteLandedError) Is(target error) bool {
	return target == ErrWriteLanded
}

// Unwrap returns the underlying cause.
func (e *WriteLandedError) Unwrap() error {
	return e.Err
}
