# Keep every closed piece until its recording is handed over, through crashes and failed joins

Issue #9 in gitdek/onair, opened by @gitdek.

Project: core/pieces

On Air is a menu-bar app that records a meeting's screen and sound, and hands each finished recording to a folder that a meeting archive watches. While it records, the picture and sound go to disk in pieces a few seconds long, each of which plays on its own, so a crash costs at most the piece being written. When the recording is over, its pieces are joined into one file, and that file is handed over. This issue is the part of the core that keeps track of a recording's pieces: which are whole, what a crash lost, what goes into the join, what happens when the join fails, and when the pieces may be removed. It holds three of the spec's rules:

- A piece that was closed is never changed or removed until its recording has been handed over.
- After any stop, crash or power loss, On Air can tell which pieces are whole, and loses at most the piece that was open.
- On Air never hands On the Record more than one file for a recording. A failed join never costs the recording: the pieces stay in On Air's spool, untouched and never removed while the recording is unjoined, the menu says what failed and where they are, and the join is tried again at the next launch and when the person asks. If a piece itself can't be read, the join leaves it out and the rest is handed over as one file, with the gap marked: the file jumps in time where the piece was, and the menu says how long. Pieces it couldn't read stay in the spool.

It decides, and it keeps its own record of where a recording's pieces stand. It touches no file itself. The steps it asks for are carried out by its caller, which reports what happened: write a piece's bytes under a hidden name, give the piece its name, discard what an open piece left behind, join the closed pieces into one file, hand over the joined file, and remove the pieces. The join reports the pieces it couldn't read and left out. It is also told when a recording's capture has ended for good and the recording is to be finished, and when the person asks for a failed join to be tried again. So the part can be checked without a disk. Its caller saves the record whole before carrying out any step the part asks for, and gives it back at the next launch. The caller's join only reads the pieces, and gives its file its name only once the file is whole, never replacing a file already there. The second rule is proved for this bookkeeping only: whether bytes really survive a power loss is up to the disk, and is tried by hand.

Words used here:

- A piece is **open** while its bytes are being written under a hidden name, and **closed** once all its bytes are on disk and it has been given its name. On disk means flushed so that it survives a power loss, which the caller makes sure of before it reports. A closed piece is **whole**: all its bytes are on disk under its name. A piece without its name is never whole. Whether a piece can be read is for the caller's join to find out.
- Pieces are numbered from 1 in the order they are written, and one is open at a time: the next is written only after the one before it is closed.
- **On Air dies** at any moment, by a crash, a kill or a power loss. What it held in memory is lost, and what is on disk stays. At the next launch the caller lists what is on disk: the pieces that have their names, what an open piece left under its hidden name, and the joined file, if it has its name.
- **Handing over** is another part's work: it copies a file into the archive's folder and checks the copy. This part asks for it and is told whether it worked.
- A recording can **carry on** after its capture ended by itself or On Air died, which another part decides. Its next pieces then follow its last closed one.

What must always be true:

- **Closed pieces are left alone.** A closed piece is never written to, replaced or removed until its recording has been handed over and the time to keep its pieces afterwards has passed. A new piece never takes the number of a closed one.
- **Whole means named.** A piece is given its name only after all its bytes are on disk, and only pieces with their names count as whole.
- **A crash loses at most the open piece.** After On Air dies, every piece that was closed still counts as whole, and nothing but the piece that was open is lost.
- **The join holds the recording.** The join is asked for only when no piece is open, and it is given every closed piece once, in order, and nothing else.
- **One file, never the pieces.** Nothing is handed over for a recording but its one joined file. A piece is never handed over.
- **A failed join never costs the recording.** When the join fails, nothing is handed over. Every closed piece stays on disk, untouched, and none is removed while the recording is unjoined. The join's error goes with the outcome, for the menu to show with where the pieces are, and the join is tried again at the next launch and when the person asks.
- **A piece that can't be read is left out, not lost.** When the join couldn't read a piece, it leaves it out and the rest becomes one file, with a jump in time where the piece was. The outcome says how long each gap is, for the menu to show, and the pieces the join couldn't read stay on disk with the rest.
- **Handed over, or kept.** The recording counts as handed over once its joined file has been handed over. If the join fails, or the hand-over fails, the recording is kept: all its pieces stay, and the error goes with the outcome.

Choices settled in advance:

