# Recording sessions

Written by Invariant for [#8](https://github.com/gitdek/onair/issues/8): Run one recording at a time, and end each one handed over or kept with its error shown.

@gitdek ratified these statements [on the issue](https://github.com/gitdek/onair/issues/8#issuecomment-6881580339). They're pinned by hash in [`ratified.lock`](.invariant/ratified.lock), and their text is in [`.invariant/specs/RecordingSession.tla`](.invariant/specs/RecordingSession.tla).

| Statement | Kind | Says |
| :-- | :-- | :-- |
| `Spec` | spec | The session starts in its initial state and changes only by the modeled steps. |
| `OneRunning` | invariant | At most one recording is running, including while its capture is starting or waiting to restart. |
| `EveryAcceptedIsAccountedFor` | invariant | Every accepted recording stays in the saved record until it has ended and its end has actually been shown. |
| `StopIsLatched` | invariant | A Stop is saved even during startup before capture is asked to stop, and a Stop naming no running recording changes nothing. |
| `FinishAfterCapture` | invariant | Finishing is requested only after that recording’s capture has ended for good and can no longer write pieces. |
| `TypeOK` | invariant | The saved records, requests, reported results and specification histories have the declared bounded types. |
| `StartDecision` | invariant | A Start reaching this component is accepted whenever no recording is running, and otherwise is refused with a menu notice without changing the saved recordings. |
| `CaptureBelongsToRunning` | invariant | Every capture request belongs to the running recording and uses its original capture selection. |
| `ExactlyOneEnd` | invariant | An accepted recording has at most one end, whose reported result stays unchanged while its end awaits acknowledgement. |
| `KeptHasError` | invariant | Every kept recording has the error reported by finishing. |
| `NothingRecordedIsKept` | invariant | A report that nothing was recorded ends kept with an error saying that nothing was recorded and giving the reason its capture ended for good. |
| `UnshownEndIsSaved` | invariant | Every end that has not been shown is still in the saved record. |
| `EndNoticeIsExact` | invariant | Each end notice gives the exact reported outcome and error and identifies the recording’s original pieces directory. |
| `StartingStopIsImmediate` | invariant | When capture reports that startup succeeded after a saved Stop, the session immediately asks that capture to stop. |
| `NoStartAfterStop` | invariant | No capture start or restart is requested for a recording after its Stop has been accepted. |
| `AutomaticStopExplained` | invariant | An automatic Stop of the named running recording emits a menu notice carrying its caller-provided reason. |
| `RestartBudgetIsSaved` | invariant | Each restart request consumes one saved retry before capture is asked to start, including requests interrupted by a crash, and never exceeds the bound. |
| `RestartShowsGap` | invariant | A successful restart uses the same recording and emits one gap notice, while an initial start emits no gap notice. |
| `OneFinishInOrder` | invariant | Only the earliest recording whose capture ended for good and whose finish is unanswered may be finishing, with at most one finish request in progress. |
| `GapKeepsRecording` | invariant | An unexpected capture end or a new launch keeps the same recording open for restart if it has no saved Stop and still has retry budget. |
| `CaptureFailureCloses` | invariant | When the caller reports that capture cannot start, the recording’s capture ends for good and its reported reason accompanies the queued finish. |
| `AcknowledgedEndsAreRemoved` | invariant | Once the caller acknowledges that an end was displayed, its recording is removed from the saved record. |
| `CrashKeepsSavedRecord` | invariant | A crash ends all capture and discards pending caller requests while preserving the whole saved record and finishing order. |
| `StoppedOrClosedEndsAndIsShown` | property | If On Air eventually keeps running and its caller answers each request, every stopped recording or recording whose capture ended for good eventually ends, has its end shown and leaves the saved record. |
| `GapsAreResolved` | property | If On Air eventually keeps running and its caller answers each request, every gap eventually leads to a counted restart request or capture ending for good. |
| `ResolveGapFair` | fairness | The session eventually resolves a pending gap whenever resolving it stays possible. |
| `SendStopFair` | fairness | The session eventually asks started capture to stop whenever a saved Stop keeps that request possible. |
| `FinishDispatchFair` | fairness | The session eventually requests the next finish whenever dispatching it stays possible. |
| `EndNoticeDispatchFair` | fairness | The session eventually requests an unacknowledged end notice whenever requesting that notice stays possible. |
| `HandedOver` | witness | A recording can end handed over. |
| `HandedOverWithWarning` | witness | A recording can end handed over with a finishing error to show. |
| `KeptWithError` | witness | A recording can end kept with a finishing error. |
| `NothingRecorded` | witness | A recording can end kept because nothing was recorded. |
| `EndAcknowledged` | witness | All accepted recordings can have their ends shown and leave the saved record. |
| `StartDuringFinishing` | witness | A new recording can be running while an earlier one is being finished. |
| `RefusalIsPossible` | witness | A Start can be refused with its menu notice while another recording is running. |
| `StopDuringStarting` | witness | A Stop can be saved while capture is still starting. |
| `GapIsPossible` | witness | Capture can restart in the same recording and emit a gap notice. |
| `ExhaustedRestarts` | witness | A recording can reach its restart limit and be queued for finishing. |
| `CrashWithUnshownEnd` | witness | A crash can leave an unshown end saved for the next launch. |
| `CrashDuringRestart` | witness | A crash can interrupt a restart whose attempt has already been counted. |
| `RestartAfterInterruptedRestart` | witness | After a crash interrupts a counted restart, the same recording can request one more restart with both attempts counted. |
| `StartUnchecked` | bug | A faulty Start accepts a second recording while one is running. |
| `ForgetWhenFinishRequested` | bug | A faulty finish dispatch forgets the recording before finishing has replied. |
| `ForgetWhenNoticeRequested` | bug | A faulty end notice forgets the recording before the caller confirms display. |
| `IgnoreStopDuringStarting` | bug | A faulty Stop received during startup leaves the Stop unsaved. |
| `FinishWhileWriting` | bug | A faulty finish dispatch joins pieces while capture can still write them. |
| `RefuseStartWhileFinishing` | bug | A faulty Start is refused while an earlier recording is being finished even though no recording is running. |
| `StopBeforeSaved` | bug | A faulty Stop asks capture to stop before saving the Stop in the record. |

Checked within `Errors = {fault}`, `MaxRestarts = 2`, `Recordings = {r1, r2}`. To run the gate yourself:

```bash
go run ./cmd/invariant verify core/session
```
