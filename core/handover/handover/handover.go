// +gobra

package handover

// CounterLimit leaves headroom for integer arithmetic on a 64-bit machine.
// It is an implementation limit, independent of exploration or retry policy.
const CounterLimit = 1 << 62

type Outcome int

const (
	NotStarted Outcome = iota
	Active
	HandedOver
	Failed
)

type Phase int

const (
	Idle Phase = iota
	Inspect
	InspectTaken
	Preclean
	Copying
	Verify
	Discard
	Publish
	Retention
	Terminal
)

type Reason int

const (
	NoReason Reason = iota
	FolderGone
	FolderFull
	NameConflict
	DifferentCopies
)

type Cleanup int

const (
	Dormant Cleanup = iota
	Waiting
	Cleaning
	CleanupFailed
	CleanupDone
)

type Kind int

const (
	NoKind Kind = iota
	CompareNamed
	Copy
	CompareHidden
	Rename
	RemoveHidden
	RemoveSpool
)

// Target denotes a path owned by this hand-over. There is no arbitrary-path
// request. Rename must be implemented as a non-replacing publication.
type Target int

const (
	HiddenTarget Target = iota
	FinalTarget
	SpoolTarget
)

type Reply int

const (
	NoReply Reply = iota
	OK
	Absent
	Same
	Different
	Taken
	ReplyFolderGone
	ReplyFolderFull
	RemoveFailed
)

// OperationID is scoped to a recording. The caller durably shares execution and its
// original result for each token, including requests replayed while in flight.
type OperationID struct {
	Attempt    int
	Round      int
	Phase      Phase
	CleanupTry int
}

type Request struct {
	Kind   Kind
	Target Target
	ID     OperationID
}

// Record is the core's durable progress. The zero value is a new hand-over.
// It contains no caller execution, disk contents, clock, or historical counters.
type Record struct {
	Outcome        Outcome
	Phase          Phase
	Reason         Reason
	Attempt        int
	Round          int
	Differences    int
	Cleanup        Cleanup
	CleanupReason  Reply
	CleanupTry     int
	Intent         Request
	HiddenVerified bool
	NamedVerified  bool
}

// Handover is an immutable value. Persist the returned value before making its
// request visible to the caller. Restore that same value after interruption.
// Recording and DifferenceLimit are immutable configuration and must be restored
// with Record. A recording's namespace must not be reused for a different file.
type Handover struct {
	Recording       string
	DifferenceLimit int
	Record          Record
}

// Path is an abstract path component. The caller resolves directory against its
// configured folder or spool and prepends Prefix to Name. Recording is a caller-
// chosen path component, not a path to be interpreted by this core.
type Path struct {
	Directory string
	Prefix    string
	Name      string
}

// The logical views below describe transitions without mutable permissions:
// receivers and results are values, and requests have no pointer-bearing fields.
// @ ghost
// @ requires true
// @ decreases
// @ pure func deliveryFailed(reply Reply) bool { return reply == ReplyFolderGone || reply == ReplyFolderFull }

// @ ghost
// @ requires true
// @ decreases
// @ pure func delivered(phase Phase, reply Reply) bool { return ((phase == Inspect || phase == InspectTaken) && reply == Same) || (phase == Publish && reply == OK) }

// @ ghost
// @ requires true
// @ decreases
// @ pure func conflicted(phase Phase, reply Reply) bool { return (phase == Inspect || phase == InspectTaken) && reply == Different }

// @ ghost
// @ requires true
// @ decreases
// @ pure func exhausted(h Handover, reply Reply) bool { return h.Record.Phase == Discard && reply == OK && h.Record.Differences == h.DifferenceLimit }

// New configures a hand-over. Delivery and cleanup retries have no policy limit;
// the caller chooses when to retry. Differing copies have a configured limit.
// @ requires true
// @ ensures ok == (recording != "" && 0 < differenceLimit && differenceLimit <= CounterLimit)
// @ ensures !ok ==> h == Handover{}
// @ ensures ok ==> h.Recording == recording && h.DifferenceLimit == differenceLimit && h.Record == Record{}
func New(recording string, differenceLimit int) (h Handover, ok bool) {
	if recording == "" || differenceLimit <= 0 || differenceLimit > CounterLimit {
		return h, false
	}
	return Handover{Recording: recording, DifferenceLimit: differenceLimit}, true
}

