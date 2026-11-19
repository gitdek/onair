---- MODULE RecordingPieces ----
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS MaxPieces, InterruptionLimit, Durations, RecordingId, SpoolPath
VARIABLES online, phase, capture, open, disk, durable, contents, durations,
          known, sealed, captureFault, joinInput, partial, prepared, omitted,
          gaps, joined, fileOmitted, interruptions, interruptionEvidence,
          handoverKey, handoverTarget, delivered, handedOver, retentionElapsed,
          error, errorAt, retryPending, recoveryKind, recoveryReply,
          nextNumber, flushReported, recoveredAtLimit

vars == <<online, phase, capture, open, disk, durable, contents, durations,
          known, sealed, captureFault, joinInput, partial, prepared, omitted,
          gaps, joined, fileOmitted, interruptions, interruptionEvidence,
          handoverKey, handoverTarget, delivered, handedOver, retentionElapsed,
          error, errorAt, retryPending, recoveryKind, recoveryReply,
          nextNumber, flushReported, recoveredAtLimit>>

Pieces == 1..MaxPieces
Phases == {"Capture", "Write", "Name", "DiscardPiece", "JoinReady", "Join",
           "PublishJoin", "DiscardJoinRetry", "DiscardJoinFailed", "JoinFailed",
           "HandoverReady", "Handover", "HandoverFailed", "Retained", "Remove",
           "Done", "Empty"}
Errors == {"", "write failed", "join failed", "none readable",
           "interruption limit", "handover failed"}
Named == {p \in Pieces : disk[p] = "named"}
ClosedOrder == [i \in 1..Cardinality(sealed) |-> i]
ZeroGaps == [p \in Pieces |-> 0]
GapReport(missing) == [p \in Pieces |-> IF p \in missing THEN durations[p] ELSE 0]
MayRemove == handedOver /\ retentionElapsed
FinishPhases == {"JoinReady", "Join", "PublishJoin", "DiscardJoinRetry",
                 "DiscardJoinFailed", "JoinFailed", "HandoverReady", "Handover",
                 "HandoverFailed", "Retained", "Remove", "Done", "Empty"}

\* One instance owns one recording and its stable spool location.  The piece
\* bound limits the exploration, not the implementation's recording length.
\* disk, durable, contents, partial, joined, fileOmitted and delivered abstract
\* caller-owned effects.  sealed and interruptionEvidence are history used only
\* for verification; recoveryKind and recoveryReply record the last reconciliation
\* decision for checking its response.  The other fields form the atomically saved bookkeeping.
\* nextNumber and flushReported are saved bookkeeping: the next number is
\* reconciled from the launch listing, and a flush report authorizes naming.
\* recoveredAtLimit is verification history of the last launch's input: it
\* records whether that launch found an interrupted join at the cumulative limit,
\* independently of the recovery decision made in response.
\* Each phase requesting an effect is saved before any caller action can run.
\* Effect and report guards describe truthful caller behavior, not disk reads
\* by the bookkeeping.  AcceptPiece uses only its saved record; NamePiece
\* carries out the name request without checking whether bytes were flushed.
\* A hidden piece may survive a crash in any write state; recovery discards it.
\* Durations are supplied metadata; this model does not inspect media bytes.
TypeOK ==
    /\ nextNumber \in 1..(MaxPieces + 1)
    /\ flushReported \in BOOLEAN
    /\ recoveredAtLimit \in BOOLEAN
    /\ retryPending \in BOOLEAN
    /\ recoveryKind \in {"none", "named piece", "named join", "interrupted join", "interruption limit",
                         "handover", "failed join", "failed handover"}
    /\ recoveryReply \in Phases \cup {"none"}
    /\ online \in BOOLEAN
    /\ phase \in Phases
    /\ capture \in {"active", "paused", "failed", "final"}
    /\ open \in 0..MaxPieces
    /\ disk \in [Pieces -> {"absent", "hidden", "named"}]
    /\ durable \subseteq Pieces
    /\ contents \in [Pieces -> 0..2]
    /\ durations \in [Pieces -> (Durations \cup {0})]
    /\ known \subseteq Pieces
    /\ sealed \subseteq Pieces
    /\ captureFault \in BOOLEAN
    /\ joinInput \in UNION {[1..n -> Pieces] : n \in 0..MaxPieces}
    /\ partial \in BOOLEAN
    /\ prepared \in BOOLEAN
    /\ omitted \subseteq Pieces
    /\ gaps \in [Pieces -> (Durations \cup {0})]
    /\ joined \in BOOLEAN
    /\ fileOmitted \subseteq Pieces
    /\ interruptions \in 0..InterruptionLimit
    /\ interruptionEvidence \in 0..InterruptionLimit
    /\ handoverKey = RecordingId
    /\ handoverTarget \in 0..MaxPieces
    /\ delivered \subseteq 0..MaxPieces
    /\ handedOver \in BOOLEAN
    /\ retentionElapsed \in BOOLEAN
    /\ error \in Errors
    /\ errorAt \in {"", SpoolPath}

ClosedPiecesPreserved ==
    ~MayRemove => \A p \in sealed : disk[p] = "named" /\ contents[p] = 1

WholePiecesAreDurable == Named \subseteq durable

NamingRequiresFlushReport ==
    phase = "Name" => flushReported

OnlyNamedPiecesAreWhole ==
    ~MayRemove => known \subseteq Named

SettledInventoryMatchesDisk ==
    online /\ phase \notin {"Name", "Remove"} => known = Named

ClosedNumbersNeverReused ==
    /\ sealed = 1..Cardinality(sealed)
    /\ phase = "Write" => open = Cardinality(sealed) + 1 /\ open \notin sealed

