---- MODULE RecordingSession ----
EXTENDS Integers, Sequences, FiniteSets

CONSTANTS Recordings, Errors, MaxRestarts

VARIABLES records, queue, online, capture, finishing, notice,
          attempts, stopAt, ends, shown, closedOrder, event

vars == <<records, queue, online, capture, finishing, notice,
          attempts, stopAt, ends, shown, closedOrder, event>>

\* records and queue are the whole saved record. An action that changes them
\* commits them before exposing any effect. All other operational state is
\* volatile. attempts, stopAt, ends, shown, closedOrder and event are ghost
\* observations for the specification, not additional data to save.
\* attempts and stopAt monitor start ordinals while capture can run. Once
\* capture closes, both use the canonical accepted ordinal 1 (or -1 for no
\* Stop): exact old retry counts are irrelevant because Closed can never run
\* again. This quotients history without changing any operational behavior.
\* An identity also represents its immutable original capture selection and
\* pieces-directory handle; neither choosing nor interpreting those is here.
\* Caller replies describe the current request. Duplicate or stale replies
\* have no effect. Writing pieces and idempotent archive delivery are outside
\* this component. A finish reply describes their durable, repeatable result.
\* event observes emitted effects, including nonpersistent refusal, gap and
\* automatic-stop messages. End notices alone require a display receipt.
\* Empty/nonempty is a finishing fact supplied by the caller, not a fact this
\* component infers from capture callbacks or by inspecting files. A handed
\* result with an error represents a join that omitted an unreadable piece;
\* the caller's error carries the gap length, abstracted here by Errors.
\* closedOrder observes each permanent capture end together with its cause,
\* independently of the later finish reply, even after a record is removed.

None == "none"

Causes == Errors \cup {"stop", "retryLimit"}

NoError == [code |-> "none", why |-> None]

FailureError(e) == [code |-> "failure", why |-> e]

EmptyResult == [outcome |-> None, error |-> NoError, empty |-> FALSE]

NothingError(c) == [code |-> "nothingRecorded", why |-> c]

Results ==
    {[outcome |-> "handed", error |-> NoError, empty |-> FALSE]}
    \cup {[outcome |-> k, error |-> FailureError(e), empty |-> FALSE] :
             k \in {"handed", "kept"}, e \in Errors}
    \cup {[outcome |-> "kept", error |-> NothingError(c), empty |-> TRUE] :
             c \in Causes}

RecordType ==
    [phase : {"starting", "live", "retry", "queued", "ended"},
     stopped : BOOLEAN, retries : 0..MaxRestarts,
     cause : Causes \cup {None}, result : Results \cup {EmptyResult}]

IsBook(b) == DOMAIN b \subseteq Recordings /\ b \in [DOMAIN b -> RecordType]

Fresh == [phase |-> "starting", stopped |-> FALSE, retries |-> 0,
          cause |-> None, result |-> EmptyResult]

Queued(c) == [phase |-> "queued", stopped |-> FALSE, retries |-> 0,
              cause |-> c, result |-> EmptyResult]

Ended(v) == [phase |-> "ended", stopped |-> FALSE, retries |-> 0,
             cause |-> None, result |-> v]

NoCapture == [id |-> None, mode |-> None, attempt |-> 0, target |-> None]

NoNotice == [id |-> None, result |-> EmptyResult, directory |-> None]

NoEvent == [kind |-> None, id |-> None, before |-> [r \in {} |-> Fresh],
            beforeQueue |-> <<>>, running |-> {}, retry |-> 0,
            menu |-> None, reason |-> None]

Ids(s) == {s[i] : i \in 1..Len(s)}

Running == {r \in DOMAIN records :
              records[r].phase \in {"starting", "live", "retry"}}

Accepted == {r \in Recordings : attempts[r] > 0}

Stopped == {r \in Recordings : stopAt[r] >= 0}

Completed == {r \in Recordings : Len(ends[r]) > 0}

ClosedIds == [i \in 1..Len(closedOrder) |-> closedOrder[i].id]

Closed == Ids(ClosedIds)

CaptureEndCause(r) ==
    closedOrder[CHOOSE i \in 1..Len(closedOrder) : closedOrder[i].id = r].cause