// Path resolves only this hand-over's three owned names.
// @ requires true
// @ ensures ok == (target == HiddenTarget || target == FinalTarget || target == SpoolTarget)
// @ ensures !ok ==> path == Path{}
// @ ensures ok ==> path.Name == h.Recording
// @ ensures ok ==> path.Directory == (target == SpoolTarget ? "spool" : "folder")
// @ ensures ok ==> path.Prefix == (target == HiddenTarget ? "." : "")
func (h Handover) Path(target Target) (path Path, ok bool) {
	switch target {
	case HiddenTarget:
		return Path{Directory: "folder", Prefix: ".", Name: h.Recording}, true
	case FinalTarget:
		return Path{Directory: "folder", Name: h.Recording}, true
	case SpoolTarget:
		return Path{Directory: "spool", Name: h.Recording}, true
	default:
		return path, false
	}
}

// @ requires true
// @ ensures ok == (running && h.Record.Outcome == NotStarted)
// @ ensures !ok ==> next == h
// @ ensures next.Recording == h.Recording && next.DifferenceLimit == h.DifferenceLimit
// @ ensures ok ==> next.Record.Outcome == (Active)
// @ ensures ok ==> next.Record.Phase == (Inspect)
// @ ensures next.Record.Reason == h.Record.Reason
// @ ensures ok ==> next.Record.Attempt == (1)
// @ ensures ok ==> next.Record.Round == (1)
// @ ensures next.Record.Differences == h.Record.Differences
// @ ensures next.Record.Cleanup == h.Record.Cleanup
// @ ensures next.Record.CleanupReason == h.Record.CleanupReason
// @ ensures next.Record.CleanupTry == h.Record.CleanupTry
// @ ensures next.Record.Intent == h.Record.Intent
// @ ensures next.Record.HiddenVerified == h.Record.HiddenVerified
// @ ensures next.Record.NamedVerified == h.Record.NamedVerified
func (h Handover) Start(running bool) (next Handover, ok bool) {
	next = h
	if !running || h.Record.Outcome != NotStarted {
		return next, false
	}
	next.Record.Outcome = Active
	next.Record.Phase = Inspect
	next.Record.Attempt = 1
	next.Record.Round = 1
	return next, true
}

// RetryDelivery takes an observation of whether a hidden file remains from the
// failed attempt. Any such file must be removed before another copy is requested.
// @ requires true
// @ ensures ok == (running && h.Record.Outcome == Failed && h.Record.Attempt < CounterLimit)
// @ ensures !ok ==> next == h
// @ ensures next.Recording == h.Recording && next.DifferenceLimit == h.DifferenceLimit
// @ ensures ok ==> next.Record.Outcome == (Active)
// @ ensures ok ==> next.Record.Phase == (hiddenAbsent ? Inspect : Preclean)
// @ ensures ok ==> next.Record.Reason == (NoReason)
// @ ensures ok ==> next.Record.Attempt == (h.Record.Attempt + 1)
// @ ensures ok ==> next.Record.Round == (1)
// @ ensures ok ==> next.Record.Differences == (0)
// @ ensures next.Record.Cleanup == h.Record.Cleanup
// @ ensures next.Record.CleanupReason == h.Record.CleanupReason
// @ ensures next.Record.CleanupTry == h.Record.CleanupTry
// @ ensures next.Record.Intent == h.Record.Intent
// @ ensures ok ==> next.Record.HiddenVerified == (false)
// @ ensures next.Record.NamedVerified == h.Record.NamedVerified
func (h Handover) RetryDelivery(running, hiddenAbsent bool) (next Handover, ok bool) {
	next = h
	if !running || h.Record.Outcome != Failed || h.Record.Attempt >= CounterLimit {
		return next, false
	}
	next.Record.Outcome = Active
	next.Record.Reason = NoReason
	next.Record.Attempt++
	next.Record.Round = 1
	next.Record.Differences = 0
	next.Record.HiddenVerified = false
	next.Record.Phase = Preclean
	if hiddenAbsent {
		next.Record.Phase = Inspect
	}
	return next, true
}