OnlyOneOpenPiece ==
    /\ {p \in Pieces : disk[p] = "hidden"} \subseteq {open}
    /\ phase \notin {"Write", "Name", "DiscardPiece"} => open = 0
    /\ open # 0 => open = Cardinality(known) + 1

NoMoreFromFailedCapture == captureFault => capture \in {"failed", "final"}

JoinUsesEveryClosedPieceInOrder ==
    joinInput # <<>> => joinInput = ClosedOrder

JoinWaitsForFinalCapture ==
    phase \in FinishPhases => capture = "final" /\ open = 0

JoinRequestHasOnlyWholePieces ==
    phase \in {"JoinReady", "Join", "PublishJoin", "DiscardJoinRetry",
                "DiscardJoinFailed", "JoinFailed"}
        => /\ sealed # {}
           /\ Named = sealed
           /\ (~joined \/ phase = "PublishJoin")

JoinPublicationNeedsSavedManifest ==
    joined => prepared /\ omitted = fileOmitted /\ omitted \subseteq sealed
              /\ omitted # sealed /\ gaps = GapReport(omitted)

EveryOmissionHasItsDuration ==
    prepared => omitted \subseteq sealed /\ omitted # sealed
                /\ gaps = GapReport(omitted)
                /\ \A p \in omitted : gaps[p] \in Durations

OneJoinedFileOnly ==
    /\ handoverTarget = 0
    /\ delivered \subseteq {0}
    /\ delivered # {} => joined
    /\ phase = "Handover" => joined /\ handoverKey = RecordingId

HandoverRequiresSuccess ==
    /\ handedOver => delivered = {0} /\ joined
    /\ phase \in {"Retained", "Remove", "Done"} => handedOver
    /\ retentionElapsed => handedOver

RemovalWaitsThreeDaysAfterHandover ==
    phase \in {"Remove", "Done"} => MayRemove

FailedJoinKeepsRecording ==
    phase \in {"JoinFailed", "DiscardJoinFailed"}
        => ~joined /\ ~handedOver /\ delivered = {} /\ Named = sealed
           /\ error \in {"join failed", "none readable", "interruption limit"}

FailedHandoverKeepsRecording ==
    phase = "HandoverFailed"
        => ~handedOver /\ joined /\ Named = sealed /\ error = "handover failed"

ErrorsIdentifySpool == error # "" => errorAt = SpoolPath

InterruptionCountSurvivesRetries ==
    online => interruptions = interruptionEvidence

InterruptedJoinStopsAtLimit ==
    /\ phase = "DiscardJoinFailed"
        => interruptions = InterruptionLimit /\ error = "interruption limit"
    /\ recoveredAtLimit
        => recoveryKind = "interruption limit" /\ recoveryReply = "DiscardJoinFailed"

EmptyMeansNothingRecorded ==
    phase = "Empty" => sealed = {} /\ ~joined /\ delivered = {} /\ ~handedOver

NamedPieceRecoveryUsesDisk ==
    recoveryKind = "named piece" => recoveryReply = "Capture"

NamedJoinRecoveryUsesExistingFile ==
    recoveryKind = "named join" => recoveryReply = "HandoverReady"

InterruptedJoinRecovery ==
    /\ recoveryKind = "interrupted join" => recoveryReply = "DiscardJoinRetry"
    /\ recoveryKind = "interruption limit" => recoveryReply = "DiscardJoinFailed"

FailedJoinRetriesOnLaunch ==
    recoveryKind = "failed join" => recoveryReply = "DiscardJoinRetry"

HandoverRetriesOnLaunch ==
    recoveryKind \in {"handover", "failed handover"} => recoveryReply = "HandoverReady"

RetryRequestRemainsPending ==
    retryPending => phase \in {"JoinFailed", "HandoverFailed"}

PersonRetryProgress ==
    (online /\ retryPending)
        ~> (~online \/ phase \in {"DiscardJoinRetry", "HandoverReady"})

JoinRequestProgress ==
    (online /\ phase = "JoinReady") ~> (~online \/ phase = "Join")
HandoverRequestProgress ==
    (online /\ phase = "HandoverReady") ~> (~online \/ phase = "Handover")
FinalCaptureProgress ==
    (online /\ phase = "Capture" /\ capture = "final")
        ~> (~online \/ phase \in {"JoinReady", "Empty"})
RetentionRequestProgress ==
    (online /\ phase = "Retained" /\ retentionElapsed)
        ~> (~online \/ phase = "Remove")

SuccessfulHandover == handedOver /\ omitted = {} /\ sealed # {}
HandoverWithGap == handedOver /\ omitted # {} /\ omitted # sealed
WriteFailureKept == captureFault /\ phase = "Capture" /\ sealed # {} /\ Named = sealed
JoinFailureKept == phase = "JoinFailed" /\ error = "join failed" /\ Named = sealed
NoneReadableKept == phase = "JoinFailed" /\ error = "none readable" /\ Named = sealed
HandoverFailureKept == phase = "HandoverFailed" /\ Named = sealed
NothingRecorded == phase = "Empty"
RetentionCompleted == phase = "Done" /\ Named = {} /\ sealed # {}
InterruptionLimitReached == phase = "JoinFailed" /\ error = "interruption limit"
SuccessAfterInterruptionLimit == handedOver /\ interruptions = InterruptionLimit
NamedPieceSurvivesUnrecordedCrash == ~online /\ Named # known /\ phase = "Name"
NamedJoinSurvivesUnrecordedCrash == ~online /\ phase = "PublishJoin" /\ joined
UnresolvedHandoverSurvivesCrash == ~online /\ phase = "Handover" /\ delivered = {0}
CarriedOnWithNextPiece ==
    phase = "Write" /\ open = 2 /\ known = {1} /\ recoveryKind = "named piece"