- **After a crash, the disk says what is whole.** A piece can get its name just before On Air dies, before the record says so. So after a crash the pieces named on disk are the closed ones, whatever the record last said, and the next piece is numbered after the last of them.
- **What an open piece left behind,** after a crash or a failed write, is discarded before anything takes its number. It is never joined or handed over.
- **A piece that can't be written** isn't closed. The part takes no further piece from that capture, and reports that the recording can't go on. Its caller then stops it, and what was closed is finished as usual.
- **Which pieces can be read** is found out by the caller's join, which reports the ones it left out. This part decodes nothing. A join that could read none of them has failed.
- **A join that fails is tried again,** at the next launch and whenever the person asks, with every closed piece, as the first time. Until one works, nothing is handed over and the recording is kept. A hand-over that fails keeps the recording too.
- **A join cut short by On Air dying hasn't failed.** At the next launch, if its file has its name, the join is done, since only a whole file gets its name. Otherwise its partial file is discarded and the join starts again. A join cut short a set number of times counts as failed. The number is one of the model's bounds.
- **When the pieces may go.** The spec keeps a recording's pieces for 3 days after it was handed over, as a safety net, and then removes them. The caller measures the time and reports when it has passed. The joined file is the hand-over part's to remove, not this one's. A recording that hasn't been handed over keeps all its pieces, however long it waits.
- **A recording with no closed piece** has nothing to join or hand over, and the outcome says that nothing was recorded.

If something here still leaves a real choice open, ask about one choice at a time: a question with several choices and a preview of each is too large to post.

Mistakes worth guarding against:

- Naming a piece before all its bytes are on disk.
- After a crash, numbering the next piece from the record's count instead of from the pieces named on disk, so a piece named just before the crash is replaced.
- Removing the pieces as soon as the join succeeds, before the joined file has been handed over.
- Joining while a piece is still open, so the join leaves it out.
- Handing over a piece, or more than one file for a recording.
- When the join fails, removing a piece, or never trying the join again.
- Leaving a piece out of the join without the outcome saying how long the gap is.
- Joining again after a crash that fell between the joined file getting its name and the record saying so: the name is taken, so the join fails, and fails again each time it is tried, though a whole joined file is there.

Today's spool, ordinary Go that nothing proves, already names a piece only once its bytes are flushed, takes the named pieces as the whole ones after a kill, joins them in order without changing them, and never lets a join replace a file. This part proves those decisions, and adds what the spool doesn't do yet: trying a failed join again when the person asks (today the menu says where the pieces are, and only the next launch tries again), leaving out a piece the join can't read (today such a piece fails the whole join), removing the pieces, which today never happens, only after the hand-over and the days after it, and numbering a recording's pieces when it carries on after a crash.

Out of scope here: the files' format, which is how a piece plays on its own, how the join writes its file and how a gap is marked in it, from a recording that carried on or a piece left out; how the join finds that a piece can't be read; the hand-over's own steps, which another part decides; how the person asks for a join again; when a recording starts, stops or carries on, which another part decides; and how the time to keep the pieces is measured.

## Decided

- **F1. If On Air dies after the joined file gets its name but before the join result is saved, how should the gap information survive?** B. The caller reports the omitted pieces and gap durations before naming the joined file, and waits until this part has saved them. (decided by @gitdek: https://github.com/gitdek/onair/issues/9#issuecomment-6881814908)
- **F2. If On Air dies during hand-over before its result is saved, how should this part recover the unresolved hand-over?** A. The part retries the same hand-over request after recovery, and the hand-over part deduplicates it using the recording’s stable identity. (decided by @gitdek: https://github.com/gitdek/onair/issues/9#issuecomment-6895516962)
- **F3. After hand-over explicitly reports failure, should this part retry it at the next launch or wait for the person to ask?** A. Retry the same hand-over at the next launch and whenever the person asks. (decided by @gitdek: https://github.com/gitdek/onair/issues/9#issuecomment-6898519215)
- **F4. When a failed join is retried, should its count of earlier crash interruptions reset?** B. Retrying a failed join keeps its earlier crash-interruption count, so after the limit is reached any further interrupted attempt reports failure. (decided by @gitdek: https://github.com/gitdek/onair/issues/9#issuecomment-6898732510)

## Discussion

**@gitdek:**

Keep every statement; four changes before this is ratified.

1. Four of the mistakes the issue lists have no planted bug. Plant one for each: after a crash, numbering the next piece from the record's count instead of from the pieces named on disk; joining while a piece is open; handing over a piece, or the pieces, when the join fails; and, when the join fails, removing a piece or never trying the join again.
2. Plant the rejoin mistake where the issue puts it: at the launch after a crash that fell between the joined file getting its name and the record saying so. Today that bug is a step taken while On Air is running.
3. This part learns what is on disk only at launch, so asking for the next piece must not check the disk: with that check, a numbering mistake can never reuse a closed piece's number, and the statement that guards against it can't fail. And naming must not rely on the caller refusing a piece that isn't flushed: say that this part asks for a piece's name only after the caller has reported its bytes on disk.
4. Say that a join cut short the set number of times counts as failed. Today only the reverse is stated.