// Issue saves exactly one intent. Persist next before executing next.Record.Intent.
// @ requires true
// @ ensures ok == (running && h.Record.Intent == Request{} && (h.Record.Outcome == Active || h.Record.Cleanup == Cleaning) && Inspect <= h.Record.Phase && h.Record.Phase <= Retention)
// @ ensures !ok ==> next == h
// @ ensures next.Recording == h.Recording && next.DifferenceLimit == h.DifferenceLimit
// @ ensures next.Record.Outcome == h.Record.Outcome
// @ ensures next.Record.Phase == h.Record.Phase
// @ ensures next.Record.Reason == h.Record.Reason
// @ ensures next.Record.Attempt == h.Record.Attempt
// @ ensures next.Record.Round == h.Record.Round
// @ ensures next.Record.Differences == h.Record.Differences
// @ ensures next.Record.Cleanup == h.Record.Cleanup
// @ ensures next.Record.CleanupReason == h.Record.CleanupReason
// @ ensures next.Record.CleanupTry == h.Record.CleanupTry
// @ ensures ok ==> next.Record.Intent == (Request{Kind: (h.Record.Phase == Inspect || h.Record.Phase == InspectTaken ? CompareNamed : (h.Record.Phase == Preclean || h.Record.Phase == Discard ? RemoveHidden : (h.Record.Phase == Copying ? Copy : (h.Record.Phase == Verify ? CompareHidden : (h.Record.Phase == Publish ? Rename : RemoveSpool))))), Target: (h.Record.Phase == Inspect || h.Record.Phase == InspectTaken || h.Record.Phase == Publish ? FinalTarget : (h.Record.Phase == Retention ? SpoolTarget : HiddenTarget)), ID: OperationID{Attempt: h.Record.Attempt, Round: h.Record.Round, Phase: h.Record.Phase, CleanupTry: h.Record.CleanupTry}})
// @ ensures next.Record.HiddenVerified == h.Record.HiddenVerified
// @ ensures next.Record.NamedVerified == h.Record.NamedVerified
func (h Handover) Issue(running bool) (next Handover, ok bool) {
	next = h
	if !running || h.Record.Intent != (Request{}) || (h.Record.Outcome != Active && h.Record.Cleanup != Cleaning) {
		return next, false
	}
	var kind Kind
	var target Target
	switch h.Record.Phase {
	case Inspect, InspectTaken:
		kind, target = CompareNamed, FinalTarget
	case Preclean, Discard:
		kind, target = RemoveHidden, HiddenTarget
	case Copying:
		kind, target = Copy, HiddenTarget
	case Verify:
		kind, target = CompareHidden, HiddenTarget
	case Publish:
		kind, target = Rename, FinalTarget
	case Retention:
		kind, target = RemoveSpool, SpoolTarget
	default:
		return next, false
	}
	next.Record.Intent = Request{Kind: kind, Target: target, ID: OperationID{
		Attempt: h.Record.Attempt, Round: h.Record.Round,
		Phase: h.Record.Phase, CleanupTry: h.Record.CleanupTry,
	}}
	return next, true
}

// Resend repeats the saved intent when the transport is empty. It never allocates
// another identity, and does not need to know whether execution already finished.
// @ requires true
// @ ensures ok == (running && h.Record.Intent != Request{} && wire == Request{})
// @ ensures request == (ok ? h.Record.Intent : Request{})
func (h Handover) Resend(running bool, wire Request) (request Request, ok bool) {
	if !running || h.Record.Intent == (Request{}) || wire != (Request{}) {
		return request, false
	}
	return h.Record.Intent, true
}

// validReply rejects results that cannot answer the current phase.
// @ requires true
// @ ensures valid == (((phase == Inspect || phase == InspectTaken) && (reply == Absent || reply == Same || reply == Different)) || (phase == Verify && (reply == Same || reply == Different)) || ((phase == Copying || phase == Preclean || phase == Discard) && reply == OK) || (phase == Publish && (reply == OK || reply == Taken)) || (phase == Retention && (reply == OK || reply == RemoveFailed)) || (phase >= Inspect && phase <= Publish && (reply == ReplyFolderGone || reply == ReplyFolderFull)))
// @ decreases
// @ pure
func validReply(phase Phase, reply Reply) (valid bool) {
	return ((phase == Inspect || phase == InspectTaken) && (reply == Absent || reply == Same || reply == Different)) ||
		(phase == Verify && (reply == Same || reply == Different)) ||
		((phase == Copying || phase == Preclean || phase == Discard) && reply == OK) ||
		(phase == Publish && (reply == OK || reply == Taken)) ||
		(phase == Retention && (reply == OK || reply == RemoveFailed)) ||
		(phase >= Inspect && phase <= Publish && (reply == ReplyFolderGone || reply == ReplyFolderFull))
}

