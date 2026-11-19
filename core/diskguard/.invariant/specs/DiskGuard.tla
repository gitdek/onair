---- MODULE DiskGuard ----
EXTENDS Integers

CONSTANTS StartBytes, ReserveBytes, MaxPieceBytes, MaxFreeBytes
VARIABLES phase, free, result, stopCount, command, reason, finishedAfterDiskStop

vars == <<phase, free, result, stopCount, command, reason, finishedAfterDiskStop>>

\* Byte quantities are nonnegative integers; a real GB is 1,000,000,000
\* bytes. The caller supplies the production constants. MaxFreeBytes is
\* only the finite exploration bound, not a production disk-size limit.
ParameterOK ==
    /\ StartBytes \in Nat /\ ReserveBytes \in Nat
    /\ MaxPieceBytes \in Nat /\ MaxFreeBytes \in Nat
    /\ ReserveBytes > 0
    /\ MaxPieceBytes > 0
    /\ StartBytes >= ReserveBytes + 4 * MaxPieceBytes
    /\ MaxFreeBytes > StartBytes

StopLine == ReserveBytes + 2 * MaxPieceBytes

\* Negative sentinels encode unavailable readings, never actual byte counts.
FailedReading == -1

NoReading == -2

Readings == FailedReading..MaxFreeBytes

StartRoom(r) == IF r = FailedReading THEN FALSE ELSE r >= StartBytes

RunningRoom(r) == IF r = FailedReading THEN FALSE ELSE r >= StopLine

EmptyResult ==
    [purpose |-> "none", reading |-> FailedReading, allowed |-> FALSE,
     wasStopped |-> FALSE, emitsStop |-> FALSE,
     noticeFree |-> NoReading, noticeNeed |-> 0]

Reply(p, r, stopped) ==
    [purpose |-> p,
     reading |-> r,
     allowed |-> IF p = "start" THEN StartRoom(r)
                ELSE RunningRoom(r) /\ ~stopped,
     wasStopped |-> stopped,
     emitsStop |-> p # "start" /\ ~RunningRoom(r) /\ ~stopped,
     noticeFree |-> IF p = "start" /\ ~StartRoom(r) THEN r ELSE NoReading,
     noticeNeed |-> IF p = "start" /\ ~StartRoom(r) THEN StartBytes ELSE 0]

TypeOK ==
    /\ ParameterOK
    /\ phase \in {"idle", "ready", "running", "reading", "suspended",
                 "stopping", "final-reading", "drained", "finish-existing"}
    /\ free \in 0..MaxFreeBytes
    /\ result \in [purpose : {"none", "start", "resume", "piece"},
                    reading : Readings, allowed : BOOLEAN,
                    wasStopped : BOOLEAN, emitsStop : BOOLEAN,
                    noticeFree : NoReading..MaxFreeBytes,
                    noticeNeed : {0, StartBytes}]
    /\ stopCount \in 0..2
    /\ command \in {"none", "Stop"}
    /\ reason \in {"none", "disk nearly full"}
    /\ finishedAfterDiskStop \in BOOLEAN

RoomToStart ==
    result.purpose = "start" => result.allowed = StartRoom(result.reading)

ContinuationDecision ==
    result.purpose \in {"resume", "piece"} =>
        result.allowed = (RunningRoom(result.reading) /\ ~result.wasStopped)

OwnPiecesPreserveReserve ==
    phase \notin {"idle", "finish-existing"} => free >= ReserveBytes

OneStop == stopCount <= 1

StopIsLatched == stopCount > 0 => command = "Stop"

NoCaptureWhileStopRequested == command = "Stop" => ~result.allowed

StartRefusalExplainsSpace ==
    result.purpose = "start" /\ ~result.allowed =>
        /\ result.noticeFree = result.reading
        /\ result.noticeNeed = StartBytes

UnsafeReadingRequestsStop ==
    result.purpose \in {"resume", "piece"} /\ ~RunningRoom(result.reading) =>
        command = "Stop"

StopEmissionIsExact ==
    result.purpose \in {"resume", "piece"} =>
        result.emitsStop = (~RunningRoom(result.reading) /\ ~result.wasStopped)

OrdinaryStopWithReason ==
    /\ (command = "Stop") = (reason = "disk nearly full")
    /\ result.emitsStop => command = "Stop"

CaptureNeedsPermission ==
    phase \in {"ready", "running", "reading"} =>
        result.allowed /\ command = "none"

StartAtBoundary ==
    phase = "running" /\ result.purpose = "start"
    /\ result.reading = StartBytes /\ result.allowed

