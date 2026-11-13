package handover

// These constants belong to the finite environment, not to the implementation.
// MaxDifferences is also the configured policy parameter, listed in the manifest.
const (
	MaxAttempts        = 2
	MaxCleanupAttempts = 2
	MaxDifferences     = 2
	Recording          = "recording.wav"
	OtherRecording     = "unrelated.wav"
)

type content int

const (
	contentAbsent content = iota
	contentPartial
	contentSame
	contentDifferent
)

type callerState int

const (
	callerIdle callerState = iota
	callerBusy
	callerDone
)

// State is comparable. Core is copied from the implementation; all other fields
// are environment state or history, and are never restored into Handover.
type State struct {
	Core                Record
	Wire                Request
	Caller              callerState
	Reply               Reply
	Execution           Request
	Hidden              content
	Named               content
	ForeignContent      content
	InitialNamed        content
	PublicationVerified bool
	Publications        int
	Copies              int
	ActiveCopies        int
	Spool               bool
	KeepElapsed         bool
	Running             bool
	Replayed            bool
}

func Init() State {
	h, ok := New(Recording, MaxDifferences)
	if !ok {
		panic("invalid exploration configuration")
	}
	return State{Core: h.Record, Spool: true, Running: true}
}

// coreResult copies the core's output, never recomputing a system transition.
func coreResult(s State, next Handover, ok bool) State {
	if ok {
		s.Core = next.Record
	}
	return s
}

func Try(s State, tried func(step string, args []any, next State)) {
	// Value receivers keep every attempt independent, even after a refusal.
	h := Handover{Recording: Recording, DifferenceLimit: MaxDifferences, Record: s.Core}

	nextCore, ok := h.Start(s.Running)
	tried("Start", nil, coreResult(s, nextCore, ok))

	// These are environment retry budgets. The implementation itself permits
	// further retries; unlike MaxDifferences, neither is a core parameter.
	nextCore, ok = h.RetryDelivery(s.Running, s.Hidden == contentAbsent)
	if !ok || nextCore.Record.Attempt <= MaxAttempts {
		tried("RetryDelivery", nil, coreResult(s, nextCore, ok))
	}

	nextCore, ok = h.Issue(s.Running)
	next := coreResult(s, nextCore, ok)
	if ok {
		next.Wire = nextCore.Record.Intent
	}
	tried("Issue", nil, next)

	request, sent := h.Resend(s.Running, s.Wire)
	next = s
	if sent {
		next.Wire = request
		next.Replayed = s.Replayed || (s.Core.Intent.Kind == Rename && s.Reply == OK)
	}
	tried("Resend", nil, next)

	nextCore, ok = h.Consume(s.Running, s.Wire, s.Caller == callerDone, s.Reply)
	next = coreResult(s, nextCore, ok)
	if ok {
		next.Wire = Request{}
		next.Caller = callerIdle
		next.Reply = NoReply
		next.Execution = Request{}
	}
	tried("Consume", nil, next)

	nextCore, ok = h.BeginRetention(s.Running, s.KeepElapsed)
	tried("BeginRetention", nil, coreResult(s, nextCore, ok))

	next = s
	if s.Wire.Kind == Copy && s.Caller == callerIdle {
		next.Caller = callerBusy
		next.Hidden = contentPartial
		next.ActiveCopies = 1
		next.Copies++
		next.Execution = s.Wire
	}
	tried("BeginCopy", nil, next)

	for _, result := range []Reply{OK, ReplyFolderGone, ReplyFolderFull} {
		for _, contents := range []content{contentSame, contentDifferent, contentPartial} {
			next = s
			allowed := (result == OK && (contents == contentSame || contents == contentDifferent)) ||
				(result != OK && contents == contentPartial)
			if s.Caller == callerBusy && allowed {
				next.Reply = result
				next.Hidden = contents
				next.Caller = callerDone
				next.ActiveCopies = 0
			}
			tried("FinishCopy", []any{replyString(result), contentString(contents)}, next)
		}
	}

	for result := NoReply; result <= RemoveFailed; result++ {
		next = s
		if s.Wire != (Request{}) && s.Wire.Kind != Copy && s.Caller == callerIdle && possibleResult(s, result) {
			next.Caller = callerDone
			next.Reply = result
			next.Execution = s.Wire
			if result == OK {
				if s.Wire.Kind == Rename || s.Wire.Kind == RemoveHidden {
					next.Hidden = contentAbsent
				}
				if s.Wire.Kind == Rename {
					next.Named = s.Hidden
					next.Publications++
					next.PublicationVerified = s.Core.HiddenVerified
				}
				if s.Wire.Kind == RemoveSpool {
					next.Spool = false
				}
			}
		}
		tried("ExecuteOther", []any{replyString(result)}, next)
	}

	nextCore, ok = h.RetryCleanup(s.Running)
	if !ok || nextCore.Record.CleanupTry <= MaxCleanupAttempts {
		tried("RetryCleanup", nil, coreResult(s, nextCore, ok))
	}

	for _, contents := range []content{contentSame, contentDifferent} {
		next = s
		if s.Core.Outcome == Active && s.Named == contentAbsent {
			next.Named = contents
			next.ForeignContent = contents
		}
		tried("NameAppears", []any{contentString(contents)}, next)
	}

	for _, contents := range []content{contentSame, contentDifferent} {
		next = s
		if s.Core.Outcome == NotStarted && s.Named == contentAbsent {
			next.Named = contents
			next.ForeignContent = contents
			next.InitialNamed = contents
		}
		tried("InitialFile", []any{contentString(contents)}, next)
	}

	next = s
	next.KeepElapsed = true
	tried("RetentionElapsed", nil, next)

	next = s
	if s.Running {
		next.Running = false
		next.Wire = Request{}
	}
	tried("Crash", nil, next)

	next = s
	next.Running = true
	tried("Resume", nil, next)
	tried("Wait", nil, s)
}

