`pieces` is the recording's saved bookkeeping. It performs no I/O. Construct a
`Recording` with a piece capacity, cumulative join interruption limit, stable
recording identity, and spool location. Durations use caller-chosen positive
integer units, consistently for pieces and gaps.

Serialize calls. Persist the entire `Recording` atomically after each accepted
operation and **before** executing its resulting request. A method returning
`false` leaves the record unchanged. The public fields support whole-record
persistence; restore them together rather than editing individual fields.

The caller interprets `Meta.Phase` as follows:

| Phase | Caller work |
| --- | --- |
| `Write` | Write piece `Meta.Open` under its hidden name, flush its bytes, then report the flush. |
| `Name` | Give that flushed piece its final name without replacing an existing file, then report naming. |
| `DiscardPiece` | Discard the open piece's hidden remnant, then report its absence. |
| `Join` | Read pieces numbered `1` through `Meta.JoinCount`, once each in order, into a partial joined file. |
| `PublishJoin` | After the prepared manifest is saved, name the whole joined file without replacing an existing file, then report naming. |
| `DiscardJoinRetry`, `DiscardJoinFailed` | Discard any partial joined output and report its absence. |
| `Handover` | Hand over the single joined file using `ID` as the deduplication key, then report success or failure. |
| `Remove` | Remove the pieces and report completion once none remain. This request never removes the joined file. |

The caller supplies truthful result booleans to report methods; they are not
requests to inspect the disk. `ReportJoinPrepared` receives a boolean per piece
identifying omissions, records each omitted piece's original duration, and
refuses a manifest that omits every closed piece. Report an entirely
unreadable recording with `ReportJoinFailure("none readable", false, true)`.
The caller creates the corresponding timeline gaps in the joined file.

Drive `FinalizeCapture`, `RequestJoin`, `RequestHandover`, `ScheduleRetry`, and
`RequestRemoval` whenever applicable. These scheduling calls must not starve
while the application continues running. `CaptureEnded` allows a later
`CarryOn`; `FinishCapture` makes capture final. A write failure prevents further
capture and requests remnant cleanup before finalization.

On launch, restore the saved record and call `Recover` exactly once with the
authoritative named-piece listing and whether the joined file has its final
name. Save reconciliation before running any effects. Recovery accepts a piece
named before its result was saved, discards unfinished writes before their
number can be reused, and accepts a named joined file with its saved manifest.
It retries unresolved or failed hand-over using the same identity.

Failures retain the pieces and expose `Meta.Error` and `Meta.ErrorAt` for the
menu. `AskRetry` records a person's request; `ScheduleRetry` schedules it.
Launch also retries failed joins and hand-over. Interrupted joins accumulate up
to `Limit`, including across retries. Reaching the limit reports failure on
that launch; a subsequent launch or person-requested retry can still succeed.

After successful hand-over, call `ReportThreeDaysElapsed` only when the caller
has measured the full retention period. Until then, no removal is requested.
`Empty` means nothing was recorded; `Done` means piece cleanup is complete.

The unchanged TLA+ model and `explore.go` describe caller effects, crashes, and
verification history separately from this implementation. The proof concerns
bookkeeping; actual flush durability and media gap encoding remain caller
responsibilities.