StartBelowBoundaryRefused ==
    result.purpose = "start" /\ result.reading = StartBytes - 1
    /\ ~result.allowed

FailedStartRefused ==
    result.purpose = "start" /\ result.reading = FailedReading /\ ~result.allowed

ContinueAtBoundary ==
    phase = "running" /\ result.purpose = "piece"
    /\ result.reading = StopLine /\ result.allowed

StopJustBelowBoundary ==
    phase = "stopping" /\ result.purpose = "piece"
    /\ result.reading = StopLine - 1 /\ result.emitsStop

FailedRunningReadStops ==
    phase = "stopping" /\ result.purpose = "piece"
    /\ result.reading = FailedReading /\ result.emitsStop

ResumeBelowStartLine ==
    phase = "running" /\ result.purpose = "resume"
    /\ result.reading = StopLine /\ result.reading < StartBytes
    /\ result.allowed

UnsafeResumeFinishesExisting ==
    phase = "finish-existing" /\ result.purpose = "resume"
    /\ result.reading = StopLine - 1 /\ ~result.allowed /\ result.emitsStop

FailedResumeFinishesExisting ==
    phase = "finish-existing" /\ result.purpose = "resume"
    /\ result.reading = FailedReading /\ ~result.allowed /\ result.emitsStop

StopSurvivesGoodReading ==
    result.purpose = "resume" /\ RunningRoom(result.reading)
    /\ result.wasStopped /\ ~result.allowed /\ ~result.emitsStop
    /\ stopCount = 1 /\ command = "Stop"

FinalPieceUsesLastRoom ==
    phase = "final-reading" /\ free = ReserveBytes /\ command = "Stop"

FinishedAndReusable ==
    /\ phase = "idle" /\ free = ReserveBytes
    /\ finishedAfterDiskStop /\ stopCount = 0 /\ command = "none"

\* phase is the caller's capture lifecycle, not detection of whether a
\* recording exists. free is an environment ledger for ONLY this capture's
\* pieces. stopCount counts each emitted stop until capture ends for good
\* or the guard crashes. finishedAfterDiskStop records a disk-stopped
\* capture whose final piece closed before the end-for-good notification;
\* a refused Start alone cannot establish this completion witness.
\* Both are proof monitors, not persisted state.
\* A successful read in this ledger-only execution reports free; failures
\* are independent inputs. Other disk users and finishing older recordings
\* are outside the piece-space guarantee. Reply is defined for every input,
\* including a successful reading below the reserve.
Init ==
    /\ phase = "idle"
    /\ free = 0
    /\ result = EmptyResult
    /\ stopCount = 0
    /\ command = "none"
    /\ reason = "none"
    /\ finishedAfterDiskStop = FALSE

\* Checking a Start and supplying THAT Start's reading are one operation;
\* no cached reading grants a later Start permission.
CheckStart(r) ==
    /\ phase = "idle"
    /\ result' = Reply("start", r, FALSE)
    /\ phase' = IF StartRoom(r) THEN "ready" ELSE "idle"
    /\ free' = IF r = FailedReading THEN 0 ELSE r
    /\ stopCount' = stopCount + (IF result'.emitsStop THEN 1 ELSE 0)
    /\ finishedAfterDiskStop' = FALSE
    /\ UNCHANGED <<command, reason>>

\* The caller identifies an existing recording, including after a crash.
\* A denied recovery has no open piece and finishes what already exists.
RecoverRecording(r) ==
    /\ phase = "idle"
    /\ result' = Reply("resume", r, command = "Stop")
    /\ phase' = IF result'.allowed THEN "ready" ELSE "finish-existing"
    /\ free' = IF r = FailedReading THEN 0 ELSE r
    /\ stopCount' = stopCount + (IF result'.emitsStop THEN 1 ELSE 0)
    /\ command' = IF result'.emitsStop THEN "Stop" ELSE command
    /\ reason' = IF result'.emitsStop THEN "disk nearly full" ELSE reason
    /\ finishedAfterDiskStop' = FALSE

CaptureStarted ==
    /\ phase = "ready"
    /\ phase' = "running"
    /\ UNCHANGED <<free, result, stopCount, command, reason, finishedAfterDiskStop>>

\* One open piece can be written before the next report. At its close,
\* its successor is already open; it may still be written after a stop.
ClosePiece(n) ==
    /\ phase = "running"
    /\ free' = free - n
    /\ phase' = "reading"
    /\ UNCHANGED <<result, stopCount, command, reason, finishedAfterDiskStop>>

