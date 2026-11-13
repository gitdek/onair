---- MODULE RecordingHandover ----
EXTENDS Naturals, TLC

CONSTANTS Recording, OtherRecording, MaxAttempts, MaxDifferences, MaxCleanupAttempts

VARIABLES outcome, phase, reason, attempt, round, differences,
          cleanup, cleanupReason, cleanupTry, intent, wire, caller, reply, execution,
          hidden, named, foreignContent, initialNamed, hiddenVerified,
          namedVerified, publicationVerified, publications, copies, activeCopies,
          spool, keepElapsed, running, replayed

vars == <<outcome, phase, reason, attempt, round, differences,
          cleanup, cleanupReason, cleanupTry, intent, wire, caller, reply, execution,
          hidden, named, foreignContent, initialNamed, hiddenVerified,
          namedVerified, publicationVerified, publications, copies, activeCopies,
          spool, keepElapsed, running, replayed>>

\* Names are abstract path components; prefix is prepended to name by the caller.
HiddenName == [directory |-> "folder", prefix |-> ".", name |-> Recording]
FinalName == [directory |-> "folder", prefix |-> "", name |-> Recording]
SpoolName == [directory |-> "spool", prefix |-> "", name |-> Recording]
OtherName == [directory |-> "folder", prefix |-> "", name |-> OtherRecording]
Names == {HiddenName, FinalName, SpoolName, OtherName}
Kinds == {"none", "compareNamed", "copy", "compareHidden", "publish", "removeHidden", "removeSpool"}
Phases == {"idle", "inspect", "inspectTaken", "preclean", "copy", "verify", "discard", "publish", "retention", "terminal"}
Reasons == {"none", "folderGone", "folderFull", "nameConflict", "differentCopies"}
Replies == {"none", "ok", "absent", "same", "different", "taken", "folderGone", "folderFull", "removeFailed"}
NoRequest == [kind |-> "none", target |-> HiddenName, token |-> <<0, 0, "idle", 0>>]
Request(kind, target, stage) ==
    [kind |-> kind, target |-> target, token |-> <<attempt, round, stage, cleanupTry>>]
Requests ==
    [kind : Kinds, target : Names,
     token : (0..MaxAttempts) \X (0..MaxDifferences) \X Phases \X (0..MaxCleanupAttempts)]
Terminal == outcome \in {"handedOver", "failed"}
FolderMutation(r) == r.kind \in {"copy", "publish", "removeHidden"}

\* The ratified contract is about requests and durable evidence, not disk code.
PublicationIsVerified ==
    /\ (intent.kind = "publish" \/ wire.kind = "publish") => hiddenVerified
    /\ publications > 0 => publicationVerified /\ named = "same"

SpoolRemovalIsSafe ==
    (intent.kind = "removeSpool" \/ wire.kind = "removeSpool" \/ ~spool)
        => outcome = "handedOver" /\ namedVerified /\ keepElapsed

NoCopyAfterPublication ==
    publications > 0 => intent.kind # "copy" /\ wire.kind # "copy"

OneCopyAtATime == activeCopies <= 1

ReplayKeepsIdentity ==
    /\ wire = NoRequest \/ wire = intent
    /\ caller # "idle" => execution = intent

OnlyOwnedNames ==
    \A r \in {intent, wire} :
        /\ FolderMutation(r) => r.target \in {HiddenName, FinalName}
        /\ r.kind \in {"copy", "removeHidden"} => r.target = HiddenName
        /\ r.kind = "publish" => r.target = FinalName
        /\ r.kind = "removeSpool" => r.target = SpoolName

CopiesAreHidden ==
    \A r \in {intent, wire} : r.kind = "copy" => r.target.prefix = "."

NewCopiesStartCleanly ==
    (intent.kind = "copy" /\ caller = "idle") => hidden = "absent"

DifferenceLimitStopsCopying ==
    differences = MaxDifferences /\ outcome = "active" => phase = "discard"

AtMostOnePublication == publications <= 1