TypeOK ==
    /\ IsFiniteSet(Recordings) /\ IsFiniteSet(Errors)
    /\ Recordings # {} /\ Errors # {}
    /\ None \notin Recordings /\ None \notin Errors
    /\ MaxRestarts \in Nat
    /\ IsBook(records)
    /\ queue \in Seq(Recordings) /\ Len(queue) <= Cardinality(Recordings)
    /\ online \in BOOLEAN
    /\ capture \in [id : Recordings \cup {None},
                    mode : {None, "start", "live", "stop"},
                    attempt : 0..MaxRestarts, target : Recordings \cup {None}]
    /\ finishing \subseteq Recordings
    /\ notice \in [id : Recordings \cup {None},
                   result : Results \cup {EmptyResult},
                   directory : Recordings \cup {None}]
    /\ attempts \in [Recordings -> 0..(MaxRestarts + 1)]
    /\ stopAt \in [Recordings -> (-1)..(MaxRestarts + 1)]
    /\ ends \in [Recordings -> Seq(Results)]
    /\ \A r \in Recordings : Len(ends[r]) <= 2
    /\ shown \subseteq Recordings
    /\ closedOrder \in Seq([id : Recordings, cause : Causes])
    /\ Len(closedOrder) <= Cardinality(Recordings)
    /\ event \in [kind : {None, "accepted", "refused", "stop", "restart",
                         "started", "finish", "crash", "launch", "captureEnded", "failed"},
                  id : Recordings \cup {None}, before : {event.before},
                  beforeQueue : Seq(Recordings), running : SUBSET Recordings,
                  retry : 0..MaxRestarts,
                  menu : {None, "refused", "gap", "automaticStop"},
                  reason : Errors \cup {None}]
    /\ IsBook(event.before)

OneRunning == Cardinality(Running) <= 1

StartDecision ==
    /\ (event.kind = "accepted" =>
          /\ event.running = {}
          /\ event.id \in Running
          /\ capture.id = event.id /\ capture.mode = "start")
    /\ (event.kind = "refused" =>
          /\ event.running # {}
          /\ event.menu = "refused"
          /\ records = event.before /\ queue = event.beforeQueue)

CaptureBelongsToRunning ==
    /\ (capture.mode # None =>
          /\ online /\ capture.id \in Running
          /\ capture.target = capture.id
          /\ capture.attempt = records[capture.id].retries)
    /\ (capture.mode = None => capture = NoCapture)
    /\ \A r \in Running : online =>
          /\ (records[r].phase = "starting" => capture.id = r /\ capture.mode = "start")
          /\ (records[r].phase = "live" => capture.id = r /\ capture.mode \in {"live", "stop"})
          /\ (records[r].phase = "retry" => capture = NoCapture)

EveryAcceptedIsAccountedFor ==
    /\ DOMAIN records \subseteq Accepted
    /\ Accepted \subseteq DOMAIN records \cup shown
    /\ Completed \subseteq Closed
    /\ shown \subseteq Completed

ExactlyOneEnd ==
    /\ \A r \in Recordings : Len(ends[r]) <= 1
    /\ \A r \in DOMAIN records :
          (records[r].phase = "ended") <=> (r \in Completed)
    /\ \A r \in DOMAIN records \cap Completed :
          records[r].result = Head(ends[r])

KeptHasError ==
    \A r \in Completed :
        Head(ends[r]).outcome = "kept" => Head(ends[r]).error # NoError

NothingRecordedIsKept ==
    \A r \in Completed :
        Head(ends[r]).empty =>
          /\ Head(ends[r]).outcome = "kept"
          /\ Head(ends[r]).error = NothingError(CaptureEndCause(r))

UnshownEndIsSaved ==
    Completed \ shown \subseteq DOMAIN records

EndNoticeIsExact ==
    notice.id # None =>
      /\ notice.id \in DOMAIN records \cap Completed
      /\ notice.result = Head(ends[notice.id])
      /\ notice.directory = notice.id
      /\ notice.id \notin shown

