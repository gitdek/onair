// +gobra

package session

// New allocates an identity space and an independent, configurable retry limit.
// @ requires 0 <= capacity && capacity <= MaxSize
// @ requires 0 <= retryLimit && retryLimit < MaxSize
// @ ensures acc(&s.Records) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == capacity && len(s.Queue) == capacity
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j]) && s.Records[j] == Record{}
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j]) && s.Queue[j] == 0
// @ ensures s.Control == Control{Online: true, RetryLimit: retryLimit}
func New(capacity, retryLimit int) (s *Session) {
	return &Session{Records: make([]Record, capacity), Queue: make([]int, capacity), Control: Control{Online: true, RetryLimit: retryLimit}}
}

// runningIn is the index of a running record, startup and the wait for a
// restart included, or -1 when none is running. It takes the slice by value
// and each record whole, and gives the records back unchanged.
// @ requires len(records) <= MaxSize
// @ requires forall j int :: { &records[j] } 0 <= j && j < len(records) ==> acc(&records[j])
// @ ensures forall j int :: { &records[j] } 0 <= j && j < len(records) ==> acc(&records[j]) && records[j] == old(records[j])
// @ ensures -1 <= k && k < len(records)
// @ ensures 0 <= k ==> IsRunning(records[k])
// @ ensures k < 0 ==> forall j int :: { &records[j] } 0 <= j && j < len(records) ==> !IsRunning(records[j])
func runningIn(records []Record) (k int) {
	i := 0
	// @ invariant forall j int :: { &records[j] } 0 <= j && j < len(records) ==> acc(&records[j]) && records[j] == old(records[j])
	// @ invariant 0 <= i && i <= len(records) && len(records) <= MaxSize
	// @ invariant forall j int :: { &records[j] } 0 <= j && j < i ==> !IsRunning(records[j])
	// @ decreases len(records)-i
	for i < len(records) {
		if IsRunning(records[i]) {
			return i
		}
		i++
	}
	return -1
}

// Start accepts a fresh caller identity whenever capture is not running.
// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ requires 0 < id && id <= len(s.Records)
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> &s.Queue[j] == old(&s.Queue[j])
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures ok == old(s.Control.Online && s.Records[id-1].Phase == Absent && (forall j int :: 0 <= j && j < len(s.Records) ==> !IsRunning(s.Records[j])))
// @ ensures s.Control.Online == old(s.Control.Online)
// @ ensures s.Control.Finishing == old(s.Control.Finishing)
// @ ensures s.Control.Notice == old(s.Control.Notice)
// @ ensures s.Control.Count == old(s.Control.Count)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == (ok && j == id-1 ? FreshRecord() : old(s.Records[j]))
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == old(s.Queue[j])
// @ ensures !ok ==> s.Control.Capture == old(s.Control.Capture)
// @ ensures ok ==> s.Control.Capture == Capture{ID: id, Mode: StartMode, Target: id}
func (s *Session) Start(id int) (ok bool) {
	if !s.Control.Online || s.Records[id-1].Phase != Absent {
		return false
	}
	k := runningIn(s.Records)
	if k >= 0 {
		// @ assert IsRunning(old(s.Records[k]))
		return false
	}
	// @ assert forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> !IsRunning(old(s.Records[j]))
	s.Records[id-1] = FreshRecord()
	s.Control.Capture = Capture{ID: id, Mode: StartMode, Target: id}
	return true
}

