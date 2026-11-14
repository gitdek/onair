package session

import "strconv"

// Only the environment is finite. Retry policy is also a production parameter.
const (
	Recordings  = 2
	Errors      = 1
	MaxRestarts = 2
)

const (
	eNone = iota
	eAccepted
	eRefused
	eStop
	eRestart
	eStarted
	eFinish
	eCrash
	eLaunch
	eCaptureEnded
	eFailed
)

// Compact, comparable encodings of exactly the model's variables. Zero cells
// are absent map entries or sequence padding, not additional history.
type resultState struct {
	Outcome, Code uint8
	Why           int8
	Empty         bool
}
type recordState struct {
	Phase   uint8
	Stopped bool
	Retries uint8
	Cause   int8
	Result  resultState
}
type captureState struct{ ID, Mode, Attempt, Target uint8 }
type noticeState struct {
	ID        uint8
	Result    resultState
	Directory uint8
}
type closureState struct {
	ID    uint8
	Cause int8
}
type eventState struct {
	Kind, ID    uint8
	Before      [Recordings]recordState
	BeforeQueue [Recordings]uint8
	Running     [Recordings]bool
	Retry, Menu uint8
	Reason      int8
}

type State struct {
	Records     [Recordings]recordState
	Queue       [Recordings]uint8
	Online      bool
	Capture     captureState
	Finishing   uint8
	Notice      noticeState
	Attempts    [Recordings]int8
	StopAt      [Recordings]int8
	Ends        [Recordings]resultState
	Shown       [Recordings]bool
	ClosedOrder [Recordings]closureState
	Event       eventState
}

func packResult(v Result) resultState {
	return resultState{uint8(v.Outcome), uint8(v.Code), int8(v.Why), v.Empty}
}
func unpackResult(v resultState) Result {
	return Result{int(v.Outcome), int(v.Code), int(v.Why), v.Empty}
}
func packRecord(r Record) recordState {
	return recordState{uint8(r.Phase), r.Stopped, uint8(r.Retries), int8(r.Cause), packResult(r.Result)}
}
func unpackRecord(r recordState) Record {
	return Record{int(r.Phase), r.Stopped, int(r.Retries), int(r.Cause), unpackResult(r.Result)}
}

// load restores only the implementation's operational state. Histories never
// enter the implementation, and each attempted operation gets independent slices.
func load(s State) *Session {
	c := New(Recordings, MaxRestarts)
	for i, r := range s.Records {
		c.Records[i] = unpackRecord(r)
	}
	for i, id := range s.Queue {
		c.Queue[i] = int(id)
		if id != 0 {
			c.Control.Count++
		}
	}
	c.Control.Online = s.Online
	c.Control.Capture = Capture{int(s.Capture.ID), int(s.Capture.Mode), int(s.Capture.Attempt), int(s.Capture.Target)}
	c.Control.Finishing = int(s.Finishing)
	c.Control.Notice = EndNotice{int(s.Notice.ID), unpackResult(s.Notice.Result), int(s.Notice.Directory)}
	return c
}

func read(s State, c *Session) State {
	for i, r := range c.Records {
		s.Records[i] = packRecord(r)
	}
	for i := range s.Queue {
		s.Queue[i] = 0
		if i < c.Control.Count {
			s.Queue[i] = uint8(c.Queue[i])
		}
	}
	s.Online = c.Control.Online
	v := c.Control.Capture
	s.Capture = captureState{uint8(v.ID), uint8(v.Mode), uint8(v.Attempt), uint8(v.Target)}
	s.Finishing = uint8(c.Control.Finishing)
	n := c.Control.Notice
	s.Notice = noticeState{uint8(n.ID), packResult(n.Result), uint8(n.Directory)}
	return s
}

func Init() State {
	s := read(State{}, New(Recordings, MaxRestarts))
	for i := range s.StopAt {
		s.StopAt[i] = -1
	}
	return s
}

func running(s State) (set [Recordings]bool) {
	for i, r := range s.Records {
		set[i] = r.Phase == Starting || r.Phase == Live || r.Phase == Retry
	}
	return
}

func closed(s *State, id int, cause int8) {
	for i := range s.ClosedOrder {
		if s.ClosedOrder[i].ID == 0 {
			s.ClosedOrder[i] = closureState{uint8(id), cause}
			break
		}
	}
	s.Attempts[id-1] = 1
	if s.StopAt[id-1] >= 0 {
		s.StopAt[id-1] = 1
	} else {
		s.StopAt[id-1] = -1
	}
}