// Consume records a cached caller result only for the outstanding identity.
// done and wire describe the caller's delivery of that result. They are inputs,
// not durable core state. Unknown, premature, and mismatched results are refused.
// @ requires true
// @ ensures ok == (running && h.Record.Intent != Request{} && wire == h.Record.Intent && done && validReply(h.Record.Phase, reply) && !(h.Record.Phase == Verify && reply == Different && h.Record.Differences >= CounterLimit) && !(h.Record.Phase == Discard && reply == OK && !exhausted(h, reply) && h.Record.Round >= CounterLimit))
// @ ensures !ok ==> next == h
// @ ensures next.Recording == h.Recording && next.DifferenceLimit == h.DifferenceLimit
// @ ensures ok ==> next.Record.Outcome == (h.Record.Phase == Retention ? h.Record.Outcome : ((deliveryFailed(reply) || conflicted(h.Record.Phase, reply) || exhausted(h, reply)) ? Failed : (delivered(h.Record.Phase, reply) ? HandedOver : h.Record.Outcome)))
// @ ensures ok ==> next.Record.Phase == ((deliveryFailed(reply) || conflicted(h.Record.Phase, reply) || exhausted(h, reply) || delivered(h.Record.Phase, reply) || h.Record.Phase == Retention) ? Terminal : (h.Record.Phase == Inspect ? Copying : (h.Record.Phase == Preclean ? Inspect : (h.Record.Phase == Copying ? Verify : (h.Record.Phase == Verify ? (reply == Same ? Publish : Discard) : (h.Record.Phase == Discard ? Copying : (h.Record.Phase == Publish ? InspectTaken : h.Record.Phase)))))))
// @ ensures ok ==> next.Record.Reason == (reply == ReplyFolderGone ? FolderGone : (reply == ReplyFolderFull ? FolderFull : (conflicted(h.Record.Phase, reply) ? NameConflict : (exhausted(h, reply) ? DifferentCopies : h.Record.Reason))))
// @ ensures next.Record.Attempt == h.Record.Attempt
// @ ensures ok ==> next.Record.Round == (h.Record.Phase == Discard && reply == OK && !exhausted(h, reply) ? h.Record.Round + 1 : h.Record.Round)
// @ ensures ok ==> next.Record.Differences == (h.Record.Phase == Verify && reply == Different ? h.Record.Differences + 1 : h.Record.Differences)
// @ ensures ok ==> next.Record.Cleanup == (delivered(h.Record.Phase, reply) ? Waiting : (h.Record.Phase == Retention ? (reply == OK ? CleanupDone : CleanupFailed) : h.Record.Cleanup))
// @ ensures ok ==> next.Record.CleanupReason == (h.Record.Phase == Retention && reply == RemoveFailed ? RemoveFailed : h.Record.CleanupReason)
// @ ensures next.Record.CleanupTry == h.Record.CleanupTry
// @ ensures ok ==> next.Record.Intent == (Request{})
// @ ensures ok ==> next.Record.HiddenVerified == (h.Record.Phase == Verify && reply == Same ? true : ((h.Record.Phase == Preclean || h.Record.Phase == Discard) && reply == OK ? false : h.Record.HiddenVerified))
// @ ensures ok ==> next.Record.NamedVerified == (h.Record.NamedVerified || delivered(h.Record.Phase, reply))
func (h Handover) Consume(running bool, wire Request, done bool, reply Reply) (next Handover, ok bool) {
	next = h
	if !running || h.Record.Intent == (Request{}) || wire != h.Record.Intent || !done || !validReply(h.Record.Phase, reply) {
		return next, false
	}
	phase := h.Record.Phase
	failed := reply == ReplyFolderGone || reply == ReplyFolderFull
	success := ((phase == Inspect || phase == InspectTaken) && reply == Same) || (phase == Publish && reply == OK)
	conflict := (phase == Inspect || phase == InspectTaken) && reply == Different
	exhausted := phase == Discard && reply == OK && h.Record.Differences == h.DifferenceLimit
	if phase == Verify && reply == Different && h.Record.Differences >= CounterLimit {
		return next, false
	}
	if phase == Discard && reply == OK && !exhausted && h.Record.Round >= CounterLimit {
		return next, false
	}
	next.Record.Intent = Request{}
	if phase != Retention {
		if failed || conflict || exhausted {
			next.Record.Outcome = Failed
		} else if success {
			next.Record.Outcome = HandedOver
		}
	}
	if reply == ReplyFolderGone {
		next.Record.Reason = FolderGone
	} else if reply == ReplyFolderFull {
		next.Record.Reason = FolderFull
	} else if conflict {
		next.Record.Reason = NameConflict
	} else if exhausted {
		next.Record.Reason = DifferentCopies
	}
	if failed || conflict || exhausted || success || phase == Retention {
		next.Record.Phase = Terminal
	} else {
		switch phase {
		case Inspect:
			next.Record.Phase = Copying
		case Preclean:
			next.Record.Phase = Inspect
		case Copying:
			next.Record.Phase = Verify
		case Verify:
			next.Record.Phase = Discard
			if reply == Same {
				next.Record.Phase = Publish
			}
		case Discard:
			next.Record.Phase = Copying
		case Publish:
			next.Record.Phase = InspectTaken
		}
	}
	if phase == Verify && reply == Different {
		next.Record.Differences++
	}
	if phase == Discard && reply == OK && !exhausted {
		next.Record.Round++
	}
	if phase == Verify && reply == Same {
		next.Record.HiddenVerified = true
	} else if (phase == Preclean || phase == Discard) && reply == OK {
		next.Record.HiddenVerified = false
	}
	next.Record.NamedVerified = h.Record.NamedVerified || success
	if success {
		next.Record.Cleanup = Waiting
	} else if phase == Retention {
		next.Record.Cleanup = CleanupFailed
		if reply == OK {
			next.Record.Cleanup = CleanupDone
		}
	}
	if phase == Retention && reply == RemoveFailed {
		next.Record.CleanupReason = RemoveFailed
	}
	return next, true
}

