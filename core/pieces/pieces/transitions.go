// +gobra

package pieces

// AcceptPiece requests a hidden write using only the saved next number; it never examines the disk.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ requires 0 < seconds
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.Phase == "Capture" && b.Meta.Capture == "active" && !b.Meta.CaptureFault && 1 <= b.Meta.NextNumber && b.Meta.NextNumber <= len(b.Pieces))
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == "Write"
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures ok ==> b.Meta.Open == old(b.Meta.NextNumber)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures ok ==> b.Meta.FlushReported == false
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures !ok ==> forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
// @ ensures ok ==> forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> (b.Pieces[i].Duration == (i == old(b.Meta.NextNumber)-1 ? seconds : old(b.Pieces[i].Duration)) && b.Pieces[i].Known == old(b.Pieces[i].Known) && b.Pieces[i].Omitted == old(b.Pieces[i].Omitted) && b.Pieces[i].Gap == old(b.Pieces[i].Gap))
func (b *Recording) AcceptPiece(seconds int) (ok bool) {
	if !(b.Meta.Phase == "Capture" && b.Meta.Capture == "active" && !b.Meta.CaptureFault && 1 <= b.Meta.NextNumber && b.Meta.NextNumber <= len(b.Pieces)) {
		return false
	}
	b.Meta.Open = b.Meta.NextNumber
	b.Pieces[b.Meta.Open-1].Duration = seconds
	b.Meta.FlushReported = false
	b.Meta.Phase = "Write"
	return true
}

// ReportPieceFlushed authorizes naming only upon the caller's durable-write report.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.Phase == "Write" && flushed)
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == "Name"
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures ok ==> b.Meta.FlushReported == true
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) ReportPieceFlushed(flushed bool) (ok bool) {
	if !(b.Meta.Phase == "Write" && flushed) {
		return false
	}
	b.Meta.Phase = "Name"
	b.Meta.FlushReported = true
	return true
}

// ReportPieceNamed records the caller's successful naming; a missing report is reconciled at launch.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.Phase == "Name" && named && 1 <= b.Meta.Open && b.Meta.Open <= len(b.Pieces))
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == "Capture"
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures ok ==> b.Meta.Open == 0
// @ ensures ok ==> b.Meta.NextNumber == old(b.Meta.Open)+1
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures !ok ==> forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
// @ ensures ok ==> forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> (b.Pieces[i].Known == (i == old(b.Meta.Open)-1 || old(b.Pieces[i].Known)) && b.Pieces[i].Duration == old(b.Pieces[i].Duration) && b.Pieces[i].Omitted == old(b.Pieces[i].Omitted) && b.Pieces[i].Gap == old(b.Pieces[i].Gap))
func (b *Recording) ReportPieceNamed(named bool) (ok bool) {
	if !(b.Meta.Phase == "Name" && named && 1 <= b.Meta.Open && b.Meta.Open <= len(b.Pieces)) {
		return false
	}
	b.Pieces[b.Meta.Open-1].Known = true
	b.Meta.NextNumber = b.Meta.Open + 1
	b.Meta.Open = 0
	b.Meta.Phase = "Capture"
	return true
}

// ReportWriteFailure stops this capture and requests disposal of its unnamed remnant.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old((b.Meta.Phase == "Write" || b.Meta.Phase == "Name") && !named)
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == "DiscardPiece"
// @ ensures ok ==> b.Meta.Capture == (old(b.Meta.Capture) == "final" ? "final" : "failed")
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures ok ==> b.Meta.CaptureFault == true
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures ok ==> b.Meta.Error == "write failed"
// @ ensures ok ==> b.Meta.ErrorAt == old(b.Spool)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) ReportWriteFailure(named bool) (ok bool) {
	if !((b.Meta.Phase == "Write" || b.Meta.Phase == "Name") && !named) {
		return false
	}
	if b.Meta.Capture != "final" {
		b.Meta.Capture = "failed"
	}
	b.Meta.CaptureFault = true
	b.Meta.Phase = "DiscardPiece"
	b.Meta.Error = "write failed"
	b.Meta.ErrorAt = b.Spool
	return true
}

// ReportRemnantDiscarded advances the saved record when its request is applicable.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.Phase == "DiscardPiece" && absent)
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == "Capture"
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures ok ==> b.Meta.Open == 0
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) ReportRemnantDiscarded(absent bool) (ok bool) {
	if !(b.Meta.Phase == "DiscardPiece" && absent) {
		return false
	}
	b.Meta.Open = 0
	b.Meta.Phase = "Capture"
	return true
}