StopIsLatched ==
    /\ \A r \in Running : records[r].stopped <=> (r \in Stopped)
    /\ (event.kind = "stop" /\ event.id \in event.running =>
          event.id \in Stopped /\ records[event.id].stopped)
    /\ (event.kind = "stop" /\ event.id \notin event.running =>
          records = event.before /\ queue = event.beforeQueue)
    /\ (capture.mode = "stop" => capture.id \in Stopped)

StartingStopIsImmediate ==
    event.kind = "started" /\ event.id \in Stopped => capture.mode = "stop"

NoStartAfterStop ==
    \A r \in Stopped : attempts[r] = stopAt[r]

AutomaticStopExplained ==
    event.kind = "stop" /\ event.reason \in Errors /\ event.id \in event.running
      => event.menu = "automaticStop"

RestartBudgetIsSaved ==
    /\ \A r \in Running : attempts[r] = records[r].retries + 1
    /\ \A r \in Accepted : attempts[r] <= MaxRestarts + 1
    /\ (event.kind = "restart" =>
          /\ records[event.id].retries = event.retry + 1
          /\ capture.id = event.id /\ capture.mode = "start")

RestartShowsGap ==
    event.kind = "started" =>
      /\ (event.retry > 0 <=> event.menu = "gap")
      /\ capture.id = event.id /\ capture.target = event.id