ReportPiece(ok) ==
    LET r == IF ok THEN free ELSE FailedReading
    IN
        /\ phase = "reading"
        /\ result' = Reply("piece", r, command = "Stop")
        /\ phase' = IF result'.allowed THEN "running" ELSE "stopping"
        /\ stopCount' = stopCount + (IF result'.emitsStop THEN 1 ELSE 0)
        /\ command' = IF result'.emitsStop THEN "Stop" ELSE command
        /\ reason' = IF result'.emitsStop THEN "disk nearly full" ELSE reason
        /\ UNCHANGED <<free, finishedAfterDiskStop>>

CloseFinalPiece(n) ==
    /\ phase = "stopping"
    /\ free' = free - n
    /\ phase' = "final-reading"
    /\ UNCHANGED <<result, stopCount, command, reason, finishedAfterDiskStop>>

ReportFinalPiece(ok) ==
    LET r == IF ok THEN free ELSE FailedReading
    IN
        /\ phase = "final-reading"
        /\ result' = Reply("piece", r, command = "Stop")
        /\ phase' = "drained"
        /\ stopCount' = stopCount + (IF result'.emitsStop THEN 1 ELSE 0)
        /\ UNCHANGED <<free, command, reason, finishedAfterDiskStop>>

\* These are caller events. Closing a piece on an ordinary user Stop or
\* on an interruption is charged before capture is declared ended.
CallerEndsCapture(n) ==
    /\ phase \in {"running", "reading"}
    /\ free' = free - n
    /\ phase' = "drained"
    /\ UNCHANGED <<result, stopCount, command, reason, finishedAfterDiskStop>>

CaptureInterrupted(n) ==
    /\ phase \in {"running", "reading"}
    /\ free' = free - n
    /\ phase' = "suspended"
    /\ UNCHANGED <<result, stopCount, command, reason, finishedAfterDiskStop>>

ResumeCapture(ok) ==
    LET r == IF ok THEN free ELSE FailedReading
    IN
        /\ phase = "suspended"
        /\ result' = Reply("resume", r, command = "Stop")
        /\ phase' = IF result'.allowed THEN "ready" ELSE "finish-existing"
        /\ stopCount' = stopCount + (IF result'.emitsStop THEN 1 ELSE 0)
        /\ command' = IF result'.emitsStop THEN "Stop" ELSE command
        /\ reason' = IF result'.emitsStop THEN "disk nearly full" ELSE reason
        /\ UNCHANGED <<free, finishedAfterDiskStop>>

RetryResumeAfterStop(ok) ==
    LET r == IF ok THEN free ELSE FailedReading
    IN
        /\ phase \in {"stopping", "final-reading", "drained", "finish-existing"}
        /\ command = "Stop"
        /\ result' = Reply("resume", r, command = "Stop")
        /\ stopCount' = stopCount + (IF result'.emitsStop THEN 1 ELSE 0)
        /\ UNCHANGED <<phase, free, command, reason, finishedAfterDiskStop>>

CaptureEndedForGood ==
    /\ phase \in {"suspended", "final-reading", "drained", "finish-existing"}
    /\ phase' = "idle"
    /\ result' = EmptyResult
    /\ stopCount' = 0
    /\ command' = "none"
    /\ reason' = "none"
    /\ finishedAfterDiskStop' =
        (stopCount > 0 /\ command = "Stop"
         /\ phase \in {"final-reading", "drained"})
    /\ UNCHANGED free

\* No guard state survives a crash; the caller supplies a fresh reading
\* and identifies an existing recording on recovery.
Crash ==
    /\ phase' = "idle"
    /\ free' = 0
    /\ result' = EmptyResult
    /\ stopCount' = 0
    /\ command' = "none"
    /\ reason' = "none"
    /\ finishedAfterDiskStop' = FALSE

WaitForCaller == UNCHANGED vars

Next ==
    \/ (\E r \in Readings : CheckStart(r) \/ RecoverRecording(r))
    \/ CaptureStarted
    \/ (\E n \in 0..MaxPieceBytes :
            ClosePiece(n) \/ CloseFinalPiece(n)
            \/ CallerEndsCapture(n) \/ CaptureInterrupted(n))
    \/ (\E ok \in BOOLEAN :
            ReportPiece(ok) \/ ReportFinalPiece(ok)
            \/ ResumeCapture(ok) \/ RetryResumeAfterStop(ok))
    \/ CaptureEndedForGood
    \/ Crash
    \/ WaitForCaller

