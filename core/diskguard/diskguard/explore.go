package diskguard

// Only the explorer supplies finite model bounds. The three safety thresholds
// are constructor parameters, identified as such in invariant.json.
const (
	StartBytes    = 20
	ReserveBytes  = 10
	MaxPieceBytes = 2
	MaxFreeBytes  = 22
)

// State has exactly the model's seven variables. Command and Reason are read
// back from Guard; Result is its latest returned decision. All other fields
// are caller state or proof history, never stored by the production guard.
type State struct {
	Phase                 Phase
	Free                  int64
	Result                Decision
	StopCount             int
	Command               Command
	Reason                Reason
	FinishedAfterDiskStop bool
}

func emptyResult() Decision {
	return Decision{Purpose: NoPurpose, Reading: FailedReading, NoticeFree: NoReading}
}

// Init constructs the live guard and the model's initial environment.
func Init() State {
	g, ok := New(StartBytes, ReserveBytes, MaxPieceBytes)
	if !ok {
		panic("invalid exploration parameters")
	}
	s := State{Phase: Idle, Result: emptyResult()}
	return snapshot(s, g)
}

func restore(s State) *Guard {
	g, ok := New(StartBytes, ReserveBytes, MaxPieceBytes)
	if !ok {
		panic("invalid exploration parameters")
	}
	g.command, g.reason = s.Command, s.Reason
	return g
}

func snapshot(s State, g *Guard) State {
	s.Command, s.Reason = g.RequestedStop()
	return s
}

// record counts each emitted command; assigning a latch value to StopCount
// would conceal the duplicate-stop bug that this history is meant to detect.
func record(s State, d Decision) State {
	s.Result = d
	if d.EmitsStop {
		s.StopCount++
	}
	return s
}

func suppliedReading(free int64, ok bool) int64 {
	if !ok {
		return FailedReading
	}
	return free
}

func suppliedFree(r int64) int64 {
	if r == FailedReading {
		return 0
	}
	return r
}

// Try attempts every action and every argument in Next, including disabled
// actions. Guard operations themselves reject inapplicable caller phases.
// Environment-only actions report the unchanged state when they cannot run.
func Try(s State, tried func(step string, args []any, next State)) {
	for r := FailedReading; r <= MaxFreeBytes; r++ {
		g := restore(s)
		d, ran := g.CheckStart(s.Phase, r)
		next := snapshot(s, g)
		if ran {
			next = record(next, d)
			next.Phase = Idle
			if d.Allowed {
				next.Phase = Ready
			}
			next.Free = suppliedFree(r)
			next.FinishedAfterDiskStop = false
		}
		tried("CheckStart", []any{r}, next)

		g = restore(s)
		d, ran = g.RecoverRecording(s.Phase, r)
		next = snapshot(s, g)
		if ran {
			next = record(next, d)
			next.Phase = FinishExisting
			if d.Allowed {
				next.Phase = Ready
			}
			next.Free = suppliedFree(r)
			next.FinishedAfterDiskStop = false
		}
		tried("RecoverRecording", []any{r}, next)
	}

	g := restore(s)
	ran := g.CaptureStarted(s.Phase)
	next := snapshot(s, g)
	if ran {
		next.Phase = Running
	}
	tried("CaptureStarted", nil, next)

	// A closed ordinary piece already has an open successor. Both interrupted
	// and caller-stopped capture can therefore charge another bounded piece
	// while the caller is still waiting to deliver its space reading.
	for n := int64(0); n <= MaxPieceBytes; n++ {
		next = s
		if s.Phase == Running {
			next.Free -= n
			next.Phase = Reading
		}
		tried("ClosePiece", []any{n}, next)

		next = s
		if s.Phase == Stopping {
			next.Free -= n
			next.Phase = FinalReading
		}
		tried("CloseFinalPiece", []any{n}, next)

		next = s
		if s.Phase == Running || s.Phase == Reading {
			next.Free -= n
			next.Phase = Drained
		}
		tried("CallerEndsCapture", []any{n}, next)

		next = s
		if s.Phase == Running || s.Phase == Reading {
			next.Free -= n
			next.Phase = Suspended
		}
		tried("CaptureInterrupted", []any{n}, next)
	}

	for _, ok := range []bool{false, true} {
		r := suppliedReading(s.Free, ok)
		g = restore(s)
		d, ran := g.ReportPiece(s.Phase, r)
		next = snapshot(s, g)
		if ran {
			next = record(next, d)
			next.Phase = Stopping
			if d.Allowed {
				next.Phase = Running
			}
		}
		tried("ReportPiece", []any{ok}, next)

		g = restore(s)
		d, ran = g.ReportFinalPiece(s.Phase, r)
		next = snapshot(s, g)
		if ran {
			next = record(next, d)
			next.Phase = Drained
		}
		tried("ReportFinalPiece", []any{ok}, next)

		g = restore(s)
		d, ran = g.ResumeCapture(s.Phase, r)
		next = snapshot(s, g)
		if ran {
			next = record(next, d)
			next.Phase = FinishExisting
			if d.Allowed {
				next.Phase = Ready
			}
		}
		tried("ResumeCapture", []any{ok}, next)

		g = restore(s)
		d, ran = g.RetryResumeAfterStop(s.Phase, r)
		next = snapshot(s, g)
		if ran {
			next = record(next, d)
		}
		tried("RetryResumeAfterStop", []any{ok}, next)
	}

	g = restore(s)
	ran = g.CaptureEndedForGood(s.Phase)
	next = snapshot(s, g)
	if ran {
		next.Phase = Idle
		next.Result = emptyResult()
		next.StopCount = 0
		next.FinishedAfterDiskStop = s.StopCount > 0 && s.Command == StopCommand &&
			(s.Phase == FinalReading || s.Phase == Drained)
	}
	tried("CaptureEndedForGood", nil, next)
	tried("Crash", nil, Init())
	tried("WaitForCaller", nil, s)
}

// Abstract encodes only model variables; records are JSON objects and each
// scalar uses its TLA+ spelling. This model needs no set or sequence wrappers.
func Abstract(s State) map[string]any {
	phases := [...]string{"idle", "ready", "running", "reading", "suspended",
		"stopping", "final-reading", "drained", "finish-existing"}
	purposes := [...]string{"none", "start", "resume", "piece"}
	commands := [...]string{"none", "Stop"}
	reasons := [...]string{"none", "disk nearly full"}
	return map[string]any{
		"phase": phases[s.Phase],
		"free":  s.Free,
		"result": map[string]any{
			"purpose":    purposes[s.Result.Purpose],
			"reading":    s.Result.Reading,
			"allowed":    s.Result.Allowed,
			"wasStopped": s.Result.WasStopped,
			"emitsStop":  s.Result.EmitsStop,
			"noticeFree": s.Result.NoticeFree,
			"noticeNeed": s.Result.NoticeNeed,
		},
		"stopCount":             s.StopCount,
		"command":               commands[s.Command],
		"reason":                reasons[s.Reason],
		"finishedAfterDiskStop": s.FinishedAfterDiskStop,
	}
}