PersonRetryReceived == retryPending
NamedJoinRecovered == online /\ recoveryKind = "named join" /\ joined

Init ==
    /\ nextNumber = 1
    /\ flushReported = FALSE
    /\ recoveredAtLimit = FALSE
    /\ retryPending = FALSE
    /\ recoveryKind = "none"
    /\ recoveryReply = "none"
    /\ online = TRUE
    /\ phase = "Capture"
    /\ capture = "active"
    /\ open = 0
    /\ disk = [p \in Pieces |-> "absent"]
    /\ durable = {}
    /\ contents = [p \in Pieces |-> 0]
    /\ durations = [p \in Pieces |-> 0]
    /\ known = {}
    /\ sealed = {}
    /\ captureFault = FALSE
    /\ joinInput = <<>>
    /\ partial = FALSE
    /\ prepared = FALSE
    /\ omitted = {}
    /\ gaps = ZeroGaps
    /\ joined = FALSE
    /\ fileOmitted = {}
    /\ interruptions = 0
    /\ interruptionEvidence = 0
    /\ handoverKey = RecordingId
    /\ handoverTarget = 0
    /\ delivered = {}
    /\ handedOver = FALSE
    /\ retentionElapsed = FALSE
    /\ error = ""
    /\ errorAt = ""

AcceptPiece(seconds) ==
    /\ online /\ phase = "Capture" /\ capture = "active" /\ ~captureFault
    /\ Cardinality(known) < MaxPieces
    /\ open' = nextNumber
    /\ durations' = [durations EXCEPT ![nextNumber] = seconds]
    /\ flushReported' = FALSE
    /\ phase' = "Write"
    /\ UNCHANGED <<nextNumber, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, disk, durable, contents, known, sealed,
                   captureFault, joinInput, partial, prepared, omitted, gaps,
                   joined, fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

WriteHiddenBytes ==
    /\ online /\ phase = "Write" /\ disk[open] = "absent"
    /\ disk' = [disk EXCEPT ![open] = "hidden"]
    /\ contents' = [contents EXCEPT ![open] = 1]
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, phase, capture, open, durable, durations, known,
                   sealed, captureFault, joinInput, partial, prepared, omitted,
                   gaps, joined, fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

FlushPiece ==
    /\ online /\ phase = "Write" /\ disk[open] = "hidden" /\ open \notin durable
    /\ durable' = durable \cup {open}
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, phase, capture, open, disk, contents, durations, known,
                   sealed, captureFault, joinInput, partial, prepared, omitted,
                   gaps, joined, fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

ReportPieceFlushed ==
    /\ online /\ phase = "Write" /\ open \in durable
    /\ flushReported' = TRUE
    /\ phase' = "Name"
    /\ UNCHANGED <<nextNumber, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, prepared,
                   omitted, gaps, joined, fileOmitted, interruptions,
                   interruptionEvidence, handoverKey, handoverTarget, delivered,
                   handedOver, retentionElapsed, error, errorAt>>

NamePiece ==
    /\ online /\ phase = "Name" /\ disk[open] = "hidden"
    /\ disk' = [disk EXCEPT ![open] = "named"]
    /\ sealed' = sealed \cup {open}
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, phase, capture, open, durable, contents, durations,
                   known, captureFault, joinInput, partial, prepared, omitted,
                   gaps, joined, fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

ReportPieceNamed ==
    /\ online /\ phase = "Name" /\ open \in Named
    /\ known' = known \cup {open}
    /\ nextNumber' = open + 1
    /\ open' = 0
    /\ phase' = "Capture"
    /\ UNCHANGED <<flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, disk, durable, contents, durations, sealed,
                   captureFault, joinInput, partial, prepared, omitted, gaps,
                   joined, fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

ReportWriteFailure ==
    /\ online /\ phase \in {"Write", "Name"} /\ open \notin Named
    /\ capture' = IF capture = "final" THEN "final" ELSE "failed"
    /\ captureFault' = TRUE
    /\ phase' = "DiscardPiece"
    /\ error' = "write failed"
    /\ errorAt' = SpoolPath
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, open, disk, durable, contents, durations, known,
                   sealed, joinInput, partial, prepared, omitted, gaps, joined,
                   fileOmitted, interruptions, interruptionEvidence, handoverKey,
                   handoverTarget, delivered, handedOver, retentionElapsed>>

DiscardOpenRemnant ==
    /\ online /\ phase = "DiscardPiece" /\ disk[open] = "hidden"
    /\ disk' = [disk EXCEPT ![open] = "absent"]
    /\ durable' = durable \ {open}
    /\ contents' = [contents EXCEPT ![open] = 0]
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, phase, capture, open, durations, known, sealed,
                   captureFault, joinInput, partial, prepared, omitted, gaps,
                   joined, fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

ReportRemnantDiscarded ==
    /\ online /\ phase = "DiscardPiece" /\ disk[open] = "absent"
    /\ open' = 0
    /\ phase' = "Capture"
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, disk, durable, contents, durations, known,
                   sealed, captureFault, joinInput, partial, prepared, omitted,
                   gaps, joined, fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

CaptureEnded ==
    /\ online /\ capture = "active" /\ phase \in {"Capture", "Write", "Name"}
    /\ capture' = "paused"
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, phase, open, disk, durable, contents, durations, known,
                   sealed, captureFault, joinInput, partial, prepared, omitted,
                   gaps, joined, fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

CarryOn ==
    /\ online /\ phase = "Capture" /\ capture = "paused" /\ ~captureFault
    /\ capture' = "active"
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, phase, open, disk, durable, contents, durations, known,
                   sealed, captureFault, joinInput, partial, prepared, omitted,
                   gaps, joined, fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