\* Hypothetical mistakes, deliberately absent from Next.
StartUsingOldReading(r) ==
    /\ phase = "idle"
    /\ free >= StartBytes
    /\ ~StartRoom(r)
    /\ result' = [Reply("start", r, FALSE) EXCEPT !.allowed = TRUE]
    /\ phase' = "ready"
    /\ free' = IF r = FailedReading THEN 0 ELSE r
    /\ finishedAfterDiskStop' = FALSE
    /\ UNCHANGED <<stopCount, command, reason>>

BugStaleStart == \E r \in Readings : StartUsingOldReading(r)

StartUsingReserve(r) ==
    /\ phase = "idle"
    /\ r \in ReserveBytes..(StartBytes - 1)
    /\ result' = [Reply("start", r, FALSE) EXCEPT !.allowed = TRUE]
    /\ phase' = "ready"
    /\ free' = r
    /\ finishedAfterDiskStop' = FALSE
    /\ UNCHANGED <<stopCount, command, reason>>

BugStartAtReserve == \E r \in Readings : StartUsingReserve(r)

BugFailedStartAllowed ==
    /\ phase = "idle"
    /\ result' = [Reply("start", FailedReading, FALSE) EXCEPT !.allowed = TRUE]
    /\ phase' = "ready"
    /\ free' = 0
    /\ finishedAfterDiskStop' = FALSE
    /\ UNCHANGED <<stopCount, command, reason>>

BugStopOnlyBelowReserve ==
    /\ phase = "reading"
    /\ free >= ReserveBytes /\ free < StopLine
    /\ result' = [Reply("piece", free, FALSE) EXCEPT
                      !.allowed = TRUE, !.emitsStop = FALSE]
    /\ phase' = "running"
    /\ UNCHANGED <<free, stopCount, command, reason, finishedAfterDiskStop>>

BugOnlyOnePieceMargin ==
    /\ phase = "reading"
    /\ free >= ReserveBytes + MaxPieceBytes
    /\ free < StopLine
    /\ result' = [Reply("piece", free, FALSE) EXCEPT
                      !.allowed = TRUE, !.emitsStop = FALSE]
    /\ phase' = "running"
    /\ UNCHANGED <<free, stopCount, command, reason, finishedAfterDiskStop>>

BugFailedRunningReadAllowed ==
    /\ phase = "reading"
    /\ result' = [Reply("piece", FailedReading, FALSE) EXCEPT
                      !.allowed = TRUE, !.emitsStop = FALSE]
    /\ phase' = "running"
    /\ UNCHANGED <<free, stopCount, command, reason, finishedAfterDiskStop>>

BugResumeRequiresStartLine ==
    /\ phase = "suspended"
    /\ free >= StopLine /\ free < StartBytes
    /\ result' = [Reply("resume", free, FALSE) EXCEPT
                      !.allowed = FALSE, !.emitsStop = TRUE]
    /\ phase' = "finish-existing"
    /\ stopCount' = stopCount + (IF result'.emitsStop THEN 1 ELSE 0)
    /\ command' = "Stop"
    /\ reason' = "disk nearly full"
    /\ UNCHANGED <<free, finishedAfterDiskStop>>

\* The same faulty reading handler forgets that the stop was requested.
\* Check its failed and sufficient inputs separately, since they must be
\* caught by different invariants: OneStop and NoCaptureWhileStopRequested.
ForgetStopOnReading(ok) ==
    LET r == IF ok THEN free ELSE FailedReading
    IN
        /\ phase = "final-reading"
        /\ command = "Stop"
        /\ result' = Reply("piece", r, FALSE)
        /\ phase' = "drained"
        /\ stopCount' = stopCount + (IF result'.emitsStop THEN 1 ELSE 0)
        /\ UNCHANGED <<free, command, reason, finishedAfterDiskStop>>

BugDuplicateStop == ForgetStopOnReading(FALSE)

BugForgetStopOnGoodReading ==
    /\ free >= StopLine
    /\ ForgetStopOnReading(TRUE)

BugWithdrawStop ==
    /\ command = "Stop"
    /\ free >= StopLine
    /\ command' = "none"
    /\ reason' = "none"
    /\ UNCHANGED <<phase, free, result, stopCount, finishedAfterDiskStop>>

BugWriteAfterFinalPiece ==
    /\ phase = "final-reading"
    /\ free = ReserveBytes
    /\ free' = free - MaxPieceBytes
    /\ UNCHANGED <<phase, result, stopCount, command, reason, finishedAfterDiskStop>>

Spec == Init /\ [][Next]_vars
====
