# Hand a finished recording over once and whole, and only then let go of it

Issue #1 in gitdek/onair, opened by @gitdek.

Project: core/handover

On Air hands each finished recording to a folder that another program watches. `docs/PRD.md` gives the rules: 5, 6, 7 and 11. This issue is the part of the core that decides each step of that hand-over.

It decides, and it keeps its own record of where a hand-over stands. It touches no file itself. The steps it asks for are carried out by its caller, which reports what happened: copy the recording into the folder under a hidden name, compare the copy with the original, give the copy its real name, remove the original. So the part can be checked without a disk.

A recording to hand over is a finished file in On Air's own spool, with the name it will have in the folder. A hand-over can be interrupted at any step, by a crash or by its caller stopping, and is then carried on from its record.

What must always be true:

- The folder never shows a partial file under the recording's name. The copy is made under a hidden name, one that starts with a dot, and takes the recording's name only after it has been compared with the spool's file and found the same.
- A recording is handed over at most once. Carrying on after an interruption, at whatever step it fell, never leaves a second file under the recording's name, and never starts a second copy while one is under way.
- On Air's own file is removed only after the folder's file is under its name and was found the same, and only once the time to keep it has passed.
- A copy that isn't the same as the spool's file is removed and made again. It is never given the recording's name.
- A hand-over that can't go on, because the folder is gone or full, ends as failed with its reason, with the spool's file still there. It can be tried again later.
- Nothing in the folder is ever asked to be changed or removed except under the hand-over's own two names: the hidden one and the recording's.
- A hand-over that was started ends, as handed over or as failed with its reason, unless it is interrupted for good.

Settled on 13 November 2026, in answer to four questions the first draft raised:

- **Repeating a step is safe.** When an interruption leaves a step's result unknown, the core asks for the same step again under the same identity. Its caller guarantees that repetitions share one execution and return its original result. That holds too when the copy was given its name before the record of that was kept.
- **A name that's taken.** If the recording's name already exists in the folder and isn't known to be this hand-over's own, the core has the existing file compared with the spool's. An identical file is accepted as the hand-over, and nothing is copied again. A different one fails the hand-over with a name conflict, and is left exactly as it is.
- **A copy that keeps differing.** After a set number of copies that differ from the spool's file, the core has the last one removed and fails the hand-over with that reason, keeping the spool's file. The number is one of the model's bounds.
- **When it's done.** A hand-over counts as handed over once the copy is under its name and was found the same. Keeping the spool's file for its time, and removing it afterwards, are separate work that can be tried again, and failing at it doesn't undo the hand-over.

If something here still leaves a real choice open, ask about one choice at a time: a question with several choices and a preview of each is too large to post.

Mistakes worth guarding against:

- Giving the copy its name before comparing it.
- Removing the spool's file before the copy has its name.
- Starting a fresh copy after a crash that fell between giving the copy its name and recording that, which leaves two files.
- Removing a file in the folder that isn't the hand-over's.

Out of scope here: the copying and comparing themselves, how the time to keep a file is measured, and choosing the recording's name.