FinishCapture ==
    /\ online /\ phase \in {"Capture", "Write", "Name", "DiscardPiece"}
    /\ capture # "final"
    /\ capture' = "final"
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, phase, open, disk, durable, contents, durations, known,
                   sealed, captureFault, joinInput, partial, prepared, omitted,
                   gaps, joined, fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

FinalizeCapture ==
    /\ online /\ phase = "Capture" /\ capture = "final" /\ open = 0
    /\ phase' = IF known = {} THEN "Empty" ELSE "JoinReady"
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, prepared,
                   omitted, gaps, joined, fileOmitted, interruptions,
                   interruptionEvidence, handoverKey, handoverTarget, delivered,
                   handedOver, retentionElapsed, error, errorAt>>

RequestJoin ==
    /\ online /\ phase = "JoinReady" /\ ~joined /\ ~partial
    /\ phase' = "Join"
    /\ joinInput' = [i \in 1..Cardinality(known) |-> i]
    /\ prepared' = FALSE
    /\ omitted' = {}
    /\ gaps' = ZeroGaps
    /\ error' = ""
    /\ errorAt' = ""
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, partial, joined, fileOmitted,
                   interruptions, interruptionEvidence, handoverKey,
                   handoverTarget, delivered, handedOver, retentionElapsed>>

BeginJoinOutput ==
    /\ online /\ phase = "Join" /\ ~partial /\ ~joined
    /\ partial' = TRUE
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, phase, capture, open, disk, durable, contents,
                   durations, known, sealed, captureFault, joinInput, prepared,
                   omitted, gaps, joined, fileOmitted, interruptions,
                   interruptionEvidence, handoverKey, handoverTarget, delivered,
                   handedOver, retentionElapsed, error, errorAt>>

ReportJoinPrepared(missing) ==
    /\ online /\ phase = "Join" /\ partial /\ ~joined
    /\ missing \subseteq known /\ missing # known
    /\ phase' = "PublishJoin"
    /\ prepared' = TRUE
    /\ omitted' = missing
    /\ gaps' = GapReport(missing)
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, joined,
                   fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

NameJoinedFile ==
    /\ online /\ phase = "PublishJoin" /\ partial /\ prepared /\ ~joined
    /\ joined' = TRUE
    /\ partial' = FALSE
    /\ fileOmitted' = omitted
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, phase, capture, open, disk, durable, contents,
                   durations, known, sealed, captureFault, joinInput, prepared,
                   omitted, gaps, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

ReportJoinNamed ==
    /\ online /\ phase = "PublishJoin" /\ joined
    /\ phase' = "HandoverReady"
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, prepared,
                   omitted, gaps, joined, fileOmitted, interruptions,
                   interruptionEvidence, handoverKey, handoverTarget, delivered,
                   handedOver, retentionElapsed, error, errorAt>>

ReportJoinFailure(reason) ==
    /\ online /\ phase \in {"Join", "PublishJoin"} /\ ~joined
    /\ reason \in {"join failed", "none readable"}
    /\ reason = "none readable" => phase = "Join" /\ partial
    /\ phase' = "JoinFailed"
    /\ prepared' = FALSE
    /\ omitted' = {}
    /\ gaps' = ZeroGaps
    /\ error' = reason
    /\ errorAt' = SpoolPath
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, joined,
                   fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed>>

AskRetry ==
    /\ online /\ phase \in {"JoinFailed", "HandoverFailed"}
    /\ ~retryPending
    /\ retryPending' = TRUE
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, recoveryKind, recoveryReply, online, phase, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, prepared,
                   omitted, gaps, joined, fileOmitted, interruptions,
                   interruptionEvidence, handoverKey, handoverTarget, delivered,
                   handedOver, retentionElapsed, error, errorAt>>

ScheduleRetry ==
    /\ online /\ retryPending /\ phase \in {"JoinFailed", "HandoverFailed"}
    /\ phase' = IF phase = "JoinFailed" THEN "DiscardJoinRetry" ELSE "HandoverReady"
    /\ retryPending' = FALSE
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, prepared, omitted,
                   gaps, joined, fileOmitted, interruptions, interruptionEvidence, handoverKey, handoverTarget,
                   delivered, handedOver, retentionElapsed, error, errorAt, recoveryKind, recoveryReply>>

DiscardPartialJoin ==
    /\ online /\ phase \in {"DiscardJoinRetry", "DiscardJoinFailed"} /\ partial
    /\ partial' = FALSE
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, phase, capture, open, disk, durable, contents,
                   durations, known, sealed, captureFault, joinInput, prepared,
                   omitted, gaps, joined, fileOmitted, interruptions,
                   interruptionEvidence, handoverKey, handoverTarget, delivered,
                   handedOver, retentionElapsed, error, errorAt>>

ReportPartialJoinDiscarded ==
    /\ online /\ phase \in {"DiscardJoinRetry", "DiscardJoinFailed"} /\ ~partial
    /\ phase' = IF phase = "DiscardJoinRetry" THEN "JoinReady" ELSE "JoinFailed"
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, prepared,
                   omitted, gaps, joined, fileOmitted, interruptions,
                   interruptionEvidence, handoverKey, handoverTarget, delivered,
                   handedOver, retentionElapsed, error, errorAt>>

RequestHandover ==
    /\ online /\ phase = "HandoverReady" /\ joined
    /\ phase' = "Handover"
    /\ handoverKey' = RecordingId
    /\ handoverTarget' = 0
    /\ error' = ""
    /\ errorAt' = ""
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, prepared,
                   omitted, gaps, joined, fileOmitted, interruptions,
                   interruptionEvidence, delivered, handedOver, retentionElapsed>>

