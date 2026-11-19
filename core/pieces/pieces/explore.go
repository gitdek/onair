package pieces

// These are exploration bounds, not implementation limits. Durations is the
// size of the modeled set {2, 3, ...}; the other two are constructor parameters.
const (
	MaxPieces         = 2
	InterruptionLimit = 2
	Durations         = 2
)

const (
	RecordingId = "recording-1"
	SpoolPath   = "spool/recording-1"
)

const (
	absent uint8 = iota
	hidden
	named
)

// State has one representation of each model variable. Fixed arrays encode
// functions and sets over Pieces. JoinInput is zero-padded, with nonzero entries
// forming its sequence. Only this explorer owns effects and verification history.
type State struct {
	Online               bool
	Phase                string
	Capture              string
	Open                 int
	Disk                 [MaxPieces]uint8
	Durable              [MaxPieces]bool
	Contents             [MaxPieces]int
	Durations            [MaxPieces]int
	Known                [MaxPieces]bool
	Sealed               [MaxPieces]bool
	CaptureFault         bool
	JoinInput            [MaxPieces]int
	Partial              bool
	Prepared             bool
	Omitted              [MaxPieces]bool
	Gaps                 [MaxPieces]int
	Joined               bool
	FileOmitted          [MaxPieces]bool
	Interruptions        int
	InterruptionEvidence int
	HandoverKey          string
	HandoverTarget       int
	Delivered            [MaxPieces + 1]bool
	HandedOver           bool
	RetentionElapsed     bool
	Error                string
	ErrorAt              string
	RetryPending         bool
	RecoveryKind         string
	RecoveryReply        string
	NextNumber           int
	FlushReported        bool
	RecoveredAtLimit     bool
}

func Init() State {
	s := State{Online: true, RecoveryKind: "none", RecoveryReply: "none"}
	return readSystem(s, New(MaxPieces, InterruptionLimit, RecordingId, SpoolPath))
}

func restoreSystem(s State) *Recording {
	b := New(MaxPieces, InterruptionLimit, s.HandoverKey, SpoolPath)
	b.Meta = Metadata{
		Phase: s.Phase, Capture: s.Capture, Open: s.Open,
		NextNumber: s.NextNumber, FlushReported: s.FlushReported,
		CaptureFault: s.CaptureFault, Prepared: s.Prepared,
		Interruptions: s.Interruptions, HandedOver: s.HandedOver,
		RetentionElapsed: s.RetentionElapsed, Error: s.Error,
		ErrorAt: s.ErrorAt, RetryPending: s.RetryPending,
	}
	for _, p := range s.JoinInput {
		if p != 0 {
			b.Meta.JoinCount++
		}
	}
	for i := range b.Pieces {
		b.Pieces[i] = Piece{Duration: s.Durations[i], Known: s.Known[i], Omitted: s.Omitted[i], Gap: s.Gaps[i]}
	}
	return b
}

func readSystem(s State, b *Recording) State {
	m := b.Meta
	s.Phase, s.Capture, s.Open = m.Phase, m.Capture, m.Open
	s.NextNumber, s.FlushReported = m.NextNumber, m.FlushReported
	s.CaptureFault, s.Prepared = m.CaptureFault, m.Prepared
	s.Interruptions, s.HandedOver = m.Interruptions, m.HandedOver
	s.RetentionElapsed, s.Error, s.ErrorAt = m.RetentionElapsed, m.Error, m.ErrorAt
	s.RetryPending, s.HandoverKey = m.RetryPending, b.ID
	// The API's hand-over operation identifies only the joined recording.
	s.HandoverTarget = 0
	s.JoinInput = [MaxPieces]int{}
	for i := 0; i < m.JoinCount; i++ {
		s.JoinInput[i] = i + 1
	}
	for i, p := range b.Pieces {
		s.Durations[i], s.Known[i], s.Omitted[i], s.Gaps[i] = p.Duration, p.Known, p.Omitted, p.Gap
	}
	return s
}

func (s State) openHasName() bool {
	return s.Open > 0 && s.Open <= MaxPieces && s.Disk[s.Open-1] == named
}

func (s State) openIsDurable() bool {
	return s.Open > 0 && s.Open <= MaxPieces && s.Durable[s.Open-1]
}

func (s State) openIsAbsent() bool {
	return s.Open > 0 && s.Open <= MaxPieces && s.Disk[s.Open-1] == absent
}

func (s State) noNames() bool {
	for _, d := range s.Disk {
		if d == named {
			return false
		}
	}
	return true
}