// RefuseStart is the refused branch of Start, emitting a one-shot menu effect.
// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ requires 0 < id && id <= len(s.Records)
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> &s.Queue[j] == old(&s.Queue[j])
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures ok == old(s.Control.Online && s.Records[id-1].Phase == Absent && !(forall j int :: 0 <= j && j < len(s.Records) ==> !IsRunning(s.Records[j])))
// @ ensures s.Control.Online == old(s.Control.Online)
// @ ensures s.Control.Capture == old(s.Control.Capture)
// @ ensures s.Control.Finishing == old(s.Control.Finishing)
// @ ensures s.Control.Notice == old(s.Control.Notice)
// @ ensures s.Control.Count == old(s.Control.Count)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == old(s.Records[j])
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == old(s.Queue[j])
// @ ensures menu == (ok ? Menu{Kind: RefusedMenu, ID: id} : Menu{})
func (s *Session) RefuseStart(id int) (menu Menu, ok bool) {
	if !s.Control.Online || s.Records[id-1].Phase != Absent {
		return Menu{}, false
	}
	k := runningIn(s.Records)
	if k < 0 {
		// @ assert forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> !IsRunning(old(s.Records[j]))
		return Menu{}, false
	}
	// @ assert IsRunning(old(s.Records[k]))
	return Menu{Kind: RefusedMenu, ID: id}, true
}

// Stop latches a person/Quit stop (id zero), or an automatic stop of id.
// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ requires 0 <= id && id <= len(s.Records)
// @ requires 0 <= reason
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> &s.Queue[j] == old(&s.Queue[j])
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures ok == old(s.Control.Online)
// @ ensures s.Control.Online == old(s.Control.Online)
// @ ensures s.Control.Capture == old(s.Control.Capture)
// @ ensures s.Control.Finishing == old(s.Control.Finishing)
// @ ensures s.Control.Notice == old(s.Control.Notice)
// @ ensures s.Control.Count == old(s.Control.Count)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == (ok && (id == 0 || id == j+1) && IsRunning(old(s.Records[j])) ? WithStop(old(s.Records[j])) : old(s.Records[j]))
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == old(s.Queue[j])
// @ ensures menu == (ok && id > 0 && reason > 0 && IsRunning(old(s.Records[id-1])) ? Menu{Kind: AutomaticStopMenu, ID: id, Reason: reason} : Menu{})
func (s *Session) Stop(id, reason int) (menu Menu, ok bool) {
	if !s.Control.Online {
		return Menu{}, false
	}
	if id > 0 && reason > 0 && IsRunning(s.Records[id-1]) {
		menu = Menu{Kind: AutomaticStopMenu, ID: id, Reason: reason}
	}
	s.saveStops(id)
	return menu, true
}

// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ requires 0 < id && id <= len(s.Records)
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> &s.Queue[j] == old(&s.Queue[j])
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures ok == old(s.Control.Online && s.Control.Capture.ID == id && s.Control.Capture.Mode == StartMode)
// @ ensures s.Control.Online == old(s.Control.Online)
// @ ensures s.Control.Finishing == old(s.Control.Finishing)
// @ ensures s.Control.Notice == old(s.Control.Notice)
// @ ensures s.Control.Count == old(s.Control.Count)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == (ok && j == id-1 ? WithPhase(old(s.Records[j]), Live) : old(s.Records[j]))
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == old(s.Queue[j])
// @ ensures !ok ==> s.Control.Capture == old(s.Control.Capture)
// @ ensures ok ==> s.Control.Capture == WithMode(old(s.Control.Capture), old(s.Records[id-1].Stopped) ? StopMode : LiveMode)
// @ ensures menu == (ok && old(s.Records[id-1].Retries) > 0 ? Menu{Kind: GapMenu, ID: id} : Menu{})
func (s *Session) CaptureStarted(id int) (menu Menu, ok bool) {
	if !s.Control.Online || s.Control.Capture.ID != id || s.Control.Capture.Mode != StartMode {
		return Menu{}, false
	}
	r := s.Records[id-1]
	s.Records[id-1] = WithPhase(r, Live)
	mode := LiveMode
	if r.Stopped {
		mode = StopMode
	}
	s.Control.Capture = WithMode(s.Control.Capture, mode)
	if r.Retries > 0 {
		menu = Menu{Kind: GapMenu, ID: id}
	}
	return menu, true
}