FinishAfterCapture ==
    /\ finishing \subseteq Closed
    /\ Closed \cap Running = {}
    /\ (capture.mode # None => capture.id \notin Closed)
    /\ \A r \in finishing : records[r].phase = "queued"

OneFinishInOrder ==
    /\ Cardinality(finishing) <= 1
    /\ Len(closedOrder) = Cardinality(Closed)
    /\ queue = SelectSeq(ClosedIds, LAMBDA r : r \notin Completed)
    /\ (finishing # {} => Len(queue) > 0 /\ finishing = {Head(queue)})

GapKeepsRecording ==
    /\ (event.kind = "captureEnded" /\ ~event.before[event.id].stopped
          /\ event.retry < MaxRestarts =>
            event.id \in Running /\ records[event.id].phase = "retry")
    /\ (event.kind = "launch" =>
          \A r \in event.running :
            ~event.before[r].stopped /\ event.before[r].retries < MaxRestarts
              => r \in Running /\ records[r].phase = "retry")

CaptureFailureCloses ==
    event.kind = "failed" =>
      /\ event.id \in Closed
      /\ records[event.id].phase = "queued"
      /\ records[event.id].cause = event.reason

AcknowledgedEndsAreRemoved == shown \cap DOMAIN records = {}

CrashKeepsSavedRecord ==
    /\ (event.kind = "crash" =>
          records = event.before /\ queue = event.beforeQueue)
    /\ (~online =>
          capture = NoCapture /\ finishing = {} /\ notice = NoNotice)

\* This is an environmental antecedent, not fairness imposed on the caller.
\* Each outstanding request receives its reply unless that launch dies.
CallerAnswers ==
    /\ \A r \in Recordings, n \in 0..MaxRestarts :
          []<>(~online \/ capture.id # r \/ capture.mode # "start" \/ capture.attempt # n)
    /\ \A r \in Recordings :
          []<>(~online \/ capture.id # r \/ capture.mode # "stop")
    /\ \A r \in Recordings : []<>(~online \/ r \notin finishing)
    /\ \A r \in Recordings : []<>(~online \/ notice.id # r)

StableService == (<>[]online) /\ CallerAnswers

StoppedOrClosedEndsAndIsShown ==
    StableService =>
      \A r \in Recordings : (r \in Stopped \cup Closed) ~> (r \in Completed \cap shown /\ r \notin DOMAIN records)

GapsAreResolved ==
    StableService =>
      \A r \in Recordings, n \in 0..MaxRestarts :
        (r \in Running /\ records[r].phase = "retry" /\ records[r].retries = n)
          ~> (attempts[r] > n + 1 \/ r \in Closed)

HandedOver == \E r \in Completed : Head(ends[r]).outcome = "handed"

HandedOverWithWarning ==
    \E r \in Completed : Head(ends[r]).outcome = "handed" /\ Head(ends[r]).error # NoError

KeptWithError ==
    \E r \in Completed : Head(ends[r]).outcome = "kept" /\ ~Head(ends[r]).empty

NothingRecorded == \E r \in Completed : Head(ends[r]).empty

EndAcknowledged == Accepted # {} /\ DOMAIN records = {} /\ Accepted = shown

StartDuringFinishing == Running # {} /\ finishing # {}

RefusalIsPossible == event.kind = "refused" /\ event.menu = "refused"

StopDuringStarting ==
    capture.mode = "start" /\ capture.id \in Stopped

GapIsPossible == event.kind = "started" /\ event.menu = "gap"

ExhaustedRestarts ==
    \E r \in DOMAIN records : records[r].phase = "queued" /\ records[r].cause = "retryLimit"

CrashWithUnshownEnd == ~online /\ Completed \ shown # {}

CrashDuringRestart ==
    ~online /\ \E r \in Running : records[r].phase = "starting" /\ records[r].retries > 0

\* The restart event retains the immediately preceding launch's saved
\* snapshot solely to witness a crash-interrupted restart followed by another
\* request. A pre-launch starting phase proves no start reply arrived before
\* the crash; the positive retry ordinal proves it was already a restart.
RestartAfterInterruptedRestart ==
    /\ event.kind = "restart"
    /\ event.id \in DOMAIN event.before
    /\ event.before[event.id].phase = "starting"
    /\ event.before[event.id].retries > 0
    /\ event.retry = event.before[event.id].retries
    /\ event.id \in Running
    /\ records[event.id].retries = event.retry + 1
    /\ capture.id = event.id /\ capture.mode = "start"
    /\ capture.attempt = event.retry + 1

\* Initial state is a fresh launch. The finite identity supply bounds accepted
\* Starts, not simultaneous unfinished recordings. No identity is reused.
Init ==
    /\ records = [r \in {} |-> Fresh] /\ queue = <<>> /\ online = TRUE
    /\ capture = NoCapture /\ finishing = {} /\ notice = NoNotice
    /\ attempts = [r \in Recordings |-> 0]
    /\ stopAt = [r \in Recordings |-> -1]
    /\ ends = [r \in Recordings |-> <<>>] /\ shown = {} /\ closedOrder = <<>>
    /\ event = NoEvent

Start(r) ==
    /\ online /\ r \notin Accepted /\ Running = {}
    /\ records' = [x \in DOMAIN records \cup {r} |-> IF x = r THEN Fresh ELSE records[x]]
    /\ capture' = [id |-> r, mode |-> "start", attempt |-> 0, target |-> r]
    /\ attempts' = [attempts EXCEPT ![r] = 1]
    /\ event' = [NoEvent EXCEPT !.kind = "accepted", !.id = r, !.running = Running]
    /\ UNCHANGED <<queue, online, finishing, notice, stopAt, ends, shown, closedOrder>>

RefuseStart(r) ==
    /\ online /\ r \notin Accepted /\ Running # {}
    /\ event' = [NoEvent EXCEPT !.kind = "refused", !.id = r,
                    !.before = records, !.beforeQueue = queue,
                    !.running = Running, !.menu = "refused"]
    /\ UNCHANGED <<records, queue, online, capture, finishing, notice,
                   attempts, stopAt, ends, shown, closedOrder>>

\* None means the person's untargeted Stop (including Quit). An automatic
\* Stop names r and carries its caller-provided reason. A stale name is inert.
Stop(r, reason) ==
    LET target == IF r = None /\ Running # {} THEN CHOOSE x \in Running : TRUE ELSE r
    IN /\ online
       /\ records' = IF target \in Running
                       THEN [records EXCEPT ![target].stopped = TRUE]
                       ELSE records
       /\ stopAt' = IF target \in Running /\ target \notin Stopped
                      THEN [stopAt EXCEPT ![target] = attempts[target]] ELSE stopAt
       /\ event' = [NoEvent EXCEPT !.kind = "stop", !.id = target,
                       !.before = records, !.beforeQueue = queue, !.running = Running,
                       !.reason = reason,
                       !.menu = IF target \in Running /\ reason \in Errors
                                  THEN "automaticStop" ELSE None]
       /\ UNCHANGED <<queue, online, capture, finishing, notice,
                      attempts, ends, shown, closedOrder>>

CaptureStarted(r) ==
    /\ online /\ capture.id = r /\ capture.mode = "start"
    /\ records' = [records EXCEPT ![r].phase = "live"]
    /\ capture' = [capture EXCEPT !.mode = IF records[r].stopped THEN "stop" ELSE "live"]
    /\ event' = [NoEvent EXCEPT !.kind = "started", !.id = r,
                    !.retry = records[r].retries,
                    !.menu = IF records[r].retries > 0 THEN "gap" ELSE None]
    /\ UNCHANGED <<queue, online, finishing, notice, attempts, stopAt, ends, shown, closedOrder>>

CaptureFailed(r, e) ==
    /\ online /\ capture.id = r /\ capture.mode = "start"
    /\ records' = [records EXCEPT ![r] = Queued(e)]
    /\ queue' = Append(queue, r)
    /\ closedOrder' = Append(closedOrder, [id |-> r, cause |-> e])
    /\ capture' = NoCapture
    /\ attempts' = [attempts EXCEPT ![r] = 1]
    /\ stopAt' = [stopAt EXCEPT ![r] = IF r \in Stopped THEN 1 ELSE -1]
    /\ event' = [NoEvent EXCEPT !.kind = "failed", !.id = r, !.reason = e]
    /\ UNCHANGED <<online, finishing, notice, ends, shown>>

RequestStop(r) ==
    /\ online /\ r \in Running /\ records[r].stopped
    /\ capture.id = r /\ capture.mode = "live"
    /\ capture' = [capture EXCEPT !.mode = "stop"] /\ event' = NoEvent
    /\ UNCHANGED <<records, queue, online, finishing, notice,
                   attempts, stopAt, ends, shown, closedOrder>>

CaptureEnded(r) ==
    /\ online /\ capture.id = r /\ capture.mode \in {"live", "stop"}
    /\ records' = IF records[r].stopped
                    THEN [records EXCEPT ![r] = Queued("stop")]
                    ELSE [records EXCEPT ![r].phase = "retry"]
    /\ queue' = IF records[r].stopped THEN Append(queue, r) ELSE queue
    /\ closedOrder' = IF records[r].stopped
                         THEN Append(closedOrder, [id |-> r, cause |-> "stop"])
                         ELSE closedOrder
    /\ capture' = NoCapture
    /\ attempts' = IF records[r].stopped THEN [attempts EXCEPT ![r] = 1] ELSE attempts
    /\ stopAt' = IF records[r].stopped THEN [stopAt EXCEPT ![r] = 1] ELSE stopAt
    /\ event' = [NoEvent EXCEPT !.kind = "captureEnded", !.id = r,
                    !.before = records, !.retry = records[r].retries]
    /\ UNCHANGED <<online, finishing, notice, ends, shown>>

ResumeOrClose(r) ==
    /\ online /\ r \in Running /\ records[r].phase = "retry"
    /\ IF records[r].stopped \/ records[r].retries = MaxRestarts
          THEN /\ records' = [records EXCEPT ![r] =
                    Queued(IF records[r].stopped THEN "stop" ELSE "retryLimit")]
               /\ queue' = Append(queue, r)
               /\ closedOrder' = Append(closedOrder,
                    [id |-> r, cause |-> IF records[r].stopped THEN "stop" ELSE "retryLimit"])
               /\ attempts' = [attempts EXCEPT ![r] = 1]
               /\ stopAt' = [stopAt EXCEPT ![r] = IF r \in Stopped THEN 1 ELSE -1]
               /\ event' = NoEvent /\ UNCHANGED capture
          ELSE /\ records' = [records EXCEPT ![r].phase = "starting", ![r].retries = @ + 1]
               /\ attempts' = [attempts EXCEPT ![r] = @ + 1]
               /\ capture' = [id |-> r, mode |-> "start",
                               attempt |-> records[r].retries + 1, target |-> r]
               /\ event' = [NoEvent EXCEPT !.kind = "restart", !.id = r,
                               !.retry = records[r].retries,
                               !.before = IF event.kind = "launch"
                                            THEN event.before ELSE NoEvent.before]
               /\ UNCHANGED <<queue, closedOrder, stopAt>>
    /\ UNCHANGED <<online, finishing, notice, ends, shown>>

