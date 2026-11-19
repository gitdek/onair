# Recording pieces

Written by Invariant for [#9](https://github.com/gitdek/onair/issues/9): Keep every closed piece until its recording is handed over, through crashes and failed joins.

@gitdek ratified these statements [on the issue](https://github.com/gitdek/onair/issues/9#issuecomment-6901150746). They're pinned by hash in [`ratified.lock`](.invariant/ratified.lock), and their text is in [`.invariant/specs/RecordingPieces.tla`](.invariant/specs/RecordingPieces.tla).

| Statement | Kind | Says |
| :-- | :-- | :-- |
| `Spec` | spec | The bookkeeping starts in Init and changes only through the modeled requests, caller reports, crashes and launches. |
| `ClosedPiecesPreserved` | invariant | Every piece ever closed keeps its named bytes until the joined recording has been handed over and the caller reports that the three-day retention period has passed. |
| `WholePiecesAreDurable` | invariant | Every piece with its final name has all its bytes flushed to disk. |
| `NamingRequiresFlushReport` | invariant | This part requests a piece’s final name only after the caller has reported that all its bytes are flushed to disk. |
| `TypeOK` | invariant | Every bookkeeping field, caller effect and verification history has its declared type. |
| `OnlyNamedPiecesAreWhole` | invariant | Before removal is allowed, every piece recorded as whole has its final name on disk. |
| `SettledInventoryMatchesDisk` | invariant | Whenever the bookkeeping is running outside an unresolved naming or removal operation, its inventory is exactly the pieces named on disk. |
| `ClosedNumbersNeverReused` | invariant | Closed pieces form the numbered sequence starting at one, and a new write always uses the number after the last closed piece. |
| `JoinWaitsForFinalCapture` | invariant | Finishing the recording begins only after capture is final and no piece remains open. |
| `OnlyOneOpenPiece` | invariant | At most one piece has hidden bytes, and any open piece is the one immediately after the recorded closed pieces. |
| `NoMoreFromFailedCapture` | invariant | A write failure prevents any more pieces from that capture and remains recorded when the capture is finalized. |
| `JoinUsesEveryClosedPieceInOrder` | invariant | Each join request contains every closed piece exactly once in increasing number order. |
| `JoinRequestHasOnlyWholePieces` | invariant | Joining uses a nonempty set of closed pieces still present on disk and never starts another join over an already named joined file. |
| `JoinPublicationNeedsSavedManifest` | invariant | A named joined file always has a saved manifest matching its omitted pieces and their gap durations. |
| `EveryOmissionHasItsDuration` | invariant | A successful join preparation leaves at least one piece readable and records the duration of every omitted piece. |
| `OneJoinedFileOnly` | invariant | Every hand-over uses the recording’s stable identity and its single joined file, and no piece is handed over. |
| `HandoverRequiresSuccess` | invariant | A recording counts as handed over only after the hand-over has succeeded. |
| `RemovalWaitsThreeDaysAfterHandover` | invariant | Piece removal is requested only after successful hand-over and the caller’s report that three days have passed. |
| `FailedJoinKeepsRecording` | invariant | A failed join hands over nothing, preserves every closed piece, and records its failure reason. |
| `FailedHandoverKeepsRecording` | invariant | A reported hand-over failure keeps every closed piece and records the error without counting the recording as handed over. |
| `ErrorsIdentifySpool` | invariant | Every reported error identifies the recording’s spool location for the menu. |
| `InterruptionCountSurvivesRetries` | invariant | While running, the saved interruption count includes every interrupted join up to the limit and is never reset by a retry. |
| `InterruptedJoinStopsAtLimit` | invariant | Every launch that recovers an interrupted join at the cumulative interruption limit reports failure, and every such failure awaiting cleanup has reached the limit. |
| `EmptyMeansNothingRecorded` | invariant | A recording with no closed pieces finishes with nothing recorded and neither joins nor hands over a file. |
| `NamedPieceRecoveryUsesDisk` | invariant | Recovery accepts a piece named before the crash as closed even if its naming result was not saved. |
| `NamedJoinRecoveryUsesExistingFile` | invariant | Recovery of an already named joined file proceeds to hand-over using that file. |
| `InterruptedJoinRecovery` | invariant | Recovery discards an interrupted join’s partial output and schedules a retry below the interruption limit or reports failure at the limit. |
| `FailedJoinRetriesOnLaunch` | invariant | Launching after a failed join schedules its partial output cleanup and another join attempt. |
| `HandoverRetriesOnLaunch` | invariant | Launching after an unresolved or failed hand-over schedules another request for the same joined recording. |
| `RetryRequestRemainsPending` | invariant | A person’s retry request remains pending against the failed join or hand-over until it is scheduled or recovered at launch. |
| `SuccessfulHandover` | witness | A recording can be joined and handed over without omissions. |
| `HandoverWithGap` | witness | A recording can be handed over with an unreadable piece omitted and its duration reported as a gap. |
| `WriteFailureKept` | witness | A failed write can discard the open remnant while preserving earlier closed pieces. |
| `JoinFailureKept` | witness | A join can fail while every closed piece stays in the spool. |
| `NoneReadableKept` | witness | A join that can read no piece can fail while keeping all the pieces. |
| `HandoverFailureKept` | witness | Hand-over can fail after a successful join while all the pieces stay in the spool. |
| `NothingRecorded` | witness | A capture with no closed piece can finish with nothing recorded. |
| `RetentionCompleted` | witness | All pieces can be removed after successful hand-over and the retention period. |
| `InterruptionLimitReached` | witness | Repeated interrupted joins can reach the limit and be reported as a failed join. |
| `SuccessAfterInterruptionLimit` | witness | A later retry can succeed without resetting an interruption count that has already reached the limit. |
| `NamedPieceSurvivesUnrecordedCrash` | witness | A crash can leave a whole named piece whose successful naming has not yet been recorded. |
| `NamedJoinSurvivesUnrecordedCrash` | witness | A crash can leave a named joined file before the join success is recorded. |
| `UnresolvedHandoverSurvivesCrash` | witness | A crash can occur after the archive receives the joined file but before hand-over success is recorded. |
| `CarriedOnWithNextPiece` | witness | After recovering an unrecorded named piece, capture can carry on using the following piece number. |
| `PersonRetryReceived` | witness | A person can request another attempt after a reported failure. |
| `NamedJoinRecovered` | witness | A launch can recover an already named joined file and its saved gap manifest. |
| `PersonRetryProgress` | property | While the app keeps running, a pending request from the person eventually schedules join cleanup or hand-over. |
| `JoinRequestProgress` | property | While the app keeps running, a join ready to start is eventually requested. |
| `HandoverRequestProgress` | property | While the app keeps running, a hand-over ready to start is eventually requested. |
| `FinalCaptureProgress` | property | While the app keeps running, a final capture with no open piece eventually becomes ready to join or reports nothing recorded. |
| `RetentionRequestProgress` | property | While the app keeps running, expiry of the retention period eventually causes a piece removal request. |
| `FairRetryScheduling` | fairness | If scheduling a requested retry remains possible, the bookkeeping eventually schedules it. |
| `FairJoinRequest` | fairness | If issuing a join request remains possible, the bookkeeping eventually issues it. |
| `FairHandoverRequest` | fairness | If issuing a hand-over request remains possible, the bookkeeping eventually issues it. |
| `FairFinalization` | fairness | If finalizing the capture remains possible, the bookkeeping eventually finalizes it. |
| `FairRemovalRequest` | fairness | If issuing a piece removal request remains possible, the bookkeeping eventually issues it. |
| `RemoveBeforeHandover` | bug | An implementation mistakenly removes a closed piece as soon as joining succeeds, before hand-over. |
| `NameBeforeFlush` | bug | An implementation mistakenly names a piece before its bytes have been flushed. |
| `ForgetGapDurations` | bug | An implementation mistakenly loses saved gap durations after naming the joined file. |
| `ResetInterruptionCount` | bug | An implementation mistakenly resets the interruption count when retrying a failed join. |
| `RejoinNamedOutput` | bug | An implementation mistakenly starts joining again at launch after a crash between the joined file receiving its name and the join success being saved. |
| `RecoverNextNumberFromSavedCount` | bug | An implementation mistakenly recovers the next piece number from the stale saved count, so carrying on reuses a piece number already named on disk. |
| `JoinWithOpenPiece` | bug | An implementation mistakenly requests the join while the final capture still has an open piece. |
| `HandoverPieceAfterJoinFailure` | bug | An implementation mistakenly hands over a piece after the join fails. |
| `RemoveAfterJoinFailure` | bug | An implementation mistakenly removes a closed piece after the join fails. |
| `IgnorePersonJoinRetry` | bug | The current behavior leaves a failed join waiting for another launch instead of scheduling the person’s requested retry. |
| `RequestNameWithoutFlushReport` | bug | An implementation mistakenly requests a piece’s name before receiving the flush report, even when its bytes happen already to be on disk. |
| `RetryInterruptedJoinAtLimit` | bug | An implementation mistakenly retries an interrupted join at the cumulative interruption limit without reporting failure on that launch. |

Decided on the issue:

- If On Air dies after the joined file gets its name but before the join result is saved, how should the gap information survive? **B.** The caller reports the omitted pieces and gap durations before naming the joined file, and waits until this part has saved them. (@gitdek)
- If On Air dies during hand-over before its result is saved, how should this part recover the unresolved hand-over? **A.** The part retries the same hand-over request after recovery, and the hand-over part deduplicates it using the recording’s stable identity. (@gitdek)
- After hand-over explicitly reports failure, should this part retry it at the next launch or wait for the person to ask? **A.** Retry the same hand-over at the next launch and whenever the person asks. (@gitdek)
- When a failed join is retried, should its count of earlier crash interruptions reset? **B.** Retrying a failed join keeps its earlier crash-interruption count, so after the limit is reached any further interrupted attempt reports failure. (@gitdek)

Checked within `Durations = {2, 3}`, `InterruptionLimit = 2`, `MaxPieces = 2`, `RecordingId = "recording-1"`, `SpoolPath = "spool/recording-1"`. To run the gate yourself:

```bash
go run ./cmd/invariant verify core/pieces
```