// Try makes every attempt, including refusals. Only running/stopped status is
// controlled here for system operations. The bool report arguments describe
// truthful caller results; the code checks all its own scheduling guards.
func Try(s State, tried func(step string, args []any, next State)) {
	system := func(step string, args []any, call func(*Recording) bool) {
		b := restoreSystem(s)
		ran := call(b)
		n := s
		if s.Online && ran {
			n = readSystem(s, b)
		}
		tried(step, args, n)
	}
	environment := func(step string, args []any, effect func(*State)) {
		n := s
		effect(&n)
		tried(step, args, n)
	}

	for i := 0; i < Durations; i++ {
		seconds := i + 2
		system("AcceptPiece", []any{seconds}, func(b *Recording) bool { return b.AcceptPiece(seconds) })
	}
	environment("WriteHiddenBytes", nil, func(n *State) {
		if s.Online && s.Phase == "Write" && s.openIsAbsent() {
			n.Disk[s.Open-1], n.Contents[s.Open-1] = hidden, 1
		}
	})
	environment("FlushPiece", nil, func(n *State) {
		if s.Online && s.Phase == "Write" && s.Open > 0 && s.Disk[s.Open-1] == hidden && !s.openIsDurable() {
			n.Durable[s.Open-1] = true
		}
	})
	system("ReportPieceFlushed", nil, func(b *Recording) bool { return b.ReportPieceFlushed(s.openIsDurable()) })
	environment("NamePiece", nil, func(n *State) {
		// This effect does not protect the system from a premature name request.
		if s.Online && s.Phase == "Name" && s.Open > 0 && s.Disk[s.Open-1] == hidden {
			n.Disk[s.Open-1], n.Sealed[s.Open-1] = named, true
		}
	})
	system("ReportPieceNamed", nil, func(b *Recording) bool { return b.ReportPieceNamed(s.openHasName()) })
	system("ReportWriteFailure", nil, func(b *Recording) bool { return b.ReportWriteFailure(s.openHasName()) })
	environment("DiscardOpenRemnant", nil, func(n *State) {
		if s.Online && s.Phase == "DiscardPiece" && s.Open > 0 && s.Disk[s.Open-1] == hidden {
			n.Disk[s.Open-1], n.Durable[s.Open-1], n.Contents[s.Open-1] = absent, false, 0
		}
	})
	system("ReportRemnantDiscarded", nil, func(b *Recording) bool { return b.ReportRemnantDiscarded(s.openIsAbsent()) })
	system("CaptureEnded", nil, (*Recording).CaptureEnded)
	system("CarryOn", nil, (*Recording).CarryOn)
	system("FinishCapture", nil, (*Recording).FinishCapture)
	system("FinalizeCapture", nil, (*Recording).FinalizeCapture)
	system("RequestJoin", nil, (*Recording).RequestJoin)
	environment("BeginJoinOutput", nil, func(n *State) {
		if s.Online && s.Phase == "Join" && !s.Partial && !s.Joined {
			n.Partial = true
		}
	})
	for bits := 0; bits < 1<<MaxPieces; bits++ {
		missing := make([]bool, MaxPieces)
		for i := range missing {
			missing[i] = bits&(1<<i) != 0
		}
		system("ReportJoinPrepared", []any{abstractSet(missing, 1)}, func(b *Recording) bool {
			return b.ReportJoinPrepared(missing, s.Partial && !s.Joined)
		})
	}
	environment("NameJoinedFile", nil, func(n *State) {
		if s.Online && s.Phase == "PublishJoin" && s.Partial && s.Prepared && !s.Joined {
			n.Joined, n.Partial, n.FileOmitted = true, false, s.Omitted
		}
	})
	system("ReportJoinNamed", nil, func(b *Recording) bool { return b.ReportJoinNamed(s.Joined) })
	for _, reason := range []string{"join failed", "none readable"} {
		system("ReportJoinFailure", []any{reason}, func(b *Recording) bool {
			return b.ReportJoinFailure(reason, s.Joined, s.Partial)
		})
	}
	system("AskRetry", nil, (*Recording).AskRetry)
	system("ScheduleRetry", nil, (*Recording).ScheduleRetry)
	environment("DiscardPartialJoin", nil, func(n *State) {
		if s.Online && (s.Phase == "DiscardJoinRetry" || s.Phase == "DiscardJoinFailed") && s.Partial {
			n.Partial = false
		}
	})
	system("ReportPartialJoinDiscarded", nil, func(b *Recording) bool { return b.ReportPartialJoinDiscarded(!s.Partial) })
	system("RequestHandover", nil, (*Recording).RequestHandover)
	environment("DeliverJoinedFile", nil, func(n *State) {
		if s.Online && s.Phase == "Handover" && s.Joined {
			n.Delivered[s.HandoverTarget] = true
		}
	})
	delivered := s.Delivered[0]
	for i := 1; i < len(s.Delivered); i++ {
		delivered = delivered && !s.Delivered[i]
	}
	system("ReportHandoverSuccess", nil, func(b *Recording) bool { return b.ReportHandoverSuccess(delivered) })
	system("ReportHandoverFailure", nil, (*Recording).ReportHandoverFailure)
	system("ReportThreeDaysElapsed", nil, (*Recording).ReportThreeDaysElapsed)
	system("RequestRemoval", nil, (*Recording).RequestRemoval)
	for p := 1; p <= MaxPieces; p++ {
		environment("RemovePiece", []any{p}, func(n *State) {
			if s.Online && s.Phase == "Remove" && s.HandedOver && s.RetentionElapsed && s.Disk[p-1] == named {
				n.Disk[p-1], n.Durable[p-1], n.Contents[p-1] = absent, false, 0
			}
		})
	}
	system("ReportRemovalComplete", nil, func(b *Recording) bool { return b.ReportRemovalComplete(s.noNames()) })
	environment("Crash", nil, func(n *State) {
		if s.Online {
			n.Online = false
			if (s.Phase == "Join" || s.Phase == "PublishJoin") && !s.Joined && s.InterruptionEvidence < InterruptionLimit {
				n.InterruptionEvidence++
			}
		}
	})
	// Launch is the sole inventory input. Its history describes the input to
	// recovery and the actual phase returned by the implementation.
	b := restoreSystem(s)
	listing := make([]bool, MaxPieces)
	for i := range listing {
		listing[i] = s.Disk[i] == named
	}
	ran := b.Recover(listing, s.Joined)
	n := s
	if !s.Online && ran {
		n = readSystem(s, b)
		n.Online = true
		lost := (s.Phase == "Join" || s.Phase == "PublishJoin") && !s.Joined
		count := s.Interruptions
		if lost && count < InterruptionLimit {
			count++
		}
		n.RecoveredAtLimit = lost && count >= InterruptionLimit
		n.RecoveryKind = "none"
		switch s.Phase {
		case "Write", "Name", "DiscardPiece":
			if s.openHasName() {
				n.RecoveryKind = "named piece"
			}
		case "Join", "PublishJoin":
			if s.Joined {
				n.RecoveryKind = "named join"
			} else if count >= InterruptionLimit {
				n.RecoveryKind = "interruption limit"
			} else {
				n.RecoveryKind = "interrupted join"
			}
		case "JoinFailed", "DiscardJoinFailed":
			n.RecoveryKind = "failed join"
		case "Handover":
			n.RecoveryKind = "handover"
		case "HandoverFailed":
			n.RecoveryKind = "failed handover"
		}
		n.RecoveryReply = "none"
		if n.RecoveryKind != "none" {
			n.RecoveryReply = n.Phase
		}
	}
	tried("Launch", nil, n)
	tried("Finished", nil, s)
}