// attempt calls the real operation, then observes its result. Only identity
// freshness is a caller precondition: the caller must never reuse an accepted
// identity, even after its record has been acknowledged and removed.
func attempt(s State, step string, id, reason int, v Result) State {
	c := load(s)
	var menu Menu
	var ok bool
	switch step {
	case "Start":
		if s.Attempts[id-1] == 0 {
			ok = c.Start(id)
		}
	case "RefuseStart":
		if s.Attempts[id-1] == 0 {
			menu, ok = c.RefuseStart(id)
		}
	case "Stop":
		menu, ok = c.Stop(id, reason)
	case "CaptureStarted":
		menu, ok = c.CaptureStarted(id)
	case "CaptureFailed":
		ok = c.CaptureFailed(id, reason)
	case "RequestStop":
		ok = c.RequestStop(id)
	case "CaptureEnded":
		ok = c.CaptureEnded(id)
	case "ResumeOrClose":
		ok = c.ResumeOrClose(id)
	case "RequestFinish":
		ok = c.RequestFinish()
	case "FinishReported":
		ok = c.FinishReported(id, v)
	case "RequestEndNotice":
		ok = c.RequestEndNotice(id)
	case "MenuSaid":
		ok = c.MenuSaid(id)
	case "Crash":
		ok = c.Crash()
	case "Launch":
		ok = c.Launch()
	case "Idle":
		return s
	default:
		panic("unknown model step")
	}
	if !ok {
		return s
	}
	n := read(s, c)
	n.Event = eventState{}
	switch step {
	case "Start":
		n.Attempts[id-1] = 1
		n.Event = eventState{Kind: eAccepted, ID: uint8(id), Running: running(s)}
	case "RefuseStart":
		n.Event = eventState{Kind: eRefused, ID: uint8(id), Before: s.Records, BeforeQueue: s.Queue, Running: running(s), Menu: uint8(menu.Kind)}
	case "Stop":
		rs := running(s)
		target := id
		if id == 0 {
			for i, yes := range rs {
				if yes {
					target = i + 1
					break
				}
			}
		}
		if target != 0 && rs[target-1] && s.StopAt[target-1] < 0 {
			n.StopAt[target-1] = s.Attempts[target-1]
		}
		n.Event = eventState{Kind: eStop, ID: uint8(target), Before: s.Records, BeforeQueue: s.Queue, Running: rs, Reason: int8(reason), Menu: uint8(menu.Kind)}
	case "CaptureStarted":
		n.Event = eventState{Kind: eStarted, ID: uint8(id), Retry: s.Records[id-1].Retries, Menu: uint8(menu.Kind)}
	case "CaptureFailed":
		closed(&n, id, int8(reason))
		n.Event = eventState{Kind: eFailed, ID: uint8(id), Reason: int8(reason)}
	case "CaptureEnded":
		if s.Records[id-1].Stopped {
			closed(&n, id, StopCause)
		}
		n.Event = eventState{Kind: eCaptureEnded, ID: uint8(id), Before: s.Records, Retry: s.Records[id-1].Retries}
	case "ResumeOrClose":
		if n.Records[id-1].Phase == QueuedPhase {
			closed(&n, id, n.Records[id-1].Cause)
		} else {
			n.Attempts[id-1]++
			n.Event = eventState{Kind: eRestart, ID: uint8(id), Retry: s.Records[id-1].Retries}
			if s.Event.Kind == eLaunch {
				n.Event.Before = s.Event.Before
			}
		}
	case "RequestFinish":
		n.Event = eventState{Kind: eFinish, ID: n.Finishing}
	case "FinishReported":
		n.Ends[id-1] = packResult(v)
	case "MenuSaid":
		n.Shown[id-1] = true
	case "Crash":
		n.Event = eventState{Kind: eCrash, Before: s.Records, BeforeQueue: s.Queue}
	case "Launch":
		n.Event = eventState{Kind: eLaunch, Before: s.Records, Running: running(s)}
	}
	return n
}

func results() []Result {
	vs := []Result{{Outcome: Handed}}
	for e := 1; e <= Errors; e++ {
		vs = append(vs, Result{Outcome: Handed, Code: Failure, Why: e}, Result{Outcome: Kept, Code: Failure, Why: e})
	}
	for _, cause := range []int{StopCause, RetryLimitCause} {
		vs = append(vs, Result{Outcome: Kept, Code: NothingRecordedCode, Why: cause, Empty: true})
	}
	for e := 1; e <= Errors; e++ {
		vs = append(vs, Result{Outcome: Kept, Code: NothingRecordedCode, Why: e, Empty: true})
	}
	return vs
}

// Try reports all syntactic instances of Next, including disabled operations.
// A mismatched empty-result cause is deliberately passed to the implementation.
func Try(s State, tried func(step string, args []any, next State)) {
	for id := 1; id <= Recordings; id++ {
		for _, step := range []string{"Start", "RefuseStart", "CaptureStarted", "RequestStop", "CaptureEnded", "ResumeOrClose", "RequestEndNotice", "MenuSaid"} {
			tried(step, []any{recordingValue(id)}, attempt(s, step, id, 0, Result{}))
		}
		for e := 1; e <= Errors; e++ {
			for _, step := range []string{"Stop", "CaptureFailed"} {
				tried(step, []any{recordingValue(id), reasonValue(e)}, attempt(s, step, id, e, Result{}))
			}
		}
		for _, v := range results() {
			tried("FinishReported", []any{recordingValue(id), abstractResult(packResult(v))}, attempt(s, "FinishReported", id, 0, v))
		}
	}
	tried("Stop", []any{"none", "none"}, attempt(s, "Stop", 0, 0, Result{}))
	for _, step := range []string{"RequestFinish", "Crash", "Launch", "Idle"} {
		tried(step, []any{}, attempt(s, step, 0, 0, Result{}))
	}
}