DeliverJoinedFile ==
    /\ online /\ phase = "Handover" /\ joined
    /\ delivered' = delivered \cup {handoverTarget}
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, phase, capture, open, disk, durable, contents,
                   durations, known, sealed, captureFault, joinInput, partial,
                   prepared, omitted, gaps, joined, fileOmitted, interruptions,
                   interruptionEvidence, handoverKey, handoverTarget,
                   handedOver, retentionElapsed, error, errorAt>>

ReportHandoverSuccess ==
    /\ online /\ phase = "Handover" /\ delivered = {0}
    /\ handedOver' = TRUE
    /\ phase' = "Retained"
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, prepared,
                   omitted, gaps, joined, fileOmitted, interruptions,
                   interruptionEvidence, handoverKey, handoverTarget, delivered,
                   retentionElapsed, error, errorAt>>

ReportHandoverFailure ==
    /\ online /\ phase = "Handover"
    /\ phase' = "HandoverFailed"
    /\ error' = "handover failed"
    /\ errorAt' = SpoolPath
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, prepared,
                   omitted, gaps, joined, fileOmitted, interruptions,
                   interruptionEvidence, handoverKey, handoverTarget, delivered,
                   handedOver, retentionElapsed>>

ReportThreeDaysElapsed ==
    /\ online /\ handedOver /\ ~retentionElapsed
    /\ retentionElapsed' = TRUE
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, phase, capture, open, disk, durable, contents,
                   durations, known, sealed, captureFault, joinInput, partial,
                   prepared, omitted, gaps, joined, fileOmitted, interruptions,
                   interruptionEvidence, handoverKey, handoverTarget, delivered,
                   handedOver, error, errorAt>>

RequestRemoval ==
    /\ online /\ phase = "Retained" /\ MayRemove
    /\ phase' = "Remove"
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, prepared,
                   omitted, gaps, joined, fileOmitted, interruptions,
                   interruptionEvidence, handoverKey, handoverTarget, delivered,
                   handedOver, retentionElapsed, error, errorAt>>

RemovePiece(p) ==
    /\ online /\ phase = "Remove" /\ MayRemove /\ p \in Named
    /\ disk' = [disk EXCEPT ![p] = "absent"]
    /\ durable' = durable \ {p}
    /\ contents' = [contents EXCEPT ![p] = 0]
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, phase, capture, open, durations, known, sealed,
                   captureFault, joinInput, partial, prepared, omitted, gaps,
                   joined, fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

ReportRemovalComplete ==
    /\ online /\ phase = "Remove" /\ Named = {}
    /\ known' = {}
    /\ phase' = "Done"
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, capture, open, disk, durable, contents, durations,
                   sealed, captureFault, joinInput, partial, prepared, omitted,
                   gaps, joined, fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

Crash ==
    /\ online
    /\ online' = FALSE
    /\ interruptionEvidence' =
        IF phase \in {"Join", "PublishJoin"} /\ ~joined
        THEN IF interruptionEvidence < InterruptionLimit
             THEN interruptionEvidence + 1 ELSE interruptionEvidence
        ELSE interruptionEvidence
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, phase, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, prepared,
                   omitted, gaps, joined, fileOmitted, interruptions,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

\* Launch consumes an authoritative disk listing and saves the reconciled
\* record before enabling effects.  Naming the joined file is the commit point.
\* An interruption reaching the bound is shown as failure on this launch;
\* another launch or an explicit retry may start another attempt, without
\* resetting the cumulative, saturated interruption count.
Launch ==
    /\ ~online
    /\ LET lostJoin == phase \in {"Join", "PublishJoin"} /\ ~joined
           count == IF lostJoin /\ interruptions < InterruptionLimit
                    THEN interruptions + 1 ELSE interruptions
       IN /\ online' = TRUE
          /\ retryPending' = FALSE
          /\ recoveryKind' =
              CASE phase \in {"Write", "Name", "DiscardPiece"} ->
                       IF open \in Named THEN "named piece" ELSE "none"
                [] phase \in {"Join", "PublishJoin"} ->
                       IF joined THEN "named join"
                       ELSE IF count >= InterruptionLimit
                            THEN "interruption limit" ELSE "interrupted join"
                [] phase \in {"JoinFailed", "DiscardJoinFailed"} -> "failed join"
                [] phase = "Handover" -> "handover"
                [] phase = "HandoverFailed" -> "failed handover"
                [] OTHER -> "none"
          /\ nextNumber' = Cardinality(Named) + 1
          /\ recoveredAtLimit' = (lostJoin /\ count >= InterruptionLimit)
          /\ known' = Named
          /\ capture' = IF capture = "active" THEN "paused" ELSE capture
          /\ open' = IF open \in Named THEN 0 ELSE open
          /\ interruptions' = count
          /\ phase' =
              CASE phase \in {"Write", "Name", "DiscardPiece"} ->
                       IF open \in Named THEN "Capture" ELSE "DiscardPiece"
                [] phase \in {"Join", "PublishJoin"} ->
                       IF joined THEN "HandoverReady"
                       ELSE IF count >= InterruptionLimit
                            THEN "DiscardJoinFailed" ELSE "DiscardJoinRetry"
                [] phase \in {"JoinFailed", "DiscardJoinFailed"} -> "DiscardJoinRetry"
                [] phase \in {"Handover", "HandoverFailed"} -> "HandoverReady"
                [] OTHER -> phase
          /\ recoveryReply' = IF recoveryKind' = "none" THEN "none" ELSE phase'
          /\ prepared' = IF lostJoin THEN FALSE ELSE prepared
          /\ omitted' = IF lostJoin THEN {} ELSE omitted
          /\ gaps' = IF lostJoin THEN ZeroGaps ELSE gaps
          /\ error' = IF lostJoin /\ count >= InterruptionLimit
                       THEN "interruption limit" ELSE error
          /\ errorAt' = IF lostJoin /\ count >= InterruptionLimit
                         THEN SpoolPath ELSE errorAt
    /\ UNCHANGED <<flushReported, disk, durable, contents, durations, sealed, captureFault,
                   joinInput, partial, joined, fileOmitted, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver, retentionElapsed>>

