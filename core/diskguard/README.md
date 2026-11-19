# Recording disk guard

Written by Invariant for [#10](https://github.com/gitdek/onair/issues/10): Never start without 20 GB free, and stop cleanly before free space falls below 10 GB.

@gitdek ratified these statements [on the issue](https://github.com/gitdek/onair/issues/10#issuecomment-7008406191). They're pinned by hash in [`ratified.lock`](.invariant/ratified.lock), and their text is in [`.invariant/specs/DiskGuard.tla`](.invariant/specs/DiskGuard.tla).

| Statement | Kind | Says |
| :-- | :-- | :-- |
| `Spec` | spec | The guard starts in Init and follows the modeled caller events, space decisions and piece writes. |
| `RoomToStart` | invariant | A new Start is allowed exactly when the reading supplied for that Start succeeds and shows at least the start threshold. |
| `ContinuationDecision` | invariant | A piece reading or resumption check permits capture exactly when its reading succeeds with at least the reserve plus two maximum pieces and no stop was already requested. |
| `OwnPiecesPreserveReserve` | invariant | With only the running recording's pieces consuming space and the stated write bounds respected, those pieces never reduce free space below the reserve. |
| `OneStop` | invariant | The live guard emits at most one disk stop before capture ends for good or the guard crashes. |
| `StopIsLatched` | invariant | A requested disk stop remains requested until capture ends for good or the guard crashes. |
| `NoCaptureWhileStopRequested` | invariant | No answer allows capture while a Stop is requested. |
| `TypeOK` | invariant | Every state value has its declared type and the parameters leave ample space between the start threshold and the stopping threshold. |
| `StartRefusalExplainsSpace` | invariant | Every refused Start supplies the menu with that Start's free-space reading or its failure and the required start threshold. |
| `UnsafeReadingRequestsStop` | invariant | A failed or insufficient piece or resumption reading has a disk stop requested in the same decision. |
| `StopEmissionIsExact` | invariant | A piece or resumption check emits a new stop exactly when its reading fails or falls below the stopping threshold and no stop was already requested. |
| `OrdinaryStopWithReason` | invariant | Every disk stop is the ordinary Stop command with the reason that the disk is nearly full. |
| `CaptureNeedsPermission` | invariant | Capture is admitted and runs only with an allowing decision and no requested disk stop. |
| `StartAtBoundary` | witness | A Start reading exactly at the start threshold can lead to running capture. |
| `StartBelowBoundaryRefused` | witness | A Start reading one unit below the start threshold can be refused. |
| `FailedStartRefused` | witness | A failed Start reading can be refused. |
| `ContinueAtBoundary` | witness | A running recording can continue at exactly the reserve plus two maximum pieces. |
| `StopJustBelowBoundary` | witness | A running recording can request its stop at the first reading one unit below the stopping threshold. |
| `FailedRunningReadStops` | witness | A failed reading during capture can request an ordinary disk stop. |
| `ResumeBelowStartLine` | witness | An existing recording can resume at the stopping threshold even though it has less than the start threshold free. |
| `UnsafeResumeFinishesExisting` | witness | An insufficient resumption reading can request finishing the existing recording without opening another piece. |
| `FailedResumeFinishesExisting` | witness | A failed resumption reading can request finishing the existing recording without opening another piece. |
| `StopSurvivesGoodReading` | witness | A later sufficient reading can leave a previous stop in force without emitting it again or permitting capture to resume. |
| `FinalPieceUsesLastRoom` | witness | The final open piece can finish at exactly the reserve after a disk stop. |
| `FinishedAndReusable` | witness | A recording stopped for disk space can close its final piece at the reserve, end for good, and leave the guard ready for a new Start with its old stop cleared. |
| `BugStaleStart` | bug | A possible implementation grants a new Start using old free-space information despite that Start's insufficient or failed reading. |
| `BugStartAtReserve` | bug | A possible implementation grants a new Start at the reserve instead of requiring the start threshold. |
| `BugFailedStartAllowed` | bug | A possible implementation treats a failed Start reading as permission to start. |
| `BugStopOnlyBelowReserve` | bug | A possible implementation waits until space is below the reserve and permits capture on a piece reading at or above the reserve but below the stopping threshold. |
| `BugOnlyOnePieceMargin` | bug | A possible implementation continues with room for only one maximum piece above the reserve. |
| `BugFailedRunningReadAllowed` | bug | A possible implementation continues recording after a failed running reading. |
| `BugResumeRequiresStartLine` | bug | A possible implementation refuses safe resumption because the reading is below the new-Start threshold. |
| `BugDuplicateStop` | bug | A possible implementation forgets the requested stop while answering a failed final-piece reading and emits a second disk stop. |
| `BugForgetStopOnGoodReading` | bug | A possible implementation forgets the requested stop while answering a sufficient final-piece reading and permits capture despite the Stop still in force. |
| `BugWithdrawStop` | bug | A possible implementation withdraws a requested disk stop when enough free space is available. |
| `BugWriteAfterFinalPiece` | bug | A possible implementation writes another piece after the stopped recording's final piece has exhausted its safe margin. |

Checked within `MaxFreeBytes = 22`, `MaxPieceBytes = 2`, `ReserveBytes = 10`, `StartBytes = 20`. To run the gate yourself:

```bash
go run ./cmd/invariant verify core/diskguard
```