RequestFinish ==
    /\ online /\ finishing = {} /\ Len(queue) > 0
    /\ finishing' = {Head(queue)}
    /\ event' = [NoEvent EXCEPT !.kind = "finish", !.id = Head(queue)]
    /\ UNCHANGED <<records, queue, online, capture, notice,
                   attempts, stopAt, ends, shown, closedOrder>>

\* An empty recording's reply includes both 'nothing recorded' and why.
\* No filesystem operation is modeled; these are the caller's reported facts.
FinishReports(r) ==
    {v \in Results : ~v.empty \/ v.error = NothingError(records[r].cause)}

FinishReported(r, v) ==
    /\ online /\ r \in finishing /\ v \in FinishReports(r)
    /\ records' = [records EXCEPT ![r] = Ended(v)]
    /\ ends' = [ends EXCEPT ![r] = Append(@, v)]
    /\ queue' = Tail(queue) /\ finishing' = {} /\ event' = NoEvent
    /\ UNCHANGED <<online, capture, notice, attempts, stopAt, shown, closedOrder>>

RequestEndNotice(r) ==
    /\ online /\ r \in DOMAIN records /\ records[r].phase = "ended"
    /\ notice.id = None
    /\ notice' = [id |-> r, result |-> records[r].result, directory |-> r]
    /\ event' = NoEvent
    /\ UNCHANGED <<records, queue, online, capture, finishing,
                   attempts, stopAt, ends, shown, closedOrder>>