func abstractSet(bits []bool, first int) map[string]any {
	items := make([]any, 0, len(bits))
	for i, yes := range bits {
		if yes {
			items = append(items, i+first)
		}
	}
	return map[string]any{"$set": items}
}

func abstractFunction(n int, value func(int) any) map[string]any {
	pairs := make([]any, n)
	for i := range pairs {
		pairs[i] = []any{i + 1, value(i)}
	}
	return map[string]any{"$fn": pairs}
}

func Abstract(s State) map[string]any {
	join := make([]any, 0, MaxPieces)
	for _, p := range s.JoinInput {
		if p != 0 {
			join = append(join, p)
		}
	}
	return map[string]any{
		"online": s.Online, "phase": s.Phase, "capture": s.Capture, "open": s.Open,
		"disk":      abstractFunction(MaxPieces, func(i int) any { return []string{"absent", "hidden", "named"}[s.Disk[i]] }),
		"durable":   abstractSet(s.Durable[:], 1),
		"contents":  abstractFunction(MaxPieces, func(i int) any { return s.Contents[i] }),
		"durations": abstractFunction(MaxPieces, func(i int) any { return s.Durations[i] }),
		"known":     abstractSet(s.Known[:], 1), "sealed": abstractSet(s.Sealed[:], 1),
		"captureFault": s.CaptureFault, "joinInput": map[string]any{"$seq": join},
		"partial": s.Partial, "prepared": s.Prepared, "omitted": abstractSet(s.Omitted[:], 1),
		"gaps":   abstractFunction(MaxPieces, func(i int) any { return s.Gaps[i] }),
		"joined": s.Joined, "fileOmitted": abstractSet(s.FileOmitted[:], 1),
		"interruptions": s.Interruptions, "interruptionEvidence": s.InterruptionEvidence,
		"handoverKey": s.HandoverKey, "handoverTarget": s.HandoverTarget,
		"delivered": abstractSet(s.Delivered[:], 0), "handedOver": s.HandedOver,
		"retentionElapsed": s.RetentionElapsed, "error": s.Error, "errorAt": s.ErrorAt,
		"retryPending": s.RetryPending, "recoveryKind": s.RecoveryKind, "recoveryReply": s.RecoveryReply,
		"nextNumber": s.NextNumber, "flushReported": s.FlushReported, "recoveredAtLimit": s.RecoveredAtLimit,
	}
}
