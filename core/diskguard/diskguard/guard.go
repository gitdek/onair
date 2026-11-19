// +gobra

package diskguard

// FailedReading represents a failed space query. NoReading means that a
// decision has no Start-refusal notice. All actual readings are byte counts.
const (
	FailedReading int64 = -1
	NoReading     int64 = -2
)

// MaxBytes is the signed 64-bit machine limit, used to reject configurations
// whose safety margins would overflow. Readings have no model-size limit.
const MaxBytes int64 = 1<<63 - 1

// Phase is supplied by the caller that owns the capture lifecycle. The guard
// neither discovers that lifecycle nor keeps a copy of it.
type Phase uint8

const (
	Idle Phase = iota
	Ready
	Running
	Reading
	Suspended
	Stopping
	FinalReading
	Drained
	FinishExisting
)

type Purpose uint8

const (
	NoPurpose Purpose = iota
	StartPurpose
	ResumePurpose
	PiecePurpose
)

// StopCommand is the caller's ordinary Stop command, including closing the
// open piece, joining the recording and handing it over.
type Command uint8

const (
	NoCommand Command = iota
	StopCommand
)

type Reason uint8

const (
	NoReason       Reason = iota
	DiskNearlyFull        // The ordinary Stop reason: "disk nearly full".
)

// Decision is an answer to one supplied reading. Allowed is permission to
// capture. EmitsStop asks the caller to dispatch the guard's ordinary Stop
// exactly once. NoticeFree and NoticeNeed explain a refused Start in bytes;
// NoticeFree == FailedReading reports a failed query to the menu.
type Decision struct {
	Purpose    Purpose
	Reading    int64
	Allowed    bool
	WasStopped bool
	EmitsStop  bool
	NoticeFree int64
	NoticeNeed int64
}

// Guard contains only configuration and the live disk-stop latch. It performs
// no I/O and is owned by one caller. Construct a fresh Guard after a crash.
// Decisions, write accounting and capture lifecycle belong to the caller.
type Guard struct {
	startBytes int64
	stopBytes  int64
	command    Command
	reason     Reason
}

// ValidParameters checks the required headroom without overflowing, even for
// arbitrary signed inputs. Units are bytes; a macOS GB is a billion bytes.
// @ requires true
// @ ensures ok == (reserve > 0 && piece > 0 && piece <= MaxBytes/4 && reserve <= MaxBytes-4*piece && start >= reserve+4*piece)
func ValidParameters(start, reserve, piece int64) (ok bool) {
	if reserve <= 0 || piece <= 0 || piece > MaxBytes/4 {
		return false
	}
	if reserve > MaxBytes-4*piece {
		return false
	}
	return start >= reserve+4*piece
}

// New configures the new-Start threshold and the continuation threshold
// reserve + 2*piece. The caller must bound every piece by piece bytes.
// Invalid parameters return nil, false.
// @ requires true
// @ ensures ok == (reserve > 0 && piece > 0 && piece <= MaxBytes/4 && reserve <= MaxBytes-4*piece && start >= reserve+4*piece)
// @ ensures !ok ==> g == nil
// @ ensures ok ==> g != nil && acc(&g.startBytes) && acc(&g.stopBytes) && acc(&g.command) && acc(&g.reason)
// @ ensures ok ==> g.startBytes == start
// @ ensures ok ==> g.stopBytes == reserve + 2*piece
// @ ensures ok ==> g.command == NoCommand && g.reason == NoReason
// @ ensures ok ==> g.Valid()
func New(start, reserve, piece int64) (g *Guard, ok bool) {
	if !ValidParameters(start, reserve, piece) {
		return nil, false
	}
	return &Guard{startBytes: start, stopBytes: reserve + 2*piece,
		command: NoCommand, reason: NoReason}, true
}