Finished == online /\ phase \in {"Done", "Empty"} /\ UNCHANGED vars

Next ==
    \/ \E seconds \in Durations : AcceptPiece(seconds)
    \/ WriteHiddenBytes
    \/ FlushPiece
    \/ ReportPieceFlushed
    \/ NamePiece
    \/ ReportPieceNamed
    \/ ReportWriteFailure
    \/ DiscardOpenRemnant
    \/ ReportRemnantDiscarded
    \/ CaptureEnded
    \/ CarryOn
    \/ FinishCapture
    \/ FinalizeCapture
    \/ RequestJoin
    \/ BeginJoinOutput
    \/ \E missing \in SUBSET Pieces : ReportJoinPrepared(missing)
    \/ NameJoinedFile
    \/ ReportJoinNamed
    \/ \E reason \in {"join failed", "none readable"} : ReportJoinFailure(reason)
    \/ AskRetry
    \/ ScheduleRetry
    \/ DiscardPartialJoin
    \/ ReportPartialJoinDiscarded
    \/ RequestHandover
    \/ DeliverJoinedFile
    \/ ReportHandoverSuccess
    \/ ReportHandoverFailure
    \/ ReportThreeDaysElapsed
    \/ RequestRemoval
    \/ \E p \in Pieces : RemovePiece(p)
    \/ ReportRemovalComplete
    \/ Crash
    \/ Launch
    \/ Finished

FairRetryScheduling == WF_vars(ScheduleRetry)
FairJoinRequest == WF_vars(RequestJoin)
FairHandoverRequest == WF_vars(RequestHandover)
FairFinalization == WF_vars(FinalizeCapture)
FairRemovalRequest == WF_vars(RequestRemoval)

\* Hypothetical premature cleanup after a successful join, before hand-over.
RemoveBeforeHandoverStep(p) ==
    /\ online /\ phase = "HandoverReady" /\ ~handedOver /\ p \in Named
    /\ disk' = [disk EXCEPT ![p] = "absent"]
    /\ durable' = durable \ {p}
    /\ contents' = [contents EXCEPT ![p] = 0]
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, retryPending, recoveryKind, recoveryReply, online, phase, capture, open, durations, known, sealed,
                   captureFault, joinInput, partial, prepared, omitted, gaps,
                   joined, fileOmitted, interruptions, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt>>

RemoveBeforeHandover == \E p \in Pieces : RemoveBeforeHandoverStep(p)

\* Hypothetical naming before the caller has reported a successful flush.
NameBeforeFlush ==
    /\ online /\ phase = "Write" /\ disk[open] = "hidden" /\ open \notin durable
    /\ phase' = "Name"
    /\ disk' = [disk EXCEPT ![open] = "named"]
    /\ sealed' = sealed \cup {open}
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, online, capture, open, durable, contents, durations, known,
                   captureFault, joinInput, partial, prepared, omitted, gaps, joined,
                   fileOmitted, interruptions, interruptionEvidence, handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt, retryPending, recoveryKind, recoveryReply>>

\* Hypothetical loss of already saved gap metadata, even though the file exists.
ForgetGapDurations ==
    /\ online /\ phase = "PublishJoin" /\ joined /\ omitted # {}
    /\ gaps' = ZeroGaps
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, online, phase, capture, open, disk, durable, contents,
                   durations, known, sealed, captureFault, joinInput, partial, prepared,
                   omitted, joined, fileOmitted, interruptions, interruptionEvidence, handoverKey, handoverTarget,
                   delivered, handedOver, retentionElapsed, error, errorAt, retryPending, recoveryKind,
                   recoveryReply>>

\* Hypothetical reset contrary to F4, on a person-requested retry.
ResetInterruptionCount ==
    /\ online /\ retryPending /\ phase = "JoinFailed" /\ interruptions > 0
    /\ interruptions' = 0
    /\ retryPending' = FALSE
    /\ phase' = "DiscardJoinRetry"
    /\ UNCHANGED <<nextNumber, flushReported, recoveredAtLimit, online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, prepared, omitted,
                   gaps, joined, fileOmitted, interruptionEvidence, handoverKey, handoverTarget, delivered,
                   handedOver, retentionElapsed, error, errorAt, recoveryKind, recoveryReply>>