// possibleResult is solely the caller/environment model. No implementation
// decision about advancing the hand-over is made here.
func possibleResult(s State, result Reply) bool {
	folderFailure := result == ReplyFolderGone || result == ReplyFolderFull
	switch s.Wire.Kind {
	case CompareNamed:
		return result == comparison(s.Named) || folderFailure
	case CompareHidden:
		return result == comparison(s.Hidden) || folderFailure
	case Rename:
		return folderFailure || (s.Named == contentAbsent && result == OK) || (s.Named != contentAbsent && result == Taken)
	case RemoveHidden:
		return result == OK || folderFailure
	case RemoveSpool:
		return result == OK || result == RemoveFailed
	default:
		return false
	}
}

func comparison(c content) Reply {
	switch c {
	case contentAbsent:
		return Absent
	case contentSame:
		return Same
	case contentDifferent:
		return Different
	default:
		// Partial contents are not in Replies and cannot answer a comparison.
		return Reply(-1)
	}
}

func contentString(c content) string {
	return [...]string{"absent", "partial", "same", "different"}[c]
}

func phaseString(p Phase) string {
	return [...]string{"idle", "inspect", "inspectTaken", "preclean", "copy", "verify", "discard", "publish", "retention", "terminal"}[p]
}

func replyString(r Reply) string {
	return [...]string{"none", "ok", "absent", "same", "different", "taken", "folderGone", "folderFull", "removeFailed"}[r]
}

func abstractRequest(r Request) map[string]any {
	directory, prefix := "folder", "."
	if r.Target == FinalTarget {
		prefix = ""
	} else if r.Target == SpoolTarget {
		directory, prefix = "spool", ""
	}
	return map[string]any{
		"kind":   [...]string{"none", "compareNamed", "copy", "compareHidden", "publish", "removeHidden", "removeSpool"}[r.Kind],
		"target": map[string]any{"directory": directory, "prefix": prefix, "name": Recording},
		"token":  map[string]any{"$seq": []any{r.ID.Attempt, r.ID.Round, phaseString(r.ID.Phase), r.ID.CleanupTry}},
	}
}

func Abstract(s State) map[string]any {
	return map[string]any{
		"outcome":             [...]string{"notStarted", "active", "handedOver", "failed"}[s.Core.Outcome],
		"phase":               phaseString(s.Core.Phase),
		"reason":              [...]string{"none", "folderGone", "folderFull", "nameConflict", "differentCopies"}[s.Core.Reason],
		"attempt":             s.Core.Attempt,
		"round":               s.Core.Round,
		"differences":         s.Core.Differences,
		"cleanup":             [...]string{"dormant", "waiting", "active", "failed", "done"}[s.Core.Cleanup],
		"cleanupReason":       replyString(s.Core.CleanupReason),
		"cleanupTry":          s.Core.CleanupTry,
		"intent":              abstractRequest(s.Core.Intent),
		"wire":                abstractRequest(s.Wire),
		"caller":              [...]string{"idle", "busy", "done"}[s.Caller],
		"reply":               replyString(s.Reply),
		"execution":           abstractRequest(s.Execution),
		"hidden":              contentString(s.Hidden),
		"named":               contentString(s.Named),
		"foreignContent":      contentString(s.ForeignContent),
		"initialNamed":        contentString(s.InitialNamed),
		"hiddenVerified":      s.Core.HiddenVerified,
		"namedVerified":       s.Core.NamedVerified,
		"publicationVerified": s.PublicationVerified,
		"publications":        s.Publications,
		"copies":              s.Copies,
		"activeCopies":        s.ActiveCopies,
		"spool":               s.Spool,
		"keepElapsed":         s.KeepElapsed,
		"running":             s.Running,
		"replayed":            s.Replayed,
	}
}
