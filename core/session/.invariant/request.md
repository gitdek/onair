# Run one recording at a time, and end each one handed over or kept with its error shown

Issue #8 in gitdek/onair, opened by @gitdek.

Project: core/session

On Air is a menu-bar app that records a meeting's screen and sound, and hands each finished recording to a folder that a meeting archive watches. This issue is the part of its core that decides a recording's life: when one may start, when it stops, what happens when capture ends by itself or On Air dies, and that every recording that starts comes to an end the person is told about. It holds two of the spec's rules:

- One recording at a time. A second Start while one is running is refused, and says so.
- Every recording that starts ends in exactly one of two ways: handed over, or kept on disk with its error shown. None is dropped silently.

It decides, and it keeps its own record of every recording it has started and how far each has got. It touches no file, screen or capture itself. The steps it asks for are carried out by its caller, which reports what happened: start capture, stop capture, finish a recording, and say something in the menu. So the part can be checked without a Mac. Its caller saves the record whole before carrying out any step the part asks for, and gives it back at the next launch.

Words used here:

- **Capture** takes the screen and sound and writes them to disk in pieces a few seconds long. The caller reports when capture has started, when it couldn't start, and when it has ended. Capture **ends by itself** when it ends without anyone asking it to stop: its recorder died, or macOS stopped it for a fault.
- A recording is **running** from the moment its Start is accepted until its capture has ended for good: after a stop, or when it can't be carried on. A Start counts as accepted once it is in the record.
- **Finishing** comes after that, and two other parts do it: one joins the recording's pieces into one file, and keeps the pieces where they are if the join fails, and the other hands the file to the archive's folder. The archive is never handed more than one file for a recording. This part asks for a recording to be finished and is told how it went: handed over, perhaps with an error to show (when the join left out a piece it couldn't read, and the gap's length is shown), or not handed over, with its error. Removing On Air's own copies days later is separate work, and doesn't hold up a recording's end.
- A recording **ends** handed over, or **kept**: not handed over, with its error shown, and everything it recorded left on disk by the other parts.
- **On Air dies** at any moment, by a crash, a kill or a power loss, and its capture ends with it: the caller makes sure of that, even if capture runs in a process of its own. Only the saved record survives, and the next launch carries on from it.

What must always be true:

- **One at a time.** While a recording is running, capture is never asked to start for another one, and a Start is refused and the menu says so. A refused Start changes nothing about the running recording. When no recording is running, a Start is accepted, even while an earlier recording is still being finished.
- **Every recording ends once.** Every recording whose Start was accepted ends in exactly one of the two ways: never both, never twice, and never in any other way. On Air dying at any moment never loses one: it stays in the record until it has ended and its end has been shown.
- **Every end is shown.** The menu says how each recording ended: handed over, with any error its finishing reported, or kept, with its error and where its pieces are. If On Air dies before the menu has said it, the next launch says it.
- **Finished only after capture.** A recording is asked to be finished only after its capture has ended for good, so nothing is still writing its pieces when they are joined.
- **A Stop is never lost.** A Stop is taken at any time while a recording is running, and does nothing when none is. A Stop while capture is still starting stops it as soon as it has started. Once a recording has been stopped, capture is never asked to start for it again, after a crash or otherwise.
- **A gap doesn't end a recording.** When capture ends by itself while a recording is running, or On Air dies while one is, capture is asked to start again into the same recording, and once it has, the menu says there was a gap. If it can't start again, for whatever reason the caller gives, or has already been started again a set number of times in this recording, the recording's capture has ended for good, and it is finished with what it has. The number is one of the model's bounds.
- **Nothing waits forever.** Once a recording has been stopped, or its capture has ended for good, it ends and its end is shown, as long as On Air keeps running and its caller answers each step.

Choices settled in advance:

- **Where a Stop comes from.** From the person, from Quit, or from On Air itself. When the person stops capture from macOS's own controls, the caller reports that as their Stop. Other parts ask for a stop when the disk is nearly full or the recording has run too long, and the caller asks for one when the window being recorded closes or a piece can't be written. This part treats them all alike, and the menu gives the reason for a stop On Air made itself. A stop On Air asks for names its recording, and does nothing if that recording is no longer running. Quit is a Stop, after which On Air may end; a recording that isn't finished by then is finished at the next launch.
- **The caller may decline to carry on.** It reports that capture couldn't start again when carrying on makes no sense, for instance when On Air was down too long for the meeting to be still going. How long that is, is the caller's to decide, outside this part.
- **Capture's own fallbacks stay with the caller.** When capture can't start as asked, the caller tries again without the microphone, or records the whole display in place of a pick that's gone, before it reports anything. This part sees only that capture started or couldn't. Capture started again for a recording records the same display, area or window as before, never another in its place: if that's gone, capture can't start again.
- **Counting starts again.** The set number counts every time capture is started again for a recording, whether its capture ended by itself or On Air died, and the count is kept in the record.
- **A recording that recorded nothing.** If capture never wrote a whole piece, because it couldn't start or ended at once, finishing reports nothing to hand over. The recording ends the second way, and its error says that nothing was recorded and why.
- **One finish at a time.** Recordings are finished one at a time, in the order their capture ended for good. If On Air dies while one is being finished, the next launch asks for the same recording to be finished again: the other parts carry on from their own records, so asking again is safe.
- **What is kept until it is said.** Only how a recording ended stays in the record until the menu has said it. A refusal and a gap are said once, and aren't kept. Saying something twice is harmless.
- **Kept is where it ends here.** A kept recording stays kept: handing it over later is separate work. A recording leaves the record once it has ended and its end has been shown.
- **Each recording has its own identity**, which the caller gives it and never reuses.
- **A restart that may not have happened.** If On Air dies after asking for capture to start again and before the caller says whether it started, that restart still counts toward the set number. So a crash during restarts can't repeat forever. Settled on 14 November 2026, in answer to the first draft's question.

If something here still leaves a real choice open, ask about one choice at a time: a question with several choices and a preview of each is too large to post.

Mistakes worth guarding against:

- Accepting a Start while a recording is running, so capture is asked to start for a second one.
- Refusing a Start because an earlier recording is still being finished.
- Ignoring a Stop that comes while capture is starting, so capture runs on after it.
- Asking capture to stop before the Stop is in the record, so On Air dying in between makes the next launch start the stopped recording again.
- Asking for a recording to be finished while its capture can still write pieces.
- Taking a recording out of the record when its finishing is asked for rather than when it has ended, so On Air dying during the finish forgets it.
- Counting an end as shown as soon as this part asks for it, so On Air dying before the menu says it leaves it unsaid.

How this differs from the recorder On Air has today. Today's recorder, ordinary Go that nothing proves, already refuses a second Start and says so, joins a recording only after capture's last piece, keeps what was recorded when capture ends by itself, and says each recording it recovers after a kill. This part keeps those, and differs in four places:

- Today a Start is refused while the last recording is still being joined, with a note that says On Air is recording although the dot is grey. Here it is accepted once that recording's capture has ended for good.
- Today a Stop while capture is starting is ignored, though a Quit then is kept. Here a Stop is kept as Quit is.
- Today capture that ends by itself ends the recording. Here the recording carries on, as the spec decided: if On Air dies mid-meeting, it saves what it has and starts recording again at once, with a notice and the gap marked.
- Today a recording cut short by On Air dying is joined as it stands at the next launch. Here it carries on, unless it had been stopped or the caller declines, and then it is finished.

Out of scope here: what is recorded; writing the pieces, joining them and handing them over, which other parts decide; whether the disk has room to start or go on, and how long a recording may run, which other parts decide too (a Start refused for want of room never reaches this part, and a stop they ask for comes here as a Stop); how On Air is started again after it dies; what the menu-bar dot shows; handing a kept recording over again later; and how a recording is named.

## Discussion

**@gitdek:**

Keep every statement; change four things.

1. Check the restart limit at 2, not 1, and add a witness where a restart cut short by a crash is followed by one more restart. At 1, a model where a cut-short restart ends the recording, or uses up every restart, passes every invariant.
2. Add known bugs for the two listed mistakes that don't have one: refusing a Start while an earlier recording is being finished (it must break `StartDecision`), and asking capture to stop before the Stop is in the record (it must break `StopIsLatched`).
3. Make both Forget bugs drop the record in the step that asks for the finish or for the end notice, as the issue describes. Today they drop it without asking.
4. `NothingRecordedIsKept` accepts any reason. Require the reason the recording's capture ended.

The issue's text also had one leftover from the old join rule, now corrected: a recording handed over with an error is one whose join left out a piece it couldn't read.