// closeCapture commits a permanent capture end; callers already know capture is dead.
// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ requires 0 < id && id <= len(s.Records)
// @ requires s.Control.Count < len(s.Queue)
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> &s.Queue[j] == old(&s.Queue[j])
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures s.Control.Online == old(s.Control.Online)
// @ ensures s.Control.Capture == old(s.Control.Capture)
// @ ensures s.Control.Finishing == old(s.Control.Finishing)
// @ ensures s.Control.Notice == old(s.Control.Notice)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == (j == id-1 ? QueuedRecord(cause) : old(s.Records[j]))
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == (j == old(s.Control.Count) ? id : old(s.Queue[j]))
// @ ensures s.Control.Count == old(s.Control.Count)+1
func (s *Session) closeCapture(id, cause int) {
	s.Records[id-1] = QueuedRecord(cause)
	s.Queue[s.Control.Count] = id
	s.Control.Count++
}

// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ requires 0 < id && id <= len(s.Records)
// @ requires reason > 0
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> &s.Queue[j] == old(&s.Queue[j])
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures ok == old(s.Control.Online && s.Control.Capture.ID == id && s.Control.Capture.Mode == StartMode && s.Control.Count < len(s.Queue))
// @ ensures s.Control.Online == old(s.Control.Online)
// @ ensures s.Control.Finishing == old(s.Control.Finishing)
// @ ensures s.Control.Notice == old(s.Control.Notice)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == (ok && j == id-1 ? QueuedRecord(reason) : old(s.Records[j]))
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == (ok && j == old(s.Control.Count) ? id : old(s.Queue[j]))
// @ ensures !ok ==> s.Control.Capture == old(s.Control.Capture) && s.Control.Count == old(s.Control.Count)
// @ ensures ok ==> s.Control.Capture == Capture{} && s.Control.Count == old(s.Control.Count)+1
func (s *Session) CaptureFailed(id, reason int) (ok bool) {
	if !s.Control.Online || s.Control.Capture.ID != id || s.Control.Capture.Mode != StartMode || s.Control.Count == len(s.Queue) {
		return false
	}
	s.closeCapture(id, reason)
	s.Control.Capture = Capture{}
	return true
}

// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ requires 0 < id && id <= len(s.Records)
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> &s.Queue[j] == old(&s.Queue[j])
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures ok == old(s.Control.Online && IsRunning(s.Records[id-1]) && s.Records[id-1].Stopped && s.Control.Capture.ID == id && s.Control.Capture.Mode == LiveMode)
// @ ensures s.Control.Online == old(s.Control.Online)
// @ ensures s.Control.Finishing == old(s.Control.Finishing)
// @ ensures s.Control.Notice == old(s.Control.Notice)
// @ ensures s.Control.Count == old(s.Control.Count)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == old(s.Records[j])
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == old(s.Queue[j])
// @ ensures !ok ==> s.Control.Capture == old(s.Control.Capture)
// @ ensures ok ==> s.Control.Capture == WithMode(old(s.Control.Capture), StopMode)
func (s *Session) RequestStop(id int) (ok bool) {
	if !s.Control.Online || !IsRunning(s.Records[id-1]) || !s.Records[id-1].Stopped || s.Control.Capture.ID != id || s.Control.Capture.Mode != LiveMode {
		return false
	}
	s.Control.Capture = WithMode(s.Control.Capture, StopMode)
	return true
}

// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ requires 0 < id && id <= len(s.Records)
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> &s.Queue[j] == old(&s.Queue[j])
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures ok == old(s.Control.Online && s.Control.Capture.ID == id && (s.Control.Capture.Mode == LiveMode || s.Control.Capture.Mode == StopMode) && (!s.Records[id-1].Stopped || s.Control.Count < len(s.Queue)))
// @ ensures s.Control.Online == old(s.Control.Online)
// @ ensures s.Control.Finishing == old(s.Control.Finishing)
// @ ensures s.Control.Notice == old(s.Control.Notice)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == (ok && j == id-1 ? (old(s.Records[j].Stopped) ? QueuedRecord(StopCause) : WithPhase(old(s.Records[j]), Retry)) : old(s.Records[j]))
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == (ok && old(s.Records[id-1].Stopped) && j == old(s.Control.Count) ? id : old(s.Queue[j]))
// @ ensures !ok ==> s.Control.Capture == old(s.Control.Capture) && s.Control.Count == old(s.Control.Count)
// @ ensures ok ==> s.Control.Capture == Capture{}
// @ ensures s.Control.Count == old(s.Control.Count) + (ok && old(s.Records[id-1].Stopped) ? 1 : 0)
func (s *Session) CaptureEnded(id int) (ok bool) {
	if !s.Control.Online || s.Control.Capture.ID != id || (s.Control.Capture.Mode != LiveMode && s.Control.Capture.Mode != StopMode) {
		return false
	}
	r := s.Records[id-1]
	if r.Stopped {
		if s.Control.Count == len(s.Queue) {
			return false
		}
		s.closeCapture(id, StopCause)
	} else {
		s.Records[id-1] = WithPhase(r, Retry)
	}
	s.Control.Capture = Capture{}
	return true
}

// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ requires 0 < id && id <= len(s.Records)
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> &s.Queue[j] == old(&s.Queue[j])
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures ok == old(s.Control.Online && s.Records[id-1].Phase == Retry && 0 <= s.Records[id-1].Retries && s.Records[id-1].Retries <= s.Control.RetryLimit && (!(s.Records[id-1].Stopped || s.Records[id-1].Retries == s.Control.RetryLimit) || s.Control.Count < len(s.Queue)))
// @ ensures s.Control.Online == old(s.Control.Online)
// @ ensures s.Control.Finishing == old(s.Control.Finishing)
// @ ensures s.Control.Notice == old(s.Control.Notice)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == (ok && j == id-1 ? (old((s.Records[id-1].Stopped || s.Records[id-1].Retries == s.Control.RetryLimit)) ? QueuedRecord(old(s.Records[j].Stopped) ? StopCause : RetryLimitCause) : RestartedRecord(old(s.Records[j]))) : old(s.Records[j]))
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == (ok && old((s.Records[id-1].Stopped || s.Records[id-1].Retries == s.Control.RetryLimit)) && j == old(s.Control.Count) ? id : old(s.Queue[j]))
// @ ensures !ok ==> s.Control.Capture == old(s.Control.Capture) && s.Control.Count == old(s.Control.Count)
// @ ensures s.Control.Count == old(s.Control.Count) + (ok && old((s.Records[id-1].Stopped || s.Records[id-1].Retries == s.Control.RetryLimit)) ? 1 : 0)
// @ ensures ok && old((s.Records[id-1].Stopped || s.Records[id-1].Retries == s.Control.RetryLimit)) ==> s.Control.Capture == old(s.Control.Capture)
// @ ensures ok && !old((s.Records[id-1].Stopped || s.Records[id-1].Retries == s.Control.RetryLimit)) ==> s.Control.Capture == Capture{ID: id, Mode: StartMode, Attempt: old(s.Records[id-1].Retries)+1, Target: id}
func (s *Session) ResumeOrClose(id int) (ok bool) {
	if !s.Control.Online || s.Records[id-1].Phase != Retry {
		return false
	}
	r := s.Records[id-1]
	if r.Retries < 0 || r.Retries > s.Control.RetryLimit {
		return false
	}
	if r.Stopped || r.Retries == s.Control.RetryLimit {
		if s.Control.Count == len(s.Queue) {
			return false
		}
		cause := RetryLimitCause
		if r.Stopped {
			cause = StopCause
		}
		s.closeCapture(id, cause)
	} else {
		s.Records[id-1] = RestartedRecord(r)
		s.Control.Capture = Capture{ID: id, Mode: StartMode, Attempt: r.Retries + 1, Target: id}
	}
	return true
}

// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> &s.Queue[j] == old(&s.Queue[j])
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures ok == old(s.Control.Online && s.Control.Finishing == 0 && s.Control.Count > 0)
// @ ensures s.Control.Online == old(s.Control.Online)
// @ ensures s.Control.Capture == old(s.Control.Capture)
// @ ensures s.Control.Notice == old(s.Control.Notice)
// @ ensures s.Control.Count == old(s.Control.Count)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == old(s.Records[j])
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == old(s.Queue[j])
// @ ensures !ok ==> s.Control.Finishing == old(s.Control.Finishing)
// @ ensures ok ==> s.Control.Finishing == old(s.Queue[0])
func (s *Session) RequestFinish() (ok bool) {
	if !s.Control.Online || s.Control.Finishing != 0 || s.Control.Count == 0 {
		return false
	}
	s.Control.Finishing = s.Queue[0]
	return true
}

// FinishReported saves the exact result; an empty report must name the closure cause.
// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ requires 0 < id && id <= len(s.Records)
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures ok == old(s.Control.Online && s.Control.Finishing == id && s.Control.Count > 0 && s.Queue[0] == id && s.Records[id-1].Phase == QueuedPhase && ValidResult(v) && (!v.Empty || v.Why == s.Records[id-1].Cause))
// @ ensures s.Control.Online == old(s.Control.Online)
// @ ensures s.Control.Capture == old(s.Control.Capture)
// @ ensures s.Control.Notice == old(s.Control.Notice)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == (ok && j == id-1 ? EndedRecord(v) : old(s.Records[j]))
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == (ok ? (j < old(s.Control.Count)-1 ? old(s.Queue[j+1]) : 0) : old(s.Queue[j]))
// @ ensures !ok ==> s.Control.Count == old(s.Control.Count) && s.Control.Finishing == old(s.Control.Finishing)
// @ ensures ok ==> s.Control.Count == old(s.Control.Count)-1 && s.Control.Finishing == 0
func (s *Session) FinishReported(id int, v Result) (ok bool) {
	if !s.Control.Online || s.Control.Finishing != id || s.Control.Count == 0 || s.Queue[0] != id || s.Records[id-1].Phase != QueuedPhase || !ValidResult(v) || (v.Empty && v.Why != s.Records[id-1].Cause) {
		return false
	}
	s.popQueue()
	s.Control.Finishing = 0
	s.Records[id-1] = EndedRecord(v)
	return true
}

// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ requires 0 < id && id <= len(s.Records)
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> &s.Queue[j] == old(&s.Queue[j])
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures ok == old(s.Control.Online && s.Records[id-1].Phase == EndedPhase && s.Control.Notice.ID == 0)
// @ ensures s.Control.Online == old(s.Control.Online)
// @ ensures s.Control.Capture == old(s.Control.Capture)
// @ ensures s.Control.Finishing == old(s.Control.Finishing)
// @ ensures s.Control.Count == old(s.Control.Count)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == old(s.Records[j])
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == old(s.Queue[j])
// @ ensures !ok ==> s.Control.Notice == old(s.Control.Notice)
// @ ensures ok ==> s.Control.Notice == EndNotice{ID: id, Result: old(s.Records[id-1].Result), Directory: id}
func (s *Session) RequestEndNotice(id int) (ok bool) {
	if !s.Control.Online || s.Records[id-1].Phase != EndedPhase || s.Control.Notice.ID != 0 {
		return false
	}
	s.Control.Notice = EndNotice{ID: id, Result: s.Records[id-1].Result, Directory: id}
	return true
}