\* This input acknowledges actual display and commits its removal. If the
\* caller dies after displaying but before reporting it, no input occurred:
\* the saved end is requested again, and displaying it twice is harmless.
MenuSaid(r) ==
    /\ online /\ notice.id = r
    /\ records' = [x \in DOMAIN records \ {r} |-> records[x]]
    /\ notice' = NoNotice
    /\ shown' = shown \cup {r} /\ event' = NoEvent
    /\ UNCHANGED <<queue, online, capture, finishing,
                   attempts, stopAt, ends, closedOrder>>

Crash ==
    /\ online /\ online' = FALSE
    /\ capture' = NoCapture /\ finishing' = {} /\ notice' = NoNotice
    /\ event' = [NoEvent EXCEPT !.kind = "crash", !.before = records, !.beforeQueue = queue]
    /\ UNCHANGED <<records, queue, attempts, stopAt, ends, shown, closedOrder>>

Launch ==
    /\ ~online /\ online' = TRUE
    /\ records' = [r \in DOMAIN records |->
          IF r \in Running THEN [records[r] EXCEPT !.phase = "retry"] ELSE records[r]]
    /\ event' = [NoEvent EXCEPT !.kind = "launch", !.before = records, !.running = Running]
    /\ UNCHANGED <<queue, capture, finishing, notice, attempts, stopAt, ends, shown, closedOrder>>

Idle == UNCHANGED vars

Next ==
    \/ \E r \in Recordings : Start(r) \/ RefuseStart(r)
    \/ Stop(None, None)
    \/ \E r \in Recordings, e \in Errors : Stop(r, e)
    \/ \E r \in Recordings : CaptureStarted(r) \/ RequestStop(r) \/ CaptureEnded(r)
                              \/ ResumeOrClose(r) \/ RequestEndNotice(r) \/ MenuSaid(r)
    \/ \E r \in Recordings, e \in Errors : CaptureFailed(r, e)
    \/ \E r \in Recordings, v \in Results : FinishReported(r, v)
    \/ RequestFinish \/ Crash \/ Launch \/ Idle

ResolveGapFair == \A r \in Recordings : WF_vars(ResumeOrClose(r))

SendStopFair == \A r \in Recordings : WF_vars(RequestStop(r))

FinishDispatchFair == WF_vars(RequestFinish)

EndNoticeDispatchFair == \A r \in Recordings : WF_vars(RequestEndNotice(r))