// Valid describes the guard's representation, independent of any exploration
// bound. A zero Guard must be initialized with New before use.
// @ requires g != nil && acc(&g.startBytes, _) && acc(&g.stopBytes, _) && acc(&g.command, _) && acc(&g.reason, _)
// @ decreases
// @ pure
func (g *Guard) Valid() bool {
	return g.stopBytes > 0 && g.startBytes >= g.stopBytes &&
		((g.command == NoCommand && g.reason == NoReason) ||
			(g.command == StopCommand && g.reason == DiskNearlyFull))
}

// RequestedStop returns the latched command and its reason. Dispatch it when
// a Decision has EmitsStop set, rather than dispatching every time it is read.
// @ requires g != nil && acc(&g.command, 1/2) && acc(&g.reason, 1/2)
// @ ensures acc(&g.command, 1/2) && acc(&g.reason, 1/2)
// @ ensures cmd == g.command && why == g.reason && g.command == old(g.command) && g.reason == old(g.reason)
func (g *Guard) RequestedStop() (cmd Command, why Reason) {
	return g.command, g.reason
}

// replyMatches is the size-independent view of a decision used in contracts.
// @ ghost
// @ requires true
// @ decreases
// @ pure func replyMatches(d Decision, p Purpose, r, start, stop int64, stopped bool) bool {
// @     return d.Purpose == p && d.Reading == r && d.WasStopped == stopped &&
// @         d.Allowed == (p == StartPurpose ? r >= start : r >= stop && !stopped) &&
// @         d.EmitsStop == (p != StartPurpose && r < stop && !stopped) &&
// @         d.NoticeFree == (p == StartPurpose && r < start ? r : NoReading) &&
// @         d.NoticeNeed == (p == StartPurpose && r < start ? start : 0)
// @ }

// @ requires p == StartPurpose || p == ResumePurpose || p == PiecePurpose
// @ requires r >= FailedReading && start > 0 && stop > 0
// @ ensures replyMatches(d, p, r, start, stop, stopped)
func makeReply(p Purpose, r, start, stop int64, stopped bool) (d Decision) {
	d = Decision{Purpose: p, Reading: r, WasStopped: stopped, NoticeFree: NoReading}
	if p == StartPurpose {
		d.Allowed = r >= start
		if !d.Allowed {
			d.NoticeFree = r
			d.NoticeNeed = start
		}
	} else {
		d.Allowed = r >= stop && !stopped
		d.EmitsStop = r < stop && !stopped
	}
	return d
}

// CheckStart uses this Start's reading, without consulting any previous one.
// ran distinguishes an inapplicable operation from a refused Start decision.
// @ requires g != nil && acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command, 1/2) && acc(&g.reason, 1/2) && g.Valid()
// @ requires reading >= FailedReading
// @ ensures acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command, 1/2) && acc(&g.reason, 1/2) && g.Valid()
// @ ensures g.startBytes == old(g.startBytes) && g.stopBytes == old(g.stopBytes) && g.command == old(g.command) && g.reason == old(g.reason)
// @ ensures ran == (caller == Idle && old(g.command) == NoCommand)
// @ ensures !ran ==> d == Decision{}
// @ ensures ran ==> replyMatches(d, StartPurpose, reading, g.startBytes, g.stopBytes, false)
func (g *Guard) CheckStart(caller Phase, reading int64) (d Decision, ran bool) {
	if caller != Idle || g.command != NoCommand {
		return d, false
	}
	return makeReply(StartPurpose, reading, g.startBytes, g.stopBytes, false), true
}

// continueWith is shared by piece and resumption checks. A requested Stop
// stays in force regardless of subsequent successful or failed readings.
// @ requires g != nil && acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command) && acc(&g.reason) && g.Valid()
// @ requires reading >= FailedReading && (p == ResumePurpose || p == PiecePurpose)
// @ ensures acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command) && acc(&g.reason) && g.Valid()
// @ ensures g.startBytes == old(g.startBytes) && g.stopBytes == old(g.stopBytes)
// @ ensures replyMatches(d, p, reading, g.startBytes, g.stopBytes, old(g.command) == StopCommand)
// @ ensures g.command == (old(g.command) == StopCommand || reading < g.stopBytes ? StopCommand : NoCommand)
// @ ensures g.reason == (old(g.command) == StopCommand || reading < g.stopBytes ? DiskNearlyFull : NoReason)
func (g *Guard) continueWith(p Purpose, reading int64) (d Decision) {
	d = makeReply(p, reading, g.startBytes, g.stopBytes, g.command == StopCommand)
	if d.EmitsStop {
		g.command = StopCommand
		g.reason = DiskNearlyFull
	}
	return d
}