// MenuSaid removes a recording only after the caller confirms actual display.
// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ requires 0 < id && id <= len(s.Records)
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> &s.Queue[j] == old(&s.Queue[j])
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures ok == old(s.Control.Online && s.Control.Notice.ID == id)
// @ ensures s.Control.Online == old(s.Control.Online)
// @ ensures s.Control.Capture == old(s.Control.Capture)
// @ ensures s.Control.Finishing == old(s.Control.Finishing)
// @ ensures s.Control.Count == old(s.Control.Count)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == (ok && j == id-1 ? Record{} : old(s.Records[j]))
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == old(s.Queue[j])
// @ ensures !ok ==> s.Control.Notice == old(s.Control.Notice)
// @ ensures ok ==> s.Control.Notice == EndNotice{}
func (s *Session) MenuSaid(id int) (ok bool) {
	if !s.Control.Online || s.Control.Notice.ID != id {
		return false
	}
	s.Records[id-1] = Record{}
	s.Control.Notice = EndNotice{}
	return true
}

// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> &s.Queue[j] == old(&s.Queue[j])
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures ok == old(s.Control.Online)
// @ ensures s.Control.Count == old(s.Control.Count)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == old(s.Records[j])
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == old(s.Queue[j])
// @ ensures !ok ==> s.Control.Online == old(s.Control.Online) && s.Control.Capture == old(s.Control.Capture) && s.Control.Finishing == old(s.Control.Finishing) && s.Control.Notice == old(s.Control.Notice)
// @ ensures ok ==> !s.Control.Online && s.Control.Capture == Capture{} && s.Control.Finishing == 0 && s.Control.Notice == EndNotice{}
func (s *Session) Crash() (ok bool) {
	if !s.Control.Online {
		return false
	}
	s.Control.Online = false
	s.Control.Capture = Capture{}
	s.Control.Finishing = 0
	s.Control.Notice = EndNotice{}
	return true
}

// Launch keeps counted interrupted restarts and saved Stops intact.
// @ requires acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ requires 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ requires 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures acc(&s.Records, 1/2) && acc(&s.Queue) && acc(&s.Control)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> &s.Queue[j] == old(&s.Queue[j])
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures len(s.Records) == len(s.Queue) && len(s.Records) <= MaxSize
// @ ensures 0 <= s.Control.Count && s.Control.Count <= len(s.Queue)
// @ ensures 0 <= s.Control.RetryLimit && s.Control.RetryLimit < MaxSize
// @ ensures ok == old(!s.Control.Online)
// @ ensures s.Control.Capture == old(s.Control.Capture)
// @ ensures s.Control.Finishing == old(s.Control.Finishing)
// @ ensures s.Control.Notice == old(s.Control.Notice)
// @ ensures s.Control.Count == old(s.Control.Count)
// @ ensures s.Control.RetryLimit == old(s.Control.RetryLimit)
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == (ok && IsRunning(old(s.Records[j])) ? WithPhase(old(s.Records[j]), Retry) : old(s.Records[j]))
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == old(s.Queue[j])
// @ ensures !ok ==> s.Control.Online == old(s.Control.Online)
// @ ensures ok ==> s.Control.Online
func (s *Session) Launch() (ok bool) {
	if s.Control.Online {
		return false
	}
	s.prepareRecovery()
	s.Control.Online = true
	return true
}