\* Hypothetical launch after naming the joined file but before saving success.
RejoinNamedOutput ==
    /\ ~online /\ phase = "PublishJoin" /\ joined
    /\ LET lostJoin == phase \in {"Join", "PublishJoin"} /\ ~joined
           count == IF lostJoin /\ interruptions < InterruptionLimit
                    THEN interruptions + 1 ELSE interruptions
       IN /\ online' = TRUE
          /\ retryPending' = FALSE
          /\ recoveryKind' =
              CASE phase \in {"Write", "Name", "DiscardPiece"} ->
                       IF open \in Named THEN "named piece" ELSE "none"
                [] phase \in {"Join", "PublishJoin"} ->
                       IF joined THEN "named join"
                       ELSE IF count >= InterruptionLimit
                            THEN "interruption limit" ELSE "interrupted join"
                [] phase \in {"JoinFailed", "DiscardJoinFailed"} -> "failed join"
                [] phase = "Handover" -> "handover"
                [] phase = "HandoverFailed" -> "failed handover"
                [] OTHER -> "none"
          /\ nextNumber' = Cardinality(Named) + 1
          /\ recoveredAtLimit' = (lostJoin /\ count >= InterruptionLimit)
          /\ known' = Named
          /\ capture' = IF capture = "active" THEN "paused" ELSE capture
          /\ open' = IF open \in Named THEN 0 ELSE open
          /\ interruptions' = count
          /\ phase' =
              CASE phase \in {"Write", "Name", "DiscardPiece"} ->
                       IF open \in Named THEN "Capture" ELSE "DiscardPiece"
                [] phase \in {"Join", "PublishJoin"} ->
                       IF joined THEN "Join"
                       ELSE IF count >= InterruptionLimit
                            THEN "DiscardJoinFailed" ELSE "DiscardJoinRetry"
                [] phase \in {"JoinFailed", "DiscardJoinFailed"} -> "DiscardJoinRetry"
                [] phase \in {"Handover", "HandoverFailed"} -> "HandoverReady"
                [] OTHER -> phase
          /\ recoveryReply' = IF recoveryKind' = "none" THEN "none" ELSE phase'
          /\ prepared' = IF lostJoin THEN FALSE ELSE prepared
          /\ omitted' = IF lostJoin THEN {} ELSE omitted
          /\ gaps' = IF lostJoin THEN ZeroGaps ELSE gaps
          /\ error' = IF lostJoin /\ count >= InterruptionLimit
                       THEN "interruption limit" ELSE error
          /\ errorAt' = IF lostJoin /\ count >= InterruptionLimit
                         THEN SpoolPath ELSE errorAt
    /\ UNCHANGED <<flushReported, disk, durable, contents, durations, sealed, captureFault,
                   joinInput, partial, joined, fileOmitted, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver, retentionElapsed>>

\* Hypothetical launch keeps the stale recorded count for the next request.
RecoverNextNumberFromSavedCount ==
    /\ ~online /\ phase = "Name" /\ open \in Named /\ Named # known
    /\ LET lostJoin == phase \in {"Join", "PublishJoin"} /\ ~joined
           count == IF lostJoin /\ interruptions < InterruptionLimit
                    THEN interruptions + 1 ELSE interruptions
       IN /\ online' = TRUE
          /\ retryPending' = FALSE
          /\ recoveryKind' =
              CASE phase \in {"Write", "Name", "DiscardPiece"} ->
                       IF open \in Named THEN "named piece" ELSE "none"
                [] phase \in {"Join", "PublishJoin"} ->
                       IF joined THEN "named join"
                       ELSE IF count >= InterruptionLimit
                            THEN "interruption limit" ELSE "interrupted join"
                [] phase \in {"JoinFailed", "DiscardJoinFailed"} -> "failed join"
                [] phase = "Handover" -> "handover"
                [] phase = "HandoverFailed" -> "failed handover"
                [] OTHER -> "none"
          /\ nextNumber' = Cardinality(known) + 1
          /\ recoveredAtLimit' = (lostJoin /\ count >= InterruptionLimit)
          /\ known' = Named
          /\ capture' = IF capture = "active" THEN "paused" ELSE capture
          /\ open' = IF open \in Named THEN 0 ELSE open
          /\ interruptions' = count
          /\ phase' =
              CASE phase \in {"Write", "Name", "DiscardPiece"} ->
                       IF open \in Named THEN "Capture" ELSE "DiscardPiece"
                [] phase \in {"Join", "PublishJoin"} ->
                       IF joined THEN "HandoverReady"
                       ELSE IF count >= InterruptionLimit
                            THEN "DiscardJoinFailed" ELSE "DiscardJoinRetry"
                [] phase \in {"JoinFailed", "DiscardJoinFailed"} -> "DiscardJoinRetry"
                [] phase \in {"Handover", "HandoverFailed"} -> "HandoverReady"
                [] OTHER -> phase
          /\ recoveryReply' = IF recoveryKind' = "none" THEN "none" ELSE phase'
          /\ prepared' = IF lostJoin THEN FALSE ELSE prepared
          /\ omitted' = IF lostJoin THEN {} ELSE omitted
          /\ gaps' = IF lostJoin THEN ZeroGaps ELSE gaps
          /\ error' = IF lostJoin /\ count >= InterruptionLimit
                       THEN "interruption limit" ELSE error
          /\ errorAt' = IF lostJoin /\ count >= InterruptionLimit
                         THEN SpoolPath ELSE errorAt
    /\ UNCHANGED <<flushReported, disk, durable, contents, durations, sealed, captureFault,
                   joinInput, partial, joined, fileOmitted, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver, retentionElapsed>>