// CaptureEnded advances the saved record when its request is applicable.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.Capture == "active" && (b.Meta.Phase == "Capture" || b.Meta.Phase == "Write" || b.Meta.Phase == "Name"))
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures b.Meta.Phase == old(b.Meta.Phase)
// @ ensures ok ==> b.Meta.Capture == "paused"
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) CaptureEnded() (ok bool) {
	if !(b.Meta.Capture == "active" && (b.Meta.Phase == "Capture" || b.Meta.Phase == "Write" || b.Meta.Phase == "Name")) {
		return false
	}
	b.Meta.Capture = "paused"
	return true
}

// CarryOn advances the saved record when its request is applicable.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.Phase == "Capture" && b.Meta.Capture == "paused" && !b.Meta.CaptureFault)
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures b.Meta.Phase == old(b.Meta.Phase)
// @ ensures ok ==> b.Meta.Capture == "active"
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) CarryOn() (ok bool) {
	if !(b.Meta.Phase == "Capture" && b.Meta.Capture == "paused" && !b.Meta.CaptureFault) {
		return false
	}
	b.Meta.Capture = "active"
	return true
}

// FinishCapture makes finalization irrevocable, while allowing an outstanding piece request to settle.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old((b.Meta.Phase == "Capture" || b.Meta.Phase == "Write" || b.Meta.Phase == "Name" || b.Meta.Phase == "DiscardPiece") && b.Meta.Capture != "final")
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures b.Meta.Phase == old(b.Meta.Phase)
// @ ensures ok ==> b.Meta.Capture == "final"
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) FinishCapture() (ok bool) {
	if !((b.Meta.Phase == "Capture" || b.Meta.Phase == "Write" || b.Meta.Phase == "Name" || b.Meta.Phase == "DiscardPiece") && b.Meta.Capture != "final") {
		return false
	}
	b.Meta.Capture = "final"
	return true
}

// FinalizeCapture waits for every open piece to settle before scheduling a join.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.Phase == "Capture" && b.Meta.Capture == "final" && b.Meta.Open == 0)
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == (old(knownPrefix(seq(b.Pieces), len(b.Pieces))) == 0 ? "Empty" : "JoinReady")
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) FinalizeCapture() (ok bool) {
	if !(b.Meta.Phase == "Capture" && b.Meta.Capture == "final" && b.Meta.Open == 0) {
		return false
	}
	if countKnown(b.Pieces) == 0 {
		b.Meta.Phase = "Empty"
	} else {
		b.Meta.Phase = "JoinReady"
	}
	return true
}

// RequestJoin requests every closed piece once, in numeric order. JoinReady implies partial output was discarded.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.Phase == "JoinReady")
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == "Join"
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures ok ==> b.Meta.JoinCount == old(knownPrefix(seq(b.Pieces), len(b.Pieces)))
// @ ensures ok ==> b.Meta.Prepared == false
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures ok ==> b.Meta.Error == ""
// @ ensures ok ==> b.Meta.ErrorAt == ""
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures !ok ==> forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
// @ ensures ok ==> forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> (b.Pieces[i].Duration == old(b.Pieces[i].Duration) && b.Pieces[i].Known == old(b.Pieces[i].Known) && !b.Pieces[i].Omitted && b.Pieces[i].Gap == 0)
func (b *Recording) RequestJoin() (ok bool) {
	if b.Meta.Phase != "JoinReady" {
		return false
	}
	b.Meta.JoinCount = countKnown(b.Pieces)
	clearManifest(b.Pieces)
	b.Meta.Prepared = false
	b.Meta.Error = ""
	b.Meta.ErrorAt = ""
	b.Meta.Phase = "Join"
	return true
}

// ReportJoinPrepared saves omissions and their durations before the caller may name the joined file.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ requires len(missing) == len(b.Pieces)
// @ requires forall i int :: { &missing[i] } 0 <= i && i < len(missing) ==> acc(&missing[i], 1/2)
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures forall i int :: { &missing[i] } 0 <= i && i < len(missing) ==> acc(&missing[i], 1/2)
// @ ensures forall i int :: { missing[i] } 0 <= i && i < len(missing) ==> missing[i] == old(missing[i])
// @ ensures ok == old(b.Meta.Phase == "Join" && outputReady && (forall i int :: 0 <= i && i < len(b.Pieces) ==> (missing[i] ==> b.Pieces[i].Known)) && (exists i int :: 0 <= i && i < len(b.Pieces) && b.Pieces[i].Known && !missing[i]))
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == "PublishJoin"
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures ok ==> b.Meta.Prepared == true
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures !ok ==> forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
// @ ensures ok ==> forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> (b.Pieces[i].Known == old(b.Pieces[i].Known) && b.Pieces[i].Duration == old(b.Pieces[i].Duration) && b.Pieces[i].Omitted == missing[i] && b.Pieces[i].Gap == (missing[i] ? old(b.Pieces[i].Duration) : 0))
func (b *Recording) ReportJoinPrepared(missing []bool, outputReady bool) (ok bool) {
	if b.Meta.Phase != "Join" || !outputReady {
		return false
	}
	// The join may leave out only known pieces, and must keep one. Each scan
	// returns the piece that settles its half, or -1, and the assertion names
	// that witness.
	k := omittedUnknown(b.Pieces, missing)
	if k >= 0 {
		// @ assert old(missing[k]) && !old(b.Pieces[k].Known)
		return false
	}
	k = includedKnown(b.Pieces, missing)
	if k < 0 {
		return false
	}
	// @ assert old(b.Pieces[k].Known) && !old(missing[k])
	fillManifest(b.Pieces, missing)
	b.Meta.Prepared = true
	b.Meta.Phase = "PublishJoin"
	return true
}