// RecoverRecording checks an existing recording after a crash or relaunch.
// A denied resumption finishes what exists without opening another piece.
// @ requires g != nil && acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command) && acc(&g.reason) && g.Valid()
// @ requires reading >= FailedReading
// @ ensures acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command) && acc(&g.reason) && g.Valid()
// @ ensures g.startBytes == old(g.startBytes) && g.stopBytes == old(g.stopBytes)
// @ ensures ran == (caller == Idle)
// @ ensures !ran ==> d == Decision{} && g.command == old(g.command) && g.reason == old(g.reason)
// @ ensures ran ==> replyMatches(d, ResumePurpose, reading, g.startBytes, g.stopBytes, old(g.command) == StopCommand)
// @ ensures ran ==> g.command == (old(g.command) == StopCommand || reading < g.stopBytes ? StopCommand : NoCommand)
// @ ensures ran ==> g.reason == (old(g.command) == StopCommand || reading < g.stopBytes ? DiskNearlyFull : NoReason)
func (g *Guard) RecoverRecording(caller Phase, reading int64) (d Decision, ran bool) {
	if caller != Idle {
		return d, false
	}
	return g.continueWith(ResumePurpose, reading), true
}

// ResumeCapture checks a fresh reading after capture was interrupted.
// @ requires g != nil && acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command) && acc(&g.reason) && g.Valid()
// @ requires reading >= FailedReading
// @ ensures acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command) && acc(&g.reason) && g.Valid()
// @ ensures g.startBytes == old(g.startBytes) && g.stopBytes == old(g.stopBytes)
// @ ensures ran == (caller == Suspended)
// @ ensures !ran ==> d == Decision{} && g.command == old(g.command) && g.reason == old(g.reason)
// @ ensures ran ==> replyMatches(d, ResumePurpose, reading, g.startBytes, g.stopBytes, old(g.command) == StopCommand)
// @ ensures ran ==> g.command == (old(g.command) == StopCommand || reading < g.stopBytes ? StopCommand : NoCommand)
// @ ensures ran ==> g.reason == (old(g.command) == StopCommand || reading < g.stopBytes ? DiskNearlyFull : NoReason)
func (g *Guard) ResumeCapture(caller Phase, reading int64) (d Decision, ran bool) {
	if caller != Suspended {
		return d, false
	}
	return g.continueWith(ResumePurpose, reading), true
}

// RetryResumeAfterStop reaffirms a requested Stop even on a good reading.
// @ requires g != nil && acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command) && acc(&g.reason) && g.Valid()
// @ requires reading >= FailedReading
// @ ensures acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command) && acc(&g.reason) && g.Valid()
// @ ensures g.startBytes == old(g.startBytes) && g.stopBytes == old(g.stopBytes)
// @ ensures ran == (old(g.command) == StopCommand && (caller == Stopping || caller == FinalReading || caller == Drained || caller == FinishExisting))
// @ ensures !ran ==> d == Decision{}
// @ ensures g.command == old(g.command) && g.reason == old(g.reason)
// @ ensures ran ==> replyMatches(d, ResumePurpose, reading, g.startBytes, g.stopBytes, true)
func (g *Guard) RetryResumeAfterStop(caller Phase, reading int64) (d Decision, ran bool) {
	if g.command != StopCommand ||
		(caller != Stopping && caller != FinalReading && caller != Drained && caller != FinishExisting) {
		return d, false
	}
	return g.continueWith(ResumePurpose, reading), true
}