\* Hypothetical failure to report an interrupted attempt at the limit.
RetryInterruptedJoinAtLimit ==
    /\ ~online /\ phase \in {"Join", "PublishJoin"} /\ ~joined
    /\ interruptionEvidence = InterruptionLimit
    /\ LET lostJoin == phase \in {"Join", "PublishJoin"} /\ ~joined
           count == IF lostJoin /\ interruptions < InterruptionLimit
                    THEN interruptions + 1 ELSE interruptions
       IN /\ online' = TRUE
          /\ retryPending' = FALSE
          /\ recoveryKind' =
              CASE phase \in {"Write", "Name", "DiscardPiece"} ->
                       IF open \in Named THEN "named piece" ELSE "none"
                [] phase \in {"Join", "PublishJoin"} ->
                       IF joined THEN "named join"
                       ELSE IF count >= InterruptionLimit
                            THEN "interrupted join" ELSE "interrupted join"
                [] phase \in {"JoinFailed", "DiscardJoinFailed"} -> "failed join"
                [] phase = "Handover" -> "handover"
                [] phase = "HandoverFailed" -> "failed handover"
                [] OTHER -> "none"
          /\ nextNumber' = Cardinality(Named) + 1
          /\ recoveredAtLimit' = (lostJoin /\ count >= InterruptionLimit)
          /\ known' = Named
          /\ capture' = IF capture = "active" THEN "paused" ELSE capture
          /\ open' = IF open \in Named THEN 0 ELSE open
          /\ interruptions' = count
          /\ phase' =
              CASE phase \in {"Write", "Name", "DiscardPiece"} ->
                       IF open \in Named THEN "Capture" ELSE "DiscardPiece"
                [] phase \in {"Join", "PublishJoin"} ->
                       IF joined THEN "HandoverReady"
                       ELSE IF count >= InterruptionLimit
                            THEN "DiscardJoinRetry" ELSE "DiscardJoinRetry"
                [] phase \in {"JoinFailed", "DiscardJoinFailed"} -> "DiscardJoinRetry"
                [] phase \in {"Handover", "HandoverFailed"} -> "HandoverReady"
                [] OTHER -> phase
          /\ recoveryReply' = IF recoveryKind' = "none" THEN "none" ELSE phase'
          /\ prepared' = IF lostJoin THEN FALSE ELSE prepared
          /\ omitted' = IF lostJoin THEN {} ELSE omitted
          /\ gaps' = IF lostJoin THEN ZeroGaps ELSE gaps
          /\ error' = IF lostJoin /\ count >= InterruptionLimit
                       THEN error ELSE error
          /\ errorAt' = IF lostJoin /\ count >= InterruptionLimit
                         THEN errorAt ELSE errorAt
    /\ UNCHANGED <<flushReported, disk, durable, contents, durations, sealed, captureFault,
                   joinInput, partial, joined, fileOmitted, interruptionEvidence,
                   handoverKey, handoverTarget, delivered, handedOver, retentionElapsed>>

\* Hypothetical naming request before the flush report, even if bytes happen
\* already to be durable; the bookkeeping must wait for the report itself.
RequestNameWithoutFlushReport ==
    /\ online /\ phase = "Write" /\ open \in durable /\ ~flushReported
    /\ phase' = "Name"
    /\ UNCHANGED <<online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, joinInput, partial, prepared, omitted,
                   gaps, joined, fileOmitted, interruptions, interruptionEvidence, handoverKey, handoverTarget,
                   delivered, handedOver, retentionElapsed, error, errorAt, retryPending, recoveryKind,
                   recoveryReply, nextNumber, flushReported, recoveredAtLimit>>

\* Hypothetical join while the final capture still has an open piece.
JoinWithOpenPiece ==
    /\ online /\ phase = "Write" /\ capture = "final" /\ known # {} /\ open # 0
    /\ phase' = "Join"
    /\ joinInput' = [i \in 1..Cardinality(known) |-> i]
    /\ UNCHANGED <<online, capture, open, disk, durable, contents, durations,
                   known, sealed, captureFault, partial, prepared, omitted, gaps,
                   joined, fileOmitted, interruptions, interruptionEvidence, handoverKey, handoverTarget, delivered,
                   handedOver, retentionElapsed, error, errorAt, retryPending, recoveryKind, recoveryReply,
                   nextNumber, flushReported, recoveredAtLimit>>

\* Hypothetical hand-over of a piece after the join fails.
HandoverPieceAfterJoinFailureStep(p) ==
    /\ online /\ phase = "JoinFailed" /\ p \in known
    /\ handoverTarget' = p
    /\ delivered' = delivered \cup {p}
    /\ UNCHANGED <<online, phase, capture, open, disk, durable, contents,
                   durations, known, sealed, captureFault, joinInput, partial, prepared,
                   omitted, gaps, joined, fileOmitted, interruptions, interruptionEvidence, handoverKey,
                   handedOver, retentionElapsed, error, errorAt, retryPending, recoveryKind, recoveryReply,
                   nextNumber, flushReported, recoveredAtLimit>>

HandoverPieceAfterJoinFailure == \E p \in Pieces : HandoverPieceAfterJoinFailureStep(p)

\* Hypothetical cleanup that destroys an unjoined recording after failure.
RemoveAfterJoinFailureStep(p) ==
    /\ online /\ phase = "JoinFailed" /\ p \in known
    /\ disk' = [disk EXCEPT ![p] = "absent"]
    /\ durable' = durable \ {p}
    /\ contents' = [contents EXCEPT ![p] = 0]
    /\ UNCHANGED <<online, phase, capture, open, durations, known, sealed,
                   captureFault, joinInput, partial, prepared, omitted, gaps, joined,
                   fileOmitted, interruptions, interruptionEvidence, handoverKey, handoverTarget, delivered, handedOver,
                   retentionElapsed, error, errorAt, retryPending, recoveryKind, recoveryReply, nextNumber,
                   flushReported, recoveredAtLimit>>

RemoveAfterJoinFailure == \E p \in Pieces : RemoveAfterJoinFailureStep(p)

\* Today's spool retries only at launch, so a person's request can go unserved.
\* Dropping the pending request disables scheduling fairness, allowing an
\* online failed recording to wait forever and violating PersonRetryProgress.
IgnorePersonJoinRetry ==
    /\ online /\ phase = "JoinFailed" /\ retryPending
    /\ retryPending' = FALSE
    /\ UNCHANGED <<online, phase, capture, open, disk, durable, contents,
                   durations, known, sealed, captureFault, joinInput, partial, prepared,
                   omitted, gaps, joined, fileOmitted, interruptions, interruptionEvidence, handoverKey,
                   handoverTarget, delivered, handedOver, retentionElapsed, error, errorAt, recoveryKind,
                   recoveryReply, nextNumber, flushReported, recoveredAtLimit>>

Spec == Init /\ [][Next]_vars
====