ForeignFileIsPreserved == foreignContent # "absent" => named = foreignContent /\ publications = 0

SuccessHasEvidence == outcome = "handedOver" => named = "same" /\ namedVerified

FailureKeepsSpool == outcome = "failed" => spool /\ reason \in (Reasons \ {"none"})

DifferentCopyIsNotPublished == hidden = "different" => ~hiddenVerified

DifferenceLimitCleansUp == reason = "differentCopies" => differences = MaxDifferences /\ hidden = "absent"

ExistingMatchAvoidsCopy == initialNamed = "same" => copies = 0

CleanupCannotUndoDelivery == cleanup # "dormant" => outcome = "handedOver"

TypeOK ==
    /\ Recording # OtherRecording
    /\ MaxAttempts \in Nat \ {0} /\ MaxDifferences \in Nat \ {0} /\ MaxCleanupAttempts \in Nat \ {0}
    /\ outcome \in {"notStarted", "active", "handedOver", "failed"}
    /\ phase \in Phases /\ reason \in Reasons
    /\ attempt \in 0..MaxAttempts /\ round \in 0..MaxDifferences /\ differences \in 0..MaxDifferences
    /\ cleanup \in {"dormant", "waiting", "active", "failed", "done"}
    /\ cleanupReason \in {"none", "removeFailed"} /\ cleanupTry \in 0..MaxCleanupAttempts
    /\ intent \in Requests /\ wire \in Requests /\ execution \in Requests
    /\ caller \in {"idle", "busy", "done"} /\ reply \in Replies
    /\ hidden \in {"absent", "partial", "same", "different"}
    /\ named \in {"absent", "same", "different"}
    /\ foreignContent \in {"absent", "same", "different"} /\ initialNamed \in {"absent", "same", "different"}
    /\ hiddenVerified \in BOOLEAN /\ namedVerified \in BOOLEAN /\ publicationVerified \in BOOLEAN
    /\ publications \in 0..2 /\ copies \in 0..(MaxAttempts * MaxDifferences)
    /\ activeCopies \in 0..2 /\ spool \in BOOLEAN /\ keepElapsed \in BOOLEAN
    /\ running \in BOOLEAN /\ replayed \in BOOLEAN

DeliveredByCopy == outcome = "handedOver" /\ publications = 1 /\ spool
AcceptedExisting == outcome = "handedOver" /\ initialNamed = "same" /\ copies = 0
ConflictingName == outcome = "failed" /\ reason = "nameConflict" /\ named = "different"
FolderGone == outcome = "failed" /\ reason = "folderGone"
FolderFull == outcome = "failed" /\ reason = "folderFull"
DifferencesExhausted == outcome = "failed" /\ reason = "differentCopies" /\ hidden = "absent"
ReplacedDifferentCopy == outcome = "handedOver" /\ round > 1 /\ publications = 1
RetriedDelivery == outcome = "handedOver" /\ attempt > 1
RecoveredPublication == outcome = "handedOver" /\ publications = 1 /\ replayed
RetainingOriginal == outcome = "handedOver" /\ spool /\ ~keepElapsed
OriginalRemoved == outcome = "handedOver" /\ cleanup = "done" /\ ~spool
CleanupFailed == outcome = "handedOver" /\ cleanup = "failed" /\ spool
CleanupRetried == outcome = "handedOver" /\ cleanup = "done" /\ cleanupTry > 1

