# Never start without 20 GB free, and stop cleanly before free space falls below 10 GB

Issue #10 in gitdek/onair, opened by @gitdek.

Project: core/diskguard

On Air is a menu-bar app that records a meeting's screen and sound, and hands each finished recording to a folder that a meeting archive watches. While it records, the picture and sound go to disk in pieces a few seconds long, under 1 GB an hour in all. This issue is the part of its core that keeps a recording from filling the disk. It holds one of the spec's rules:

- On Air never starts with less than 20 GB free, and stops cleanly before free space falls below 10 GB.

It decides, and it keeps nothing across a crash. It reads no disk itself: its caller reads how much space is free on the disk that holds On Air's pieces, and reports each reading. The part answers whether a recording may start and, while one runs, whether it must stop. Its caller also tells it when a recording starts running and when its capture has ended for good. So the part can be checked without a disk. On Air has no disk guard today.

When free space is read:

- When Start is pressed, for that Start.
- When capture is about to start again for a recording already under way, which carries on after its capture ended by itself or On Air died.
- While a recording runs, each time one of its pieces is closed. Pieces close every few seconds, and when one closes, the next is already being written.

What must always be true:

- **Room to start.** A recording starts only when the reading taken for its Start shows at least 20 GB free. Otherwise the Start is refused, and the menu says how much is free and how much a recording needs.
- **Stop before 10 GB.** While a recording runs, its pieces never take free space below 10 GB. When a reading comes, the piece already open will be written, and if the guard waits for the next reading, so will the one after it. So the guard asks for the stop at the first reading that leaves less than 10 GB plus room for two of the largest pieces.
- **One ordinary Stop.** The guard asks for the stop as an ordinary Stop, giving its reason, that the disk is nearly full. It asks once, and never takes it back.
- **No reading, no room.** A reading that fails counts as too little: a Start is refused, and a running recording is stopped.

Choices settled in advance:

- **What "cleanly" means.** This part only asks for the stop. The stop itself is the person's Stop, carried out by other parts: the open piece is closed, and the recording is joined and handed over. The join and the hand-over use the room the 10 GB line leaves, and this part doesn't guard them.
- **How often.** As listed above: the caller reads, and the part decides from the latest reading it was given.
- **The numbers are the model's constants.** 20 GB, 10 GB and the most one piece can take are constants: the model is checked with small stand-ins for them, and the code takes them as parameters, so the caller passes the real numbers, setting the largest a piece can be well above what a few seconds of recording take. A GB is a billion bytes, as macOS counts it. The start line is far above the stop line plus a few pieces, so a recording that starts always has room to record.
- **What the guard can promise.** Between two readings, the running recording writes at most the piece that is open, and no piece is larger than the set size. Once the guard has asked for the stop, nothing more is written than the piece open then. Everything else that uses the disk, another program or On Air finishing an earlier recording, is seen at the next reading, and if there is too little room by then, the recording stops at once. The model counts only the running recording's pieces.
- **Carrying on isn't a new start.** Capture starting again for a recording already under way is held to the same test as a running recording, not to the 20 GB line: if the reading taken for it means the recording would have to stop, capture doesn't start again, and the recording is finished with what it has.

If something here still leaves a real choice open, ask about one choice at a time: a question with several choices and a preview of each is too large to post.

Mistakes worth guarding against:

- Starting on an old reading, such as the last one taken during the recording before, instead of the one taken for this Start.
- Holding a Start to the 10 GB line instead of the 20 GB line.
- Stopping only once a reading is already below 10 GB, so the open piece takes free space lower still.
- Leaving room for only the open piece, so the piece after it, written before the next reading, takes free space below 10 GB.
- Treating a reading that failed as room to spare.

Out of scope here: how free space is read, and which of the measures macOS gives is used; the stop itself, closing the last piece, joining and handing over, which other parts do; whether a recording is already running, which another part decides; the archive's folder, which may be on another disk; and how long a recording may run.

## Discussion

**@gitdek:**

Three changes before this is ratified.

1. "Stopping only once a reading is already below 10 GB" has no planted bug. Plant one that lets capture continue on a piece's reading at or above the reserve but below the stop line. It must break `ContinuationDecision`.
2. `FinishedAndReusable` is reached in one step by a Start refused at exactly the reserve, because `stopCount` is 0 in every idle state. Require a recording that was stopped for the disk and then ended for good, or drop it.
3. "Asks once, and never takes it back" isn't really stated: `stopCount` is set, not counted, and nothing ties `wasStopped` to the Stop in force, so a later reading that forgets its Stop can emit a second stop or allow capture, and still passes every invariant. Count each emitted stop in `stopCount`, add "no answer allows capture while a Stop is requested", and plant `BugDuplicateStop` as that forgetful reading: on a failed reading it must break `OneStop`, and on a good one the new invariant.