\* This accepts a second Start and overwrites the capture slot without checking
\* whether another recording is still running. It is deliberately not in Next.
StartUnchecked ==
    \E r \in Recordings :
      /\ online /\ r \notin Accepted /\ Running # {}
      /\ records' = [x \in DOMAIN records \cup {r} |-> IF x = r THEN Fresh ELSE records[x]]
      /\ capture' = [id |-> r, mode |-> "start", attempt |-> 0, target |-> r]
      /\ attempts' = [attempts EXCEPT ![r] = 1]
      /\ event' = [NoEvent EXCEPT !.kind = "accepted", !.id = r, !.running = Running]
      /\ UNCHANGED <<queue, online, finishing, notice, stopAt, ends, shown, closedOrder>>

ForgetWhenFinishRequested ==
    /\ online /\ finishing = {} /\ Len(queue) > 0
    /\ records' = [r \in DOMAIN records \ {Head(queue)} |-> records[r]]
    /\ finishing' = {Head(queue)}
    /\ event' = [NoEvent EXCEPT !.kind = "finish", !.id = Head(queue)]
    /\ UNCHANGED <<queue, online, capture, notice,
                   attempts, stopAt, ends, shown, closedOrder>>

ForgetWhenNoticeRequested ==
    \E r \in DOMAIN records :
      /\ online /\ records[r].phase = "ended" /\ r \notin shown
      /\ notice.id = None
      /\ records' = [x \in DOMAIN records \ {r} |-> records[x]]
      /\ notice' = [id |-> r, result |-> records[r].result, directory |-> r]
      /\ event' = NoEvent
      /\ UNCHANGED <<queue, online, capture, finishing,
                     attempts, stopAt, ends, shown, closedOrder>>

\* Today's recorder refuses Start while the previous recording is joining.
\* The guard excludes the legitimate refusal, so this step has a unique
\* action name and recording argument for replay against that behavior.
RefuseStartWhileFinishingStep(r) ==
    /\ online /\ r \notin Accepted /\ Running = {} /\ finishing # {}
    /\ event' = [NoEvent EXCEPT !.kind = "refused", !.id = r,
                    !.before = records, !.beforeQueue = queue,
                    !.running = Running, !.menu = "refused"]
    /\ UNCHANGED <<records, queue, online, capture, finishing, notice,
                   attempts, stopAt, ends, shown, closedOrder>>

RefuseStartWhileFinishing ==
    \E r \in Recordings : RefuseStartWhileFinishingStep(r)

\* This faulty dispatch exposes a stop request before committing the Stop.
StopBeforeSavedStep(r) ==
    /\ online /\ r \in Running /\ r \notin Stopped
    /\ capture.id = r /\ capture.mode = "live"
    /\ capture' = [capture EXCEPT !.mode = "stop"]
    /\ event' = [NoEvent EXCEPT !.kind = "stop", !.id = r,
                    !.running = Running, !.before = records, !.beforeQueue = queue]
    /\ UNCHANGED <<records, queue, online, finishing, notice,
                   attempts, stopAt, ends, shown, closedOrder>>

StopBeforeSaved == \E r \in Recordings : StopBeforeSavedStep(r)

IgnoreStopDuringStartingStep(r) ==
    /\ online /\ r \in Running
    /\ capture.id = r /\ capture.mode = "start" /\ r \notin Stopped
    /\ event' = [NoEvent EXCEPT !.kind = "stop", !.id = r,
                    !.running = Running, !.before = records, !.beforeQueue = queue]
    /\ UNCHANGED <<records, queue, online, capture, finishing, notice,
                   attempts, stopAt, ends, shown, closedOrder>>

IgnoreStopDuringStarting ==
    \E r \in Recordings : IgnoreStopDuringStartingStep(r)

FinishWhileWriting ==
    /\ online /\ capture.mode = "live" /\ finishing = {}
    /\ finishing' = {capture.id} /\ event' = NoEvent
    /\ UNCHANGED <<records, queue, online, capture, notice,
                   attempts, stopAt, ends, shown, closedOrder>>

Spec == Init /\ [][Next]_vars
====