func recordingValue(id int) any {
	if id == 0 {
		return "none"
	}
	return map[string]any{"$mv": "r" + strconv.Itoa(id)}
}

func reasonValue(e int) any {
	switch e {
	case 0:
		return "none"
	case StopCause:
		return "stop"
	case RetryLimitCause:
		return "retryLimit"
	case 1:
		return map[string]any{"$mv": "fault"}
	default:
		return map[string]any{"$mv": "fault" + strconv.Itoa(e)}
	}
}

func abstractResult(v resultState) map[string]any {
	outcomes := []string{"none", "handed", "kept"}
	codes := []string{"none", "failure", "nothingRecorded"}
	return map[string]any{"outcome": outcomes[v.Outcome], "error": map[string]any{"code": codes[v.Code], "why": reasonValue(int(v.Why))}, "empty": v.Empty}
}

func abstractRecords(rs [Recordings]recordState) map[string]any {
	pairs := []any{}
	phases := []string{"none", "starting", "live", "retry", "queued", "ended"}
	for i, r := range rs {
		if r.Phase == Absent {
			continue
		}
		pairs = append(pairs, []any{recordingValue(i + 1), map[string]any{"phase": phases[r.Phase], "stopped": r.Stopped, "retries": int(r.Retries), "cause": reasonValue(int(r.Cause)), "result": abstractResult(r.Result)}})
	}
	return map[string]any{"$fn": pairs}
}

func abstractQueue(q [Recordings]uint8) map[string]any {
	values := []any{}
	for _, id := range q {
		if id != 0 {
			values = append(values, recordingValue(int(id)))
		}
	}
	return map[string]any{"$seq": values}
}

func abstractSet(set [Recordings]bool) map[string]any {
	values := []any{}
	for i, yes := range set {
		if yes {
			values = append(values, recordingValue(i+1))
		}
	}
	return map[string]any{"$set": values}
}

func Abstract(s State) map[string]any {
	attempts, stopAt, ends, order := []any{}, []any{}, []any{}, []any{}
	for i := 0; i < Recordings; i++ {
		key := recordingValue(i + 1)
		attempts = append(attempts, []any{key, int(s.Attempts[i])})
		stopAt = append(stopAt, []any{key, int(s.StopAt[i])})
		values := []any{}
		if s.Ends[i].Outcome != NoOutcome {
			values = append(values, abstractResult(s.Ends[i]))
		}
		ends = append(ends, []any{key, map[string]any{"$seq": values}})
	}
	for _, c := range s.ClosedOrder {
		if c.ID != 0 {
			order = append(order, map[string]any{"id": recordingValue(int(c.ID)), "cause": reasonValue(int(c.Cause))})
		}
	}
	finishing := []any{}
	if s.Finishing != 0 {
		finishing = append(finishing, recordingValue(int(s.Finishing)))
	}
	c, n, e := s.Capture, s.Notice, s.Event
	modes := []string{"none", "start", "live", "stop"}
	kinds := []string{"none", "accepted", "refused", "stop", "restart", "started", "finish", "crash", "launch", "captureEnded", "failed"}
	menus := []string{"none", "refused", "gap", "automaticStop"}
	return map[string]any{
		"records": abstractRecords(s.Records), "queue": abstractQueue(s.Queue), "online": s.Online,
		"capture":   map[string]any{"id": recordingValue(int(c.ID)), "mode": modes[c.Mode], "attempt": int(c.Attempt), "target": recordingValue(int(c.Target))},
		"finishing": map[string]any{"$set": finishing},
		"notice":    map[string]any{"id": recordingValue(int(n.ID)), "result": abstractResult(n.Result), "directory": recordingValue(int(n.Directory))},
		"attempts":  map[string]any{"$fn": attempts}, "stopAt": map[string]any{"$fn": stopAt}, "ends": map[string]any{"$fn": ends},
		"shown": abstractSet(s.Shown), "closedOrder": map[string]any{"$seq": order},
		"event": map[string]any{"kind": kinds[e.Kind], "id": recordingValue(int(e.ID)), "before": abstractRecords(e.Before), "beforeQueue": abstractQueue(e.BeforeQueue), "running": abstractSet(e.Running), "retry": int(e.Retry), "menu": menus[e.Menu], "reason": reasonValue(int(e.Reason))},
	}
}
