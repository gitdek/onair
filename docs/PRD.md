# On Air: what it is meant to be

On Air is a menu-bar recorder for macOS that keeps a meeting's recording playable on disk from its first seconds, so nothing is left to fail when Stop is pressed. Each finished recording goes to On the Record by itself. Later milestones make it the live side of On the Record: while a meeting happens, it shows what the conversation connects to.

This is the first cut. The decisions below are proposals until the owner settles them, and only milestone one is specified.

## Why

A meeting recorded with macOS's own Screenshot app was lost when the recording failed at the end. That recorder writes a recording's index only when it stops, so a failure in the last step costs the whole recording. An earlier recording ran on across two meetings and the talk between them.

## Decisions

| # | Decision | Proposed |
| :-- | :-- | :-- |
| 1 | What it records | The whole main display. Picking one window comes later. |
| 2 | Sound | The call's sound and the microphone, mixed into one track, which is what On the Record's transcription and speaker naming read today. |
| 3 | If On Air dies mid-meeting | It saves what it has and starts recording again at once, with a notice and the gap marked. |
| 4 | A recording that runs long | It asks at 2 hours and stops at 4, unless told to keep going. |
| 5 | Where finished recordings go | Straight into On the Record's folder, picked once in On Air's settings. |
| 6 | Picture | Up to 1080p at 15 frames a second, in H.264: name tiles stay readable, any browser plays it, and an hour is under 1 GB. |
| 7 | The pieces on disk after hand-over | Kept 3 days as a safety net, then deleted. |

## Milestone one: never lose a recording

- **In the menu bar.** A dot at the top of the screen: grey when idle, red with a running clock while recording. One click gives Start, Stop, the last recording and any error. A keyboard shortcut starts and stops it too.
- **While recording.** Picture and sound go to disk in short pieces, a few seconds each, and every finished piece plays on its own. Nothing is held back for the end.
- **At Stop.** On Air closes the last piece, which takes about a second however long the meeting ran. It then joins the pieces into one file and hands it to On the Record, where the meeting appears on the shelf by itself.
- **If something dies.** If On Air, its recorder or the Mac stops mid-meeting, the pieces written so far are still there. When On Air comes back it carries on with the same recording, marks the gap and says so.
- **If the disk runs low or the meeting runs long.** On Air won't start without room to spare, stops cleanly before the disk fills, and asks before a recording passes two hours.
- **If the join fails.** The pieces are the recording. On Air hands over what plays and shows the error.

## Rules that must always hold

| # | Rule | How it's held |
| :-- | :-- | :-- |
| 1 | One recording at a time. A second Start while one is running is refused, and says so. | Proved |
| 2 | Every recording that starts ends in exactly one of two ways: handed over, or kept on disk with its error shown. None is dropped silently. | Proved |
| 3 | A piece that was closed is never changed or removed until its recording has been handed over. | Proved |
| 4 | After any stop, crash or power loss, On Air can tell which pieces are whole, and loses at most the piece that was open. | Proved for the bookkeeping, tried by hand for the files |
| 5 | On the Record's folder never shows a partial file. The finished file appears under its name only when it is whole. | Proved, and tested on a real folder |
| 6 | Each recording is handed over once. A retry after a crash never leaves two copies. | Proved |
| 7 | On Air removes its own copy only after the file in the folder matches it byte for byte, and after the days in decision 7. | Proved |
| 8 | On Air never starts with less than 20 GB free, and stops cleanly before free space falls below 10 GB. | Proved, and tested with a fake disk |
| 9 | A failed join never costs the recording. The pieces that play are handed over instead, and the error is shown. | Proved, and tried by hand |
| 10 | Stop takes about a second, however long the recording ran. | Tested with a fake recorder, and tried by hand |
| 11 | In On the Record's folder On Air writes only its own files. It never changes or removes anything else there. | Tested |
| 12 | On Air records only between Start and Stop, shows that it is recording the whole time, and sends nothing off the Mac. | Tried by hand. A test checks the core has no network code |

