// +gobra

package session

// Package session is a deterministic recording coordinator. It performs no I/O.
//
// The caller assigns a positive, never-reused identity in its configured identity
// space. That identity also names the immutable capture selection and pieces
// directory, which the caller owns. Error reasons are opaque positive handles.
//
// After every successful operation, persist Records and Queue[:Control.Count]
// together before executing any newly exposed request or returned menu effect.
// Reply only to the current request (including its attempt/launch generation).
// Capture must be dead before Crash or recovery. Drive RequestStop,
// ResumeOrClose, RequestFinish and RequestEndNotice until their work is done;
// these are the scheduler's fair dispatch obligations.
//
// Only end notices require MenuSaid. A returned refusal, gap or automatic-stop
// message is ephemeral. Never acknowledge an end before actually displaying it.
// A finishing retry must use the caller's durable, idempotent finish operation.
//
// To restore, construct a session with the same parameters, load the saved
// records into Records and the saved FIFO into Queue, and set Control.Count
// to the FIFO length. Set Control.Online to false and call Launch. Pending
// requests are deliberately not restored. Persist the result before dispatch.

// MaxSize is an arithmetic safety limit, independent of exploration bounds.
const MaxSize = 1 << 30

const (
	Absent = iota
	Starting
	Live
	Retry
	QueuedPhase
	EndedPhase
)

const (
	NoMode = iota
	StartMode
	LiveMode
	StopMode
)

const (
	NoOutcome = iota
	Handed
	Kept
)

const (
	NoErrorCode = iota
	Failure
	NothingRecordedCode
)

const (
	NoMenu = iota
	RefusedMenu
	GapMenu
	AutomaticStopMenu
)

const (
	StopCause       = -1
	RetryLimitCause = -2
)

type Result struct {
	Outcome int
	Code    int
	Why     int
	Empty   bool
}

type Record struct {
	Phase   int
	Stopped bool
	Retries int
	Cause   int
	Result  Result
}

type Capture struct {
	ID      int
	Mode    int
	Attempt int
	Target  int
}

type EndNotice struct {
	ID        int
	Result    Result
	Directory int
}

// Menu is an effect, not a saved notification or specification history.
type Menu struct {
	Kind   int
	ID     int
	Reason int
}

// Control holds volatile requests, the queue length and the retry policy.
// Count is saved together with Queue; RetryLimit is a constructor parameter.
type Control struct {
	Online     bool
	Capture    Capture
	Finishing  int
	Notice     EndNotice
	Count      int
	RetryLimit int
}

// Session owns its slices. Records[id-1] is absent exactly when Phase is Absent.
// The queue is a packed FIFO; unused cells are zero. Slots and error handles
// have caller-defined meanings, never model-specific identities or values.
type Session struct {
	Records []Record
	Queue   []int
	Control Control
}

// @ ensures yes == (r.Phase == Starting || r.Phase == Live || r.Phase == Retry)
// @ decreases
// @ pure
func IsRunning(r Record) (yes bool) {
	return r.Phase == Starting || r.Phase == Live || r.Phase == Retry
}

// @ ensures out == Record{Phase: Starting}
// @ decreases
// @ pure
func FreshRecord() (out Record) { return Record{Phase: Starting} }

// @ ensures out == Record{Phase: QueuedPhase, Cause: cause}
// @ decreases
// @ pure
func QueuedRecord(cause int) (out Record) { return Record{Phase: QueuedPhase, Cause: cause} }

// @ ensures out == Record{Phase: EndedPhase, Result: v}
// @ decreases
// @ pure
func EndedRecord(v Result) (out Record) { return Record{Phase: EndedPhase, Result: v} }

// @ ensures out == Record{Phase: phase, Stopped: r.Stopped, Retries: r.Retries, Cause: r.Cause, Result: r.Result}
// @ decreases
// @ pure
func WithPhase(r Record, phase int) (out Record) {
	return Record{Phase: phase, Stopped: r.Stopped, Retries: r.Retries, Cause: r.Cause, Result: r.Result}
}

// @ ensures out == Record{Phase: r.Phase, Stopped: true, Retries: r.Retries, Cause: r.Cause, Result: r.Result}
// @ decreases
// @ pure
func WithStop(r Record) (out Record) {
	return Record{Phase: r.Phase, Stopped: true, Retries: r.Retries, Cause: r.Cause, Result: r.Result}
}

// @ requires 0 <= r.Retries && r.Retries < MaxSize
// @ ensures out == Record{Phase: Starting, Stopped: r.Stopped, Retries: r.Retries+1, Cause: r.Cause, Result: r.Result}
// @ decreases
// @ pure
func RestartedRecord(r Record) (out Record) {
	return Record{Phase: Starting, Stopped: r.Stopped, Retries: r.Retries + 1, Cause: r.Cause, Result: r.Result}
}

// @ ensures out == Capture{ID: c.ID, Mode: mode, Attempt: c.Attempt, Target: c.Target}
// @ decreases
// @ pure
func WithMode(c Capture, mode int) (out Capture) {
	return Capture{ID: c.ID, Mode: mode, Attempt: c.Attempt, Target: c.Target}
}

// @ ensures yes == (v.Empty ? v.Outcome == Kept && v.Code == NothingRecordedCode && (v.Why > 0 || v.Why == StopCause || v.Why == RetryLimitCause) : (v.Outcome == Handed && v.Code == NoErrorCode && v.Why == 0) || ((v.Outcome == Handed || v.Outcome == Kept) && v.Code == Failure && v.Why > 0))
// @ decreases
// @ pure
func ValidResult(v Result) (yes bool) {
	return (v.Empty && v.Outcome == Kept && v.Code == NothingRecordedCode &&
		(v.Why > 0 || v.Why == StopCause || v.Why == RetryLimitCause)) ||
		(!v.Empty && ((v.Outcome == Handed && v.Code == NoErrorCode && v.Why == 0) ||
			((v.Outcome == Handed || v.Outcome == Kept) && v.Code == Failure && v.Why > 0)))
}
