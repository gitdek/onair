// +gobra

package pieces

// @ ghost
// @ decreases
// @ pure func recoveryPhase(phase string, openNamed, joined, atLimit bool) string {
// @     return phase == "Write" || phase == "Name" || phase == "DiscardPiece" ? (openNamed ? "Capture" : "DiscardPiece") :
// @            phase == "Join" || phase == "PublishJoin" ? (joined ? "HandoverReady" : (atLimit ? "DiscardJoinFailed" : "DiscardJoinRetry")) :
// @            phase == "JoinFailed" || phase == "DiscardJoinFailed" ? "DiscardJoinRetry" :
// @            phase == "Handover" || phase == "HandoverFailed" ? "HandoverReady" : phase
// @ }

// Recover reconciles a restored record with the authoritative launch listing.
// Call exactly once at launch, before resuming effects. A named joined file
// commits the saved gap manifest; an unnamed attempt increments the saturated
// interruption count. Cleanup precedes any retry or reuse of an open number.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ requires len(named) == len(b.Pieces)
// @ requires forall i int :: { &named[i] } 0 <= i && i < len(named) ==> acc(&named[i], 1/2)
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(true)
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == recoveryPhase(old(b.Meta.Phase), 1 <= old(b.Meta.Open) && old(b.Meta.Open) <= len(named) && named[old(b.Meta.Open)-1], joined, (((old(b.Meta.Phase) == "Join" || old(b.Meta.Phase) == "PublishJoin") && !joined) && old(b.Meta.Interruptions) < old(b.Limit) ? old(b.Meta.Interruptions)+1 : old(b.Meta.Interruptions)) >= old(b.Limit))
// @ ensures ok ==> b.Meta.Capture == (old(b.Meta.Capture) == "active" ? "paused" : old(b.Meta.Capture))
// @ ensures ok ==> b.Meta.Open == (1 <= old(b.Meta.Open) && old(b.Meta.Open) <= len(named) && named[old(b.Meta.Open)-1] ? 0 : old(b.Meta.Open))
// @ ensures ok ==> b.Meta.NextNumber == old(namedPrefix(seq(named), len(named)))+1
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures ok ==> b.Meta.Prepared == (((old(b.Meta.Phase) == "Join" || old(b.Meta.Phase) == "PublishJoin") && !joined) ? false : old(b.Meta.Prepared))
// @ ensures ok ==> b.Meta.Interruptions == (((old(b.Meta.Phase) == "Join" || old(b.Meta.Phase) == "PublishJoin") && !joined) && old(b.Meta.Interruptions) < old(b.Limit) ? old(b.Meta.Interruptions)+1 : old(b.Meta.Interruptions))
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures ok ==> b.Meta.Error == (((old(b.Meta.Phase) == "Join" || old(b.Meta.Phase) == "PublishJoin") && !joined) && (((old(b.Meta.Phase) == "Join" || old(b.Meta.Phase) == "PublishJoin") && !joined) && old(b.Meta.Interruptions) < old(b.Limit) ? old(b.Meta.Interruptions)+1 : old(b.Meta.Interruptions)) >= old(b.Limit) ? "interruption limit" : old(b.Meta.Error))
// @ ensures ok ==> b.Meta.ErrorAt == (((old(b.Meta.Phase) == "Join" || old(b.Meta.Phase) == "PublishJoin") && !joined) && (((old(b.Meta.Phase) == "Join" || old(b.Meta.Phase) == "PublishJoin") && !joined) && old(b.Meta.Interruptions) < old(b.Limit) ? old(b.Meta.Interruptions)+1 : old(b.Meta.Interruptions)) >= old(b.Limit) ? old(b.Spool) : old(b.Meta.ErrorAt))
// @ ensures ok ==> b.Meta.RetryPending == false
// @ ensures !ok ==> forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
// @ ensures ok ==> forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> (b.Pieces[i].Known == named[i] && b.Pieces[i].Duration == old(b.Pieces[i].Duration) && b.Pieces[i].Omitted == (((old(b.Meta.Phase) == "Join" || old(b.Meta.Phase) == "PublishJoin") && !joined) ? false : old(b.Pieces[i].Omitted)) && b.Pieces[i].Gap == (((old(b.Meta.Phase) == "Join" || old(b.Meta.Phase) == "PublishJoin") && !joined) ? 0 : old(b.Pieces[i].Gap)))
// @ ensures forall i int :: { &named[i] } 0 <= i && i < len(named) ==> acc(&named[i], 1/2)
// @ ensures forall i int :: { named[i] } 0 <= i && i < len(named) ==> named[i] == old(named[i])
func (b *Recording) Recover(named []bool, joined bool) (ok bool) {
	lost := (b.Meta.Phase == "Join" || b.Meta.Phase == "PublishJoin") && !joined
	openNamed := false
	if 1 <= b.Meta.Open && b.Meta.Open <= len(named) {
		openNamed = named[b.Meta.Open-1]
	}
	count := reconcileInventory(b.Pieces, named, lost)
	b.Meta.NextNumber = count + 1
	b.Meta.RetryPending = false
	if b.Meta.Capture == "active" {
		b.Meta.Capture = "paused"
	}
	if openNamed {
		b.Meta.Open = 0
	}
	if lost {
		if b.Meta.Interruptions < b.Limit {
			b.Meta.Interruptions++
		}
		b.Meta.Prepared = false
		if b.Meta.Interruptions >= b.Limit {
			b.Meta.Error = "interruption limit"
			b.Meta.ErrorAt = b.Spool
		}
	}
	switch b.Meta.Phase {
	case "Write", "Name", "DiscardPiece":
		if openNamed {
			b.Meta.Phase = "Capture"
		} else {
			b.Meta.Phase = "DiscardPiece"
		}
	case "Join", "PublishJoin":
		if joined {
			b.Meta.Phase = "HandoverReady"
		} else if b.Meta.Interruptions >= b.Limit {
			b.Meta.Phase = "DiscardJoinFailed"
		} else {
			b.Meta.Phase = "DiscardJoinRetry"
		}
	case "JoinFailed", "DiscardJoinFailed":
		b.Meta.Phase = "DiscardJoinRetry"
	case "Handover", "HandoverFailed":
		b.Meta.Phase = "HandoverReady"
	}
	return true
}