\* No fairness is imposed on the caller or the passage of retention time.
\* A caller that never resumes or never supplies an outstanding result has
\* interrupted the hand-over for good, and is outside this progress promise.
CallerContinues == ([]<>running) /\ ((intent # NoRequest) ~> (caller = "done"))
StartedAttemptFinishes == CallerContinues => ((outcome = "active") ~> Terminal)

Init ==
    /\ outcome = "notStarted" /\ phase = "idle" /\ reason = "none"
    /\ attempt = 0 /\ round = 0 /\ differences = 0
    /\ cleanup = "dormant" /\ cleanupReason = "none" /\ cleanupTry = 0
    /\ intent = NoRequest /\ wire = NoRequest /\ caller = "idle" /\ reply = "none"
    /\ hidden = "absent" /\ named = "absent"
    /\ foreignContent = named /\ initialNamed = named
    /\ hiddenVerified = FALSE /\ namedVerified = FALSE /\ publicationVerified = FALSE
    /\ publications = 0 /\ copies = 0 /\ activeCopies = 0
    /\ spool = TRUE /\ keepElapsed = FALSE /\ running = TRUE /\ replayed = FALSE
    /\ execution = NoRequest

\* Start and retry are caller choices; attempts are bounded only for exploration.
Start ==
    /\ running /\ outcome = "notStarted"
    /\ outcome' = "active" /\ phase' = "inspect" /\ attempt' = 1 /\ round' = 1
    /\ UNCHANGED <<reason, differences, cleanup, cleanupReason, cleanupTry, intent, wire, caller, reply,
                   hidden, named, foreignContent, initialNamed, hiddenVerified, namedVerified,
                   publicationVerified, publications, copies, activeCopies, spool, keepElapsed, running, replayed>>
    /\ UNCHANGED execution

RetryDelivery ==
    /\ running /\ outcome = "failed" /\ attempt < MaxAttempts
    /\ outcome' = "active" /\ reason' = "none" /\ attempt' = attempt + 1
    /\ round' = 1 /\ differences' = 0 /\ hiddenVerified' = FALSE
    /\ phase' = IF hidden = "absent" THEN "inspect" ELSE "preclean"
    /\ UNCHANGED <<cleanup, cleanupReason, cleanupTry, intent, wire, caller, reply, hidden, named,
                   foreignContent, initialNamed, namedVerified, publicationVerified, publications,
                   copies, activeCopies, spool, keepElapsed, running, replayed>>
    /\ UNCHANGED execution

Issue ==
    /\ running /\ intent = NoRequest /\ (outcome = "active" \/ cleanup = "active")
    /\ LET r == CASE phase \in {"inspect", "inspectTaken"} -> Request("compareNamed", FinalName, phase)
                     [] phase = "preclean" -> Request("removeHidden", HiddenName, phase)
                     [] phase = "copy" -> Request("copy", HiddenName, phase)
                     [] phase = "verify" -> Request("compareHidden", HiddenName, phase)
                     [] phase = "discard" -> Request("removeHidden", HiddenName, phase)
                     [] phase = "publish" -> Request("publish", FinalName, phase)
                     [] phase = "retention" -> Request("removeSpool", SpoolName, phase)
       IN intent' = r /\ wire' = r
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences, cleanup, cleanupReason,
                   cleanupTry, caller, reply, hidden, named, foreignContent, initialNamed,
                   hiddenVerified, namedVerified, publicationVerified, publications, copies,
                   activeCopies, spool, keepElapsed, running, replayed>>
    /\ UNCHANGED execution

Resend ==
    /\ running /\ intent # NoRequest /\ wire = NoRequest
    /\ wire' = intent
    /\ replayed' = (replayed \/ (intent.kind = "publish" /\ reply = "ok"))
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences, cleanup, cleanupReason,
                   cleanupTry, intent, caller, reply, hidden, named, foreignContent, initialNamed,
                   hiddenVerified, namedVerified, publicationVerified, publications, copies,
                   activeCopies, spool, keepElapsed, running>>
    /\ UNCHANGED execution

\* The caller adapter executes an identity once, caches its result, and shares
\* an in-flight copy with all repetitions. These are the settled caller rules.
BeginCopy ==
    /\ wire.kind = "copy" /\ caller = "idle"
    /\ caller' = "busy" /\ hidden' = "partial" /\ activeCopies' = 1 /\ copies' = copies + 1
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences, cleanup, cleanupReason,
                   cleanupTry, intent, wire, reply, named, foreignContent, initialNamed,
                   hiddenVerified, namedVerified, publicationVerified, publications,
                   spool, keepElapsed, running, replayed>>
    /\ execution' = wire

FinishCopy(result, content) ==
    /\ caller = "busy"
    /\ result \in {"ok", "folderGone", "folderFull"}
    /\ content \in IF result = "ok" THEN {"same", "different"} ELSE {"partial"}
    /\ reply' = result /\ hidden' = content
    /\ caller' = "done" /\ activeCopies' = 0
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences, cleanup, cleanupReason,
                   cleanupTry, intent, wire, named, foreignContent, initialNamed,
                   hiddenVerified, namedVerified, publicationVerified, publications, copies,
                   spool, keepElapsed, running, replayed>>
    /\ UNCHANGED execution

ExecuteOther(r) ==
    /\ wire # NoRequest /\ wire.kind # "copy" /\ caller = "idle"
    /\ caller' = "done"
    /\ r \in CASE wire.kind = "compareNamed" -> {named, "folderGone", "folderFull"}
                     [] wire.kind = "compareHidden" -> {hidden, "folderGone", "folderFull"}
                     [] wire.kind = "publish" -> {IF named = "absent" THEN "ok" ELSE "taken", "folderGone", "folderFull"}
                     [] wire.kind = "removeHidden" -> {"ok", "folderGone", "folderFull"}
                     [] wire.kind = "removeSpool" -> {"ok", "removeFailed"}
    /\ reply' = r
    /\ hidden' = IF r = "ok" /\ wire.kind \in {"publish", "removeHidden"} THEN "absent" ELSE hidden
    /\ named' = IF r = "ok" /\ wire.kind = "publish" THEN hidden ELSE named
    /\ publications' = IF r = "ok" /\ wire.kind = "publish" THEN publications + 1 ELSE publications
    /\ publicationVerified' = IF r = "ok" /\ wire.kind = "publish" THEN hiddenVerified ELSE publicationVerified
    /\ spool' = IF r = "ok" /\ wire.kind = "removeSpool" THEN FALSE ELSE spool
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences, cleanup, cleanupReason,
                   cleanupTry, intent, wire, foreignContent, initialNamed, hiddenVerified,
                   namedVerified, copies, activeCopies, keepElapsed, running, replayed>>
    /\ execution' = wire

Consume ==
    /\ running /\ intent # NoRequest /\ wire = intent /\ caller = "done"
    /\ intent' = NoRequest /\ wire' = NoRequest /\ caller' = "idle" /\ reply' = "none"
    /\ LET failed == reply \in {"folderGone", "folderFull"}
           success == (phase \in {"inspect", "inspectTaken"} /\ reply = "same") \/ (phase = "publish" /\ reply = "ok")
           conflict == phase \in {"inspect", "inspectTaken"} /\ reply = "different"
           exhausted == phase = "discard" /\ reply = "ok" /\ differences = MaxDifferences
       IN /\ outcome' = IF phase = "retention" THEN outcome
                         ELSE IF failed \/ conflict \/ exhausted THEN "failed"
                         ELSE IF success THEN "handedOver" ELSE outcome
          /\ reason' = IF failed THEN reply ELSE IF conflict THEN "nameConflict"
                        ELSE IF exhausted THEN "differentCopies" ELSE reason
          /\ phase' = IF failed \/ conflict \/ exhausted \/ success \/ phase = "retention" THEN "terminal"
                       ELSE CASE phase = "inspect" -> "copy"
                                 [] phase = "preclean" -> "inspect"
                                 [] phase = "copy" -> "verify"
                                 [] phase = "verify" -> IF reply = "same" THEN "publish" ELSE "discard"
                                 [] phase = "discard" -> "copy"
                                 [] phase = "publish" -> "inspectTaken"
          /\ differences' = IF phase = "verify" /\ reply = "different" THEN differences + 1 ELSE differences
          /\ round' = IF phase = "discard" /\ reply = "ok" /\ ~exhausted THEN round + 1 ELSE round
          /\ hiddenVerified' = IF phase = "verify" /\ reply = "same" THEN TRUE
                                ELSE IF phase \in {"preclean", "discard"} /\ reply = "ok" THEN FALSE
                                ELSE hiddenVerified
          /\ namedVerified' = (namedVerified \/ success)
          /\ cleanup' = IF success THEN "waiting"
                         ELSE IF phase = "retention" THEN IF reply = "ok" THEN "done" ELSE "failed"
                         ELSE cleanup
          /\ cleanupReason' = IF phase = "retention" /\ reply = "removeFailed" THEN "removeFailed" ELSE cleanupReason
    /\ UNCHANGED <<attempt, cleanupTry, hidden, named, foreignContent, initialNamed,
                   publicationVerified, publications, copies, activeCopies, spool, keepElapsed, running, replayed>>
    /\ execution' = NoRequest

BeginRetention ==
    /\ running /\ outcome = "handedOver" /\ cleanup = "waiting" /\ keepElapsed
    /\ cleanup' = "active" /\ cleanupTry' = 1 /\ phase' = "retention"
    /\ UNCHANGED <<outcome, reason, attempt, round, differences, cleanupReason, intent, wire,
                   caller, reply, hidden, named, foreignContent, initialNamed, hiddenVerified,
                   namedVerified, publicationVerified, publications, copies, activeCopies,
                   spool, keepElapsed, running, replayed>>
    /\ UNCHANGED execution

RetryCleanup ==
    /\ running /\ cleanup = "failed" /\ cleanupTry < MaxCleanupAttempts
    /\ cleanup' = "active" /\ cleanupReason' = "none" /\ cleanupTry' = cleanupTry + 1 /\ phase' = "retention"
    /\ UNCHANGED <<outcome, reason, attempt, round, differences, intent, wire, caller, reply,
                   hidden, named, foreignContent, initialNamed, hiddenVerified, namedVerified,
                   publicationVerified, publications, copies, activeCopies, spool, keepElapsed, running, replayed>>
    /\ UNCHANGED execution

\* The environment may take the final name even between its comparison and
\* publication. Publication is non-replacing and reports that collision.
NameAppears(content) ==
    /\ outcome = "active" /\ named = "absent"
    /\ content \in {"same", "different"}
    /\ named' = content /\ foreignContent' = content
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences, cleanup, cleanupReason,
                   cleanupTry, intent, wire, caller, reply, hidden, initialNamed, hiddenVerified,
                   namedVerified, publicationVerified, publications, copies, activeCopies,
                   spool, keepElapsed, running, replayed>>
    /\ UNCHANGED execution

RetentionElapsed ==
    /\ ~keepElapsed /\ keepElapsed' = TRUE
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences, cleanup, cleanupReason,
                   cleanupTry, intent, wire, caller, reply, hidden, named, foreignContent,
                   initialNamed, hiddenVerified, namedVerified, publicationVerified, publications,
                   copies, activeCopies, spool, running, replayed>>
    /\ UNCHANGED execution

Crash ==
    /\ running /\ running' = FALSE /\ wire' = NoRequest
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences, cleanup, cleanupReason,
                   cleanupTry, intent, caller, reply, hidden, named, foreignContent, initialNamed,
                   hiddenVerified, namedVerified, publicationVerified, publications, copies,
                   activeCopies, spool, keepElapsed, replayed>>
    /\ UNCHANGED execution

Resume ==
    /\ ~running /\ running' = TRUE
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences, cleanup, cleanupReason,
                   cleanupTry, intent, wire, caller, reply, hidden, named, foreignContent, initialNamed,
                   hiddenVerified, namedVerified, publicationVerified, publications, copies,
                   activeCopies, spool, keepElapsed, replayed>>
    /\ UNCHANGED execution

\* Stopping or waiting for the caller is legitimate and never a deadlock.
Wait == UNCHANGED vars

Drive == Issue \/ Resend \/ Consume \/ BeginRetention
IssueProgress == SF_vars(Issue)
ResendProgress == SF_vars(Resend)
ConsumeProgress == SF_vars(Consume)
RetentionProgress == SF_vars(BeginRetention)

\* Single-root exploration: choose a pre-existing file before starting.
InitialFile(content) ==
    /\ outcome = "notStarted" /\ named = "absent"
    /\ content \in {"same", "different"}
    /\ named' = content /\ foreignContent' = content /\ initialNamed' = content
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences,
                   cleanup, cleanupReason, cleanupTry, intent, wire, caller, reply,
                   execution, hidden, hiddenVerified, namedVerified, publicationVerified,
                   publications, copies, activeCopies, spool, keepElapsed, running, replayed>>

Next == Start \/ RetryDelivery \/ Issue \/ Resend \/ Consume \/ BeginRetention \/ BeginCopy
        \/ (\E result \in {"ok", "folderGone", "folderFull"}, content \in {"same", "different", "partial"} : FinishCopy(result, content))
        \/ (\E r \in Replies : ExecuteOther(r))
        \/ RetryCleanup
        \/ (\E content \in {"same", "different"} : NameAppears(content))
        \/ (\E content \in {"same", "different"} : InitialFile(content))
        \/ RetentionElapsed \/ Crash \/ Resume \/ Wait

\* Mutation tests emit the unsafe request, before any file effect is needed.
PublishUnchecked ==
    /\ running /\ phase = "verify" /\ intent = NoRequest
    /\ intent' = Request("publish", FinalName, "publish") /\ wire' = intent'
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences, cleanup, cleanupReason,
                   cleanupTry, caller, reply, hidden, named, foreignContent, initialNamed,
                   hiddenVerified, namedVerified, publicationVerified, publications, copies,
                   activeCopies, spool, keepElapsed, running, replayed>>
    /\ UNCHANGED execution

RemoveSpoolEarly ==
    /\ running /\ outcome = "active" /\ intent = NoRequest
    /\ intent' = Request("removeSpool", SpoolName, "retention") /\ wire' = intent'
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences, cleanup, cleanupReason,
                   cleanupTry, caller, reply, hidden, named, foreignContent, initialNamed,
                   hiddenVerified, namedVerified, publicationVerified, publications, copies,
                   activeCopies, spool, keepElapsed, running, replayed>>
    /\ UNCHANGED execution

RestartAfterUnrecordedPublication ==
    /\ running /\ intent.kind = "publish" /\ reply = "ok" /\ wire = NoRequest
    /\ wire' = Request("copy", HiddenName, "copy")
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences, cleanup, cleanupReason,
                   cleanupTry, intent, caller, reply, hidden, named, foreignContent, initialNamed,
                   hiddenVerified, namedVerified, publicationVerified, publications, copies,
                   activeCopies, spool, keepElapsed, running, replayed>>
    /\ UNCHANGED execution

CopyWhileBusy ==
    /\ caller = "busy" /\ activeCopies = 1
    /\ activeCopies' = 2
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences, cleanup, cleanupReason,
                   cleanupTry, intent, wire, caller, reply, hidden, named, foreignContent,
                   initialNamed, hiddenVerified, namedVerified, publicationVerified, publications,
                   copies, spool, keepElapsed, running, replayed>>
    /\ UNCHANGED execution

RemoveUnrelatedFile ==
    /\ running /\ phase = "discard" /\ intent = NoRequest
    /\ intent' = Request("removeHidden", OtherName, "discard") /\ wire' = intent'
    /\ UNCHANGED <<outcome, phase, reason, attempt, round, differences, cleanup, cleanupReason,
                   cleanupTry, caller, reply, hidden, named, foreignContent, initialNamed,
                   hiddenVerified, namedVerified, publicationVerified, publications, copies,
                   activeCopies, spool, keepElapsed, running, replayed>>
    /\ UNCHANGED execution

Spec == Init /\ [][Next]_vars
====