// ReportPiece handles the fresh reading after an ordinary piece closes.
// The successor is already open at an ordinary piece reading;
// the two-piece margin covers it and the piece before the following reading.
// @ requires g != nil && acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command) && acc(&g.reason) && g.Valid()
// @ requires reading >= FailedReading
// @ ensures acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command) && acc(&g.reason) && g.Valid()
// @ ensures g.startBytes == old(g.startBytes) && g.stopBytes == old(g.stopBytes)
// @ ensures ran == (caller == Reading)
// @ ensures !ran ==> d == Decision{} && g.command == old(g.command) && g.reason == old(g.reason)
// @ ensures ran ==> replyMatches(d, PiecePurpose, reading, g.startBytes, g.stopBytes, old(g.command) == StopCommand)
// @ ensures ran ==> g.command == (old(g.command) == StopCommand || reading < g.stopBytes ? StopCommand : NoCommand)
// @ ensures ran ==> g.reason == (old(g.command) == StopCommand || reading < g.stopBytes ? DiskNearlyFull : NoReason)
func (g *Guard) ReportPiece(caller Phase, reading int64) (d Decision, ran bool) {
	if caller != Reading {
		return d, false
	}
	return g.continueWith(PiecePurpose, reading), true
}

// ReportFinalPiece handles the last reading without repeating a latched Stop.
// @ requires g != nil && acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command) && acc(&g.reason) && g.Valid()
// @ requires reading >= FailedReading
// @ ensures acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command) && acc(&g.reason) && g.Valid()
// @ ensures g.startBytes == old(g.startBytes) && g.stopBytes == old(g.stopBytes)
// @ ensures ran == (caller == FinalReading)
// @ ensures !ran ==> d == Decision{} && g.command == old(g.command) && g.reason == old(g.reason)
// @ ensures ran ==> replyMatches(d, PiecePurpose, reading, g.startBytes, g.stopBytes, old(g.command) == StopCommand)
// @ ensures ran ==> g.command == (old(g.command) == StopCommand || reading < g.stopBytes ? StopCommand : NoCommand)
// @ ensures ran ==> g.reason == (old(g.command) == StopCommand || reading < g.stopBytes ? DiskNearlyFull : NoReason)
func (g *Guard) ReportFinalPiece(caller Phase, reading int64) (d Decision, ran bool) {
	if caller != FinalReading {
		return d, false
	}
	return g.continueWith(PiecePurpose, reading), true
}

// CaptureStarted acknowledges the caller's running notification only after
// capture was admitted. It does not duplicate the caller's lifecycle state.
// @ requires g != nil && acc(&g.command, 1/2)
// @ ensures acc(&g.command, 1/2) && g.command == old(g.command)
// @ ensures ran == (caller == Ready && g.command == NoCommand)
func (g *Guard) CaptureStarted(caller Phase) (ran bool) {
	return caller == Ready && g.command == NoCommand
}

// CaptureEndedForGood clears the latch only after the caller confirms that
// this capture cannot write again. Interruption alone never clears a Stop.
// @ requires g != nil && acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command) && acc(&g.reason) && g.Valid()
// @ ensures acc(&g.startBytes, 1/2) && acc(&g.stopBytes, 1/2) && acc(&g.command) && acc(&g.reason) && g.Valid()
// @ ensures g.startBytes == old(g.startBytes) && g.stopBytes == old(g.stopBytes)
// @ ensures ran == (caller == Suspended || caller == FinalReading || caller == Drained || caller == FinishExisting)
// @ ensures ran ==> g.command == NoCommand && g.reason == NoReason
// @ ensures !ran ==> g.command == old(g.command) && g.reason == old(g.reason)
func (g *Guard) CaptureEndedForGood(caller Phase) (ran bool) {
	if caller != Suspended && caller != FinalReading && caller != Drained && caller != FinishExisting {
		return false
	}
	g.command = NoCommand
	g.reason = NoReason
	return true
}