// reconcileInventory owns only per-piece metadata. Keeping this loop separate
// from recovery's scalar decisions makes both effects independently verifiable.
// @ requires len(ps) <= MaxCapacity && len(named) == len(ps)
// @ requires forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i])
// @ requires forall i int :: { &named[i] } 0 <= i && i < len(named) ==> acc(&named[i], 1/2)
// @ ensures forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i])
// @ ensures forall i int :: { &named[i] } 0 <= i && i < len(named) ==> acc(&named[i], 1/2)
// @ ensures seq(named) == old(seq(named))
// @ ensures forall i int :: { named[i] } 0 <= i && i < len(named) ==> named[i] == old(named[i])
// @ ensures 0 <= count && count <= len(ps) && count == namedPrefix(old(seq(named)), len(named))
// @ ensures forall i int :: { ps[i] } 0 <= i && i < len(ps) ==> ps[i].Known == named[i] && ps[i].Duration == old(ps[i].Duration) && ps[i].Omitted == (lost ? false : old(ps[i].Omitted)) && ps[i].Gap == (lost ? 0 : old(ps[i].Gap))
func reconcileInventory(ps []Piece, named []bool, lost bool) (count int) {
	i := 0
	// @ invariant 0 <= i && i <= len(ps) && len(ps) <= MaxCapacity && len(named) == len(ps)
	// @ invariant forall j int :: { &ps[j] } 0 <= j && j < len(ps) ==> acc(&ps[j])
	// @ invariant forall j int :: { &named[j] } 0 <= j && j < len(named) ==> acc(&named[j], 1/2)
	// @ invariant seq(named) == old(seq(named))
	// @ invariant forall j int :: { named[j] } 0 <= j && j < len(named) ==> named[j] == old(named[j])
	// @ invariant 0 <= count && count <= i && count == namedPrefix(seq(named), i)
	// @ invariant forall j int :: { ps[j] } 0 <= j && j < len(ps) ==> ps[j].Duration == old(ps[j].Duration)
	// @ invariant forall j int :: { ps[j] } 0 <= j && j < i ==> ps[j].Known == named[j]
	// @ invariant forall j int :: { ps[j] } 0 <= j && j < i && lost ==> !ps[j].Omitted && ps[j].Gap == 0
	// @ invariant forall j int :: { ps[j] } 0 <= j && j < len(ps) && !lost ==> ps[j].Omitted == old(ps[j].Omitted) && ps[j].Gap == old(ps[j].Gap)
	// @ decreases len(ps)-i
	for i < len(ps) {
		ps[i].Known = named[i]
		if named[i] {
			count++
		}
		if lost {
			ps[i].Omitted = false
			ps[i].Gap = 0
		}
		i++
	}
	return count
}