**Proved** means two things together: a small model of the rule, which a model checker explores in every order of events at small sizes, crashes included, and Go code proved to follow that model. It covers On Air's own decisions. It doesn't cover macOS, the disk or the encoder, which is what the hand checks are for.

## How it's built

On Air is one Go app in two layers, and only one layer makes decisions.

- **The edge** touches macOS: it draws the menu-bar dot, captures the screen and the sound, and reports what happened. It is a few small Objective-C files behind cgo, written by hand, and it builds only on darwin.
- **The core** decides when to start and stop, keeps track of which pieces are whole, joins them and hands the file over. It is plain Go that builds and tests anywhere. Its four proved parts are one recording at a time, the pieces' bookkeeping, the disk guard and the hand-over, with ordinary tested Go around them.

What On the Record needs from a finished recording:

- A `.mp4` or `.mov` in the folder it watches. It ignores names that start with `.`, so On Air writes under such a name and renames the file when it is whole.
- A name that carries the date and a title, which On the Record reads off the file name.
- Sound in the file, and name tiles sharp enough to read, since it names speakers from both.

## Trying milestone one by hand

Milestone one is done when these checks pass on the owner's Mac.

1. Record a 60-minute call. The finished file plays from start to end in On the Record, with every voice on it, and On the Record names the speakers as it does today.
2. Kill On Air's recorder ten minutes in. Recording carries on by itself, and the finished file has a gap of a few seconds at most.
3. Kill On Air and its recorder together, as a crash would. When On Air comes back, everything up to the last few seconds reaches On the Record without anyone doing anything.
4. Hold the power button mid-recording, once. After the Mac restarts, the same is true.
5. Record with the disk nearly full. On Air stops cleanly, hands over what it has, and says why.
6. Press Stop on a two-hour recording, then break the join on purpose. The menu bar is back to idle in about a second. The pieces arrive in On the Record in place of the joined file, and On Air shows the error.

## Later milestones

| Milestone | What it adds | Done when |
| :-- | :-- | :-- |
| 2. Stops when the meeting does | On Air stops by itself when the call ends, names the recording from the calendar, makes the lighter copy On the Record's Lab asks for, trims to the meeting, and leaves a transcript beside the video, so the meeting is searchable in On the Record minutes after it ends | A week of meetings is recorded without anyone touching Stop or the files |
| 3. Live context | While a meeting happens, a panel shows what its last minute connects to in On the Record: earlier moments, notebook entries and open items, each with a link. A decision model picks what is worth showing | The panel brings up the right earlier moment while the call is still on its topic, and a question gets a cited answer |

### Live context, in outline

Milestone three has two layers, one fast and one slow, and reuses what On the Record already runs.

1. **Hear and read.** The app keeps the last minute or so of the call as words, from macOS's on-device speech-to-text, and as screen text, from the Vision call On the Record uses for name tiles.
2. **Decide, fast.** Every few seconds a decision model gets that minute as state and answers typed questions: is a decision being made, is someone taking on a task, which open item is this about, is this passage worth showing. A hosted model or a local one sits behind the same interface.
3. **Fetch.** When something is worth it, the app asks On the Record's read-only door on the Mac what that minute connects to. `find_connections` answers with meeting moments, notebook entries and action items, each with a link.
4. **Show.** A small panel lists the two or three strongest, and a click opens the moment on the tape.
5. **Ask, slow.** For a real question, an agent with the meetings connector writes a cited brief. It runs when asked, never on every sentence.

What may leave the Mac during a call is the owners' decision before this milestone is built. Analysis of faces is a later idea: on the Mac only, off by default, and labelled as play.

## Not in milestone one

- Stopping by itself, calendar names, the lighter copy, trimming and the transcript: milestone two.
- Anything live: milestone three.
- Recording one window, pausing, and a second display.
- Running on any Mac but the owner's. It isn't signed for anyone else to install.

**What nothing here checks.** Whether macOS keeps delivering the picture and the sound, and whether they stay in step over a long call. The first hand check is the only guard on that, so it is worth repeating whenever macOS updates.