// popQueue removes the FIFO head into fresh storage. Source and destination
// are disjoint, so writing a new cell leaves every old cell as it was, and the
// shift needs no induction over the rest of the session state. Unused cells
// keep make's zero.
// @ requires acc(&s.Queue) && acc(&s.Control.Count)
// @ requires forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ requires 0 < s.Control.Count && s.Control.Count <= len(s.Queue) && len(s.Queue) <= MaxSize
// @ ensures acc(&s.Queue) && acc(&s.Control.Count)
// @ ensures len(s.Queue) == old(len(s.Queue))
// @ ensures forall j int :: { &s.Queue[j] } 0 <= j && j < len(s.Queue) ==> acc(&s.Queue[j])
// @ ensures s.Control.Count == old(s.Control.Count)-1
// @ ensures forall j int :: { s.Queue[j] } 0 <= j && j < len(s.Queue) ==> s.Queue[j] == (j < old(s.Control.Count)-1 ? old(s.Queue[j+1]) : 0)
func (s *Session) popQueue() {
	previous := s.Queue
	n := s.Control.Count
	next := make([]int, len(previous))
	i := 0
	// @ invariant forall j int :: { &previous[j] } 0 <= j && j < len(previous) ==> acc(&previous[j], 1/2)
	// @ invariant forall j int :: { &next[j] } 0 <= j && j < len(next) ==> acc(&next[j])
	// @ invariant len(next) == len(previous) && 0 < n && n <= len(previous)
	// @ invariant 0 <= i && i <= n-1
	// @ invariant forall j int :: { previous[j] } 0 <= j && j < len(previous) ==> previous[j] == old(s.Queue[j])
	// @ invariant forall j int :: { next[j] } 0 <= j && j < len(next) ==> next[j] == (j < i ? previous[j+1] : 0)
	// @ decreases n-1-i
	for i < n-1 {
		next[i] = previous[i+1]
		i++
	}
	s.Queue = next
	s.Control.Count = n - 1
}

// saveStops transforms saved records without access to requests or the FIFO.
// @ requires acc(&s.Records, 1/2)
// @ requires len(s.Records) <= MaxSize
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ requires 0 <= id && id <= len(s.Records)
// @ ensures acc(&s.Records, 1/2)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == ((id == 0 || id == j+1) && IsRunning(old(s.Records[j])) ? WithStop(old(s.Records[j])) : old(s.Records[j]))
func (s *Session) saveStops(id int) {
	i := 0
	// @ invariant acc(&s.Records, 1/2)
	// @ invariant len(s.Records) == old(len(s.Records)) && len(s.Records) <= MaxSize
	// @ invariant forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
	// @ invariant forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
	// @ invariant 0 <= i && i <= len(s.Records)
	// @ invariant forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == (j < i && (id == 0 || id == j+1) && IsRunning(old(s.Records[j])) ? WithStop(old(s.Records[j])) : old(s.Records[j]))
	// @ decreases len(s.Records)-i
	for i < len(s.Records) {
		if (id == 0 || id == i+1) && IsRunning(s.Records[i]) {
			s.Records[i] = WithStop(s.Records[i])
		}
		i++
	}
}

// prepareRecovery transforms saved records without access to requests or the FIFO.
// @ requires acc(&s.Records, 1/2)
// @ requires len(s.Records) <= MaxSize
// @ requires forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures acc(&s.Records, 1/2)
// @ ensures len(s.Records) == old(len(s.Records))
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
// @ ensures forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == (IsRunning(old(s.Records[j])) ? WithPhase(old(s.Records[j]), Retry) : old(s.Records[j]))
func (s *Session) prepareRecovery() {
	i := 0
	// @ invariant acc(&s.Records, 1/2)
	// @ invariant len(s.Records) == old(len(s.Records)) && len(s.Records) <= MaxSize
	// @ invariant forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> &s.Records[j] == old(&s.Records[j])
	// @ invariant forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> acc(&s.Records[j])
	// @ invariant 0 <= i && i <= len(s.Records)
	// @ invariant forall j int :: { &s.Records[j] } 0 <= j && j < len(s.Records) ==> s.Records[j] == (j < i && IsRunning(old(s.Records[j])) ? WithPhase(old(s.Records[j]), Retry) : old(s.Records[j]))
	// @ decreases len(s.Records)-i
	for i < len(s.Records) {
		if IsRunning(s.Records[i]) {
			s.Records[i] = WithPhase(s.Records[i], Retry)
		}
		i++
	}
}