// ReportJoinNamed advances the saved record when its request is applicable.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.Phase == "PublishJoin" && named)
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == "HandoverReady"
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) ReportJoinNamed(named bool) (ok bool) {
	if !(b.Meta.Phase == "PublishJoin" && named) {
		return false
	}
	b.Meta.Phase = "HandoverReady"
	return true
}

// ReportJoinFailure preserves the inventory and reports the failure together with its spool location.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old((b.Meta.Phase == "Join" || b.Meta.Phase == "PublishJoin") && !named && (reason == "join failed" || reason == "none readable") && (reason != "none readable" || (b.Meta.Phase == "Join" && partial)))
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == "JoinFailed"
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures ok ==> b.Meta.Prepared == false
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures ok ==> b.Meta.Error == reason
// @ ensures ok ==> b.Meta.ErrorAt == old(b.Spool)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures !ok ==> forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
// @ ensures ok ==> forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> (b.Pieces[i].Duration == old(b.Pieces[i].Duration) && b.Pieces[i].Known == old(b.Pieces[i].Known) && !b.Pieces[i].Omitted && b.Pieces[i].Gap == 0)
func (b *Recording) ReportJoinFailure(reason string, named, partial bool) (ok bool) {
	if !((b.Meta.Phase == "Join" || b.Meta.Phase == "PublishJoin") && !named && (reason == "join failed" || reason == "none readable") && (reason != "none readable" || (b.Meta.Phase == "Join" && partial))) {
		return false
	}
	clearManifest(b.Pieces)
	b.Meta.Phase = "JoinFailed"
	b.Meta.Prepared = false
	b.Meta.Error = reason
	b.Meta.ErrorAt = b.Spool
	return true
}

// AskRetry advances the saved record when its request is applicable.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old((b.Meta.Phase == "JoinFailed" || b.Meta.Phase == "HandoverFailed") && !b.Meta.RetryPending)
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures b.Meta.Phase == old(b.Meta.Phase)
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures ok ==> b.Meta.RetryPending == true
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) AskRetry() (ok bool) {
	if !((b.Meta.Phase == "JoinFailed" || b.Meta.Phase == "HandoverFailed") && !b.Meta.RetryPending) {
		return false
	}
	b.Meta.RetryPending = true
	return true
}

// ScheduleRetry consumes a person's request without resetting the cumulative interruption count.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.RetryPending && (b.Meta.Phase == "JoinFailed" || b.Meta.Phase == "HandoverFailed"))
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == (old(b.Meta.Phase) == "JoinFailed" ? "DiscardJoinRetry" : "HandoverReady")
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures ok ==> b.Meta.RetryPending == false
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) ScheduleRetry() (ok bool) {
	if !(b.Meta.RetryPending && (b.Meta.Phase == "JoinFailed" || b.Meta.Phase == "HandoverFailed")) {
		return false
	}
	if b.Meta.Phase == "JoinFailed" {
		b.Meta.Phase = "DiscardJoinRetry"
	} else {
		b.Meta.Phase = "HandoverReady"
	}
	b.Meta.RetryPending = false
	return true
}

// ReportPartialJoinDiscarded advances the saved record when its request is applicable.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old((b.Meta.Phase == "DiscardJoinRetry" || b.Meta.Phase == "DiscardJoinFailed") && absent)
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == (old(b.Meta.Phase) == "DiscardJoinRetry" ? "JoinReady" : "JoinFailed")
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) ReportPartialJoinDiscarded(absent bool) (ok bool) {
	if !((b.Meta.Phase == "DiscardJoinRetry" || b.Meta.Phase == "DiscardJoinFailed") && absent) {
		return false
	}
	if b.Meta.Phase == "DiscardJoinRetry" {
		b.Meta.Phase = "JoinReady"
	} else {
		b.Meta.Phase = "JoinFailed"
	}
	return true
}

