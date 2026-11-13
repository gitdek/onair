# Recording hand-over

Written by Invariant for [#1](https://github.com/gitdek/onair/issues/1): Hand a finished recording over once and whole, and only then let go of it.

@gitdek ratified these statements [on the issue](https://github.com/gitdek/onair/issues/1#issuecomment-6867217748). They're pinned by hash in [`ratified.lock`](.invariant/ratified.lock), and their text is in [`.invariant/specs/RecordingHandover.tla`](.invariant/specs/RecordingHandover.tla).

| Statement | Kind | Says |
| :-- | :-- | :-- |
| `Spec` | spec | The hand-over starts in its initial state and proceeds through the modeled core and caller steps. |
| `PublicationIsVerified` | invariant | The core requests publication only after the hidden copy was found identical to the spool file, and every publication carries that evidence. |
| `SpoolRemovalIsSafe` | invariant | The original is requested for removal or removed only after verified delivery and expiry of its retention time. |
| `NoCopyAfterPublication` | invariant | Once the recording has been published, the core never requests another copy, including after an interruption before recording success. |
| `OneCopyAtATime` | invariant | At most one copy of the recording is being executed at a time. |
| `ReplayKeepsIdentity` | invariant | A repeated request retains the saved operation, names and identity, including while its execution or result is outstanding. |
| `OnlyOwnedNames` | invariant | Every request to change or remove a file targets only the appropriate hidden name, recording name or original spool file belonging to this hand-over. |
| `CopiesAreHidden` | invariant | Every copy request uses the hand-over’s hidden name with a leading dot. |
| `NewCopiesStartCleanly` | invariant | A new copy is requested only after any previous hidden copy has been removed. |
| `DifferenceLimitStopsCopying` | invariant | Reaching the configured limit of differing copies permits only their cleanup before the attempt finishes. |
| `AtMostOnePublication` | invariant | The recording is published at most once across all interruptions and retries. |
| `ForeignFileIsPreserved` | invariant | A file already occupying the final name is never replaced or removed by this hand-over. |
| `SuccessHasEvidence` | invariant | A successful hand-over has an identical file at its final name and recorded verification evidence. |
| `FailureKeepsSpool` | invariant | A failed hand-over records its reason and retains the original spool file. |
| `DifferentCopyIsNotPublished` | invariant | A differing hidden copy never carries permission to be published. |
| `DifferenceLimitCleansUp` | invariant | An attempt that fails for exhausting its differing-copy limit has removed the last differing copy. |
| `ExistingMatchAvoidsCopy` | invariant | An identical file present at the final name when the hand-over begins is accepted without making a copy. |
| `CleanupCannotUndoDelivery` | invariant | Waiting for retention, removing the original, or failing and retrying that removal leaves the hand-over successful. |
| `TypeOK` | invariant | Every state component stays within its declared type and exploration bounds. |
| `DeliveredByCopy` | witness | A new copy can be verified and published while the original remains in the spool. |
| `AcceptedExisting` | witness | An identical existing file can be accepted without copying. |
| `ConflictingName` | witness | A differing existing file can cause a name-conflict failure while remaining unchanged. |
| `FolderGone` | witness | A missing folder can end the hand-over with that failure reason. |
| `FolderFull` | witness | A full folder can end the hand-over with that failure reason. |
| `DifferencesExhausted` | witness | Repeated differing copies can reach the limit, be cleaned up and produce the specified failure. |
| `ReplacedDifferentCopy` | witness | A differing copy can be removed and replaced by a copy that is successfully handed over. |
| `RetriedDelivery` | witness | A failed hand-over can be retried successfully. |
| `RecoveredPublication` | witness | An interruption after publication but before recording success can be recovered by repeating the original request. |
| `RetainingOriginal` | witness | A successful hand-over can retain its original while the retention time has not elapsed. |
| `OriginalRemoved` | witness | The original can be removed after successful hand-over and expiry of retention. |
| `CleanupFailed` | witness | Removal of the original can fail while the hand-over stays successful and the original remains. |
| `CleanupRetried` | witness | A failed removal of the original can be retried successfully. |
| `IssueProgress` | fairness | If issuing the next saved request is possible repeatedly, the core eventually issues it. |
| `ResendProgress` | fairness | If repeating an interrupted request is possible repeatedly, the core eventually repeats it. |
| `ConsumeProgress` | fairness | If recording a completed caller result is possible repeatedly, the core eventually records it. |
| `RetentionProgress` | fairness | If starting original removal after retention is possible repeatedly, the core eventually starts it. |
| `StartedAttemptFinishes` | property | If execution keeps resuming and the caller eventually answers outstanding requests, each started attempt eventually reports success or failure. |
| `PublishUnchecked` | bug | The core requests publication before comparing the copy. |
| `RemoveSpoolEarly` | bug | The core requests removal of the original before delivery. |
| `RestartAfterUnrecordedPublication` | bug | After an interrupted publication, recovery requests a fresh copy instead of repeating the saved publication request. |
| `CopyWhileBusy` | bug | A second copy is started while the first copy is still in progress. |
| `RemoveUnrelatedFile` | bug | Discarding a differing copy mistakenly requests removal of an unrelated file. |

Checked within `MaxAttempts = 2`, `MaxCleanupAttempts = 2`, `MaxDifferences = 2`, `OtherRecording = "unrelated.wav"`, `Recording = "recording.wav"`. To run the gate yourself:

```bash
go run ./cmd/invariant verify core/handover
```