// BeginRetention is separate from delivery. elapsed is supplied by the caller's
// retention policy; this core has no clock.
// @ requires true
// @ ensures ok == (running && h.Record.Outcome == HandedOver && h.Record.Cleanup == Waiting && elapsed)
// @ ensures !ok ==> next == h
// @ ensures next.Recording == h.Recording && next.DifferenceLimit == h.DifferenceLimit
// @ ensures next.Record.Outcome == h.Record.Outcome
// @ ensures ok ==> next.Record.Phase == (Retention)
// @ ensures next.Record.Reason == h.Record.Reason
// @ ensures next.Record.Attempt == h.Record.Attempt
// @ ensures next.Record.Round == h.Record.Round
// @ ensures next.Record.Differences == h.Record.Differences
// @ ensures ok ==> next.Record.Cleanup == (Cleaning)
// @ ensures next.Record.CleanupReason == h.Record.CleanupReason
// @ ensures ok ==> next.Record.CleanupTry == (1)
// @ ensures next.Record.Intent == h.Record.Intent
// @ ensures next.Record.HiddenVerified == h.Record.HiddenVerified
// @ ensures next.Record.NamedVerified == h.Record.NamedVerified
func (h Handover) BeginRetention(running, elapsed bool) (next Handover, ok bool) {
	next = h
	if !running || h.Record.Outcome != HandedOver || h.Record.Cleanup != Waiting || !elapsed {
		return next, false
	}
	next.Record.Cleanup = Cleaning
	next.Record.CleanupTry = 1
	next.Record.Phase = Retention
	return next, true
}

// @ requires true
// @ ensures ok == (running && h.Record.Cleanup == CleanupFailed && h.Record.CleanupTry < CounterLimit)
// @ ensures !ok ==> next == h
// @ ensures next.Recording == h.Recording && next.DifferenceLimit == h.DifferenceLimit
// @ ensures next.Record.Outcome == h.Record.Outcome
// @ ensures ok ==> next.Record.Phase == (Retention)
// @ ensures next.Record.Reason == h.Record.Reason
// @ ensures next.Record.Attempt == h.Record.Attempt
// @ ensures next.Record.Round == h.Record.Round
// @ ensures next.Record.Differences == h.Record.Differences
// @ ensures ok ==> next.Record.Cleanup == (Cleaning)
// @ ensures ok ==> next.Record.CleanupReason == (NoReply)
// @ ensures ok ==> next.Record.CleanupTry == (h.Record.CleanupTry + 1)
// @ ensures next.Record.Intent == h.Record.Intent
// @ ensures next.Record.HiddenVerified == h.Record.HiddenVerified
// @ ensures next.Record.NamedVerified == h.Record.NamedVerified
func (h Handover) RetryCleanup(running bool) (next Handover, ok bool) {
	next = h
	if !running || h.Record.Cleanup != CleanupFailed || h.Record.CleanupTry >= CounterLimit {
		return next, false
	}
	next.Record.Cleanup = Cleaning
	next.Record.CleanupReason = NoReply
	next.Record.CleanupTry++
	next.Record.Phase = Retention
	return next, true
}