// RequestHandover asks for this recording's single joined file, deduplicated by ID.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.Phase == "HandoverReady")
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == "Handover"
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures ok ==> b.Meta.Error == ""
// @ ensures ok ==> b.Meta.ErrorAt == ""
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) RequestHandover() (ok bool) {
	if !(b.Meta.Phase == "HandoverReady") {
		return false
	}
	b.Meta.Phase = "Handover"
	b.Meta.Error = ""
	b.Meta.ErrorAt = ""
	return true
}

// ReportHandoverSuccess advances the saved record when its request is applicable.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.Phase == "Handover" && delivered)
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == "Retained"
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures ok ==> b.Meta.HandedOver == true
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) ReportHandoverSuccess(delivered bool) (ok bool) {
	if !(b.Meta.Phase == "Handover" && delivered) {
		return false
	}
	b.Meta.Phase = "Retained"
	b.Meta.HandedOver = true
	return true
}

// ReportHandoverFailure advances the saved record when its request is applicable.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.Phase == "Handover")
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == "HandoverFailed"
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures ok ==> b.Meta.Error == "handover failed"
// @ ensures ok ==> b.Meta.ErrorAt == old(b.Spool)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) ReportHandoverFailure() (ok bool) {
	if !(b.Meta.Phase == "Handover") {
		return false
	}
	b.Meta.Phase = "HandoverFailed"
	b.Meta.Error = "handover failed"
	b.Meta.ErrorAt = b.Spool
	return true
}

// ReportThreeDaysElapsed advances the saved record when its request is applicable.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.HandedOver && !b.Meta.RetentionElapsed)
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures b.Meta.Phase == old(b.Meta.Phase)
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures ok ==> b.Meta.RetentionElapsed == true
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) ReportThreeDaysElapsed() (ok bool) {
	if !(b.Meta.HandedOver && !b.Meta.RetentionElapsed) {
		return false
	}
	b.Meta.RetentionElapsed = true
	return true
}

// RequestRemoval authorizes piece removal only after successful hand-over and the caller's retention report.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.Phase == "Retained" && b.Meta.HandedOver && b.Meta.RetentionElapsed)
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == "Remove"
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
func (b *Recording) RequestRemoval() (ok bool) {
	if !(b.Meta.Phase == "Retained" && b.Meta.HandedOver && b.Meta.RetentionElapsed) {
		return false
	}
	b.Meta.Phase = "Remove"
	return true
}

// ReportRemovalComplete clears the inventory only when the caller confirms every piece has been removed.
// @ requires acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ requires forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ requires len(b.Pieces) <= MaxCapacity
// @ requires 0 < b.Limit && b.Limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces, 1/2) && acc(&b.ID, 1/2) && acc(&b.Spool, 1/2) && acc(&b.Limit, 1/2)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == old(len(b.Pieces)) && cap(b.Pieces) == old(cap(b.Pieces)) && (b.Pieces == nil) == old(b.Pieces == nil) && (forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> &b.Pieces[i] == old(&b.Pieces[i])) && b.ID == old(b.ID) && b.Spool == old(b.Spool) && b.Limit == old(b.Limit)
// @ ensures ok == old(b.Meta.Phase == "Remove" && allAbsent)
// @ ensures !ok ==> b.Meta == old(b.Meta)
// @ ensures ok ==> b.Meta.Phase == "Done"
// @ ensures b.Meta.Capture == old(b.Meta.Capture)
// @ ensures b.Meta.Open == old(b.Meta.Open)
// @ ensures b.Meta.NextNumber == old(b.Meta.NextNumber)
// @ ensures b.Meta.FlushReported == old(b.Meta.FlushReported)
// @ ensures b.Meta.CaptureFault == old(b.Meta.CaptureFault)
// @ ensures b.Meta.JoinCount == old(b.Meta.JoinCount)
// @ ensures b.Meta.Prepared == old(b.Meta.Prepared)
// @ ensures b.Meta.Interruptions == old(b.Meta.Interruptions)
// @ ensures b.Meta.HandedOver == old(b.Meta.HandedOver)
// @ ensures b.Meta.RetentionElapsed == old(b.Meta.RetentionElapsed)
// @ ensures b.Meta.Error == old(b.Meta.Error)
// @ ensures b.Meta.ErrorAt == old(b.Meta.ErrorAt)
// @ ensures b.Meta.RetryPending == old(b.Meta.RetryPending)
// @ ensures !ok ==> forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == old(b.Pieces[i])
// @ ensures ok ==> forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> (!b.Pieces[i].Known && b.Pieces[i].Duration == old(b.Pieces[i].Duration) && b.Pieces[i].Omitted == old(b.Pieces[i].Omitted) && b.Pieces[i].Gap == old(b.Pieces[i].Gap))
func (b *Recording) ReportRemovalComplete(allAbsent bool) (ok bool) {
	if b.Meta.Phase != "Remove" || !allAbsent {
		return false
	}
	forgetPieces(b.Pieces)
	b.Meta.Phase = "Done"
	return true
}
