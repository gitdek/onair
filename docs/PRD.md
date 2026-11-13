# On Air: what it is meant to be

On Air sits in the menu bar during a meeting and does two things. It gets the meeting on tape in a way that can't be lost, and hands the tape to On the Record, a meeting archive. And while the meeting happens, it follows the conversation, decides fast what matters, and shows what On the Record already holds about it.

This is the first cut. The decisions below are proposals until they are settled, and only milestone one is specified in full.

## The whole idea

On Air has three jobs, and each one feeds the next.

1. **Record.** Picture and sound go to disk in short pieces, so the meeting is safe from its first seconds. This is milestone one, and it comes first because everything else stands on it.
2. **Follow.** On Air keeps the last minute of the call as text: the words, from on-device speech-to-text, and what is on screen, read off the frames on the Mac.
3. **Decide and connect.** Every few seconds a decision model answers typed questions about that minute. When an answer says something matters, On Air asks On the Record what it connects to, and shows the two or three strongest links beside the call.

### The live loop

The fast layer is a decision model in the style of jev: it is given state and typed questions, and answers with one of the options it was given, with probabilities, in well under a second. It never writes prose, and nothing waits on a long answer. A hosted model and a local one sit behind the same interface.

| Question about the last minute | Kind | What a yes or a high score does |
| :-- | :-- | :-- |
| Is a decision being made? | Yes or no | Marks the moment, and shows what was decided about the same thing before |
| Is someone taking on a task? | Yes or no | Marks it as a likely action item, and shows that person's open items |
| Which open item is this about? | One of the open items, or none | Shows that item's card: where it stands and what it waits on |
| Is a question being asked that the record can answer? | Yes or no | Asks On the Record, and shows the best passage with its link |
| How well does this passage fit what is being said? | A score | Drops weak matches, so the panel stays quiet |
| Has the topic changed? | Yes or no | Clears the panel and starts a fresh minute |
| Has the meeting ended? | Yes or no | Asks whether to stop recording |

The slow layer is an agent connected to On the Record. It writes a cited brief when asked a real question, which takes seconds to minutes, so it runs on request and never on every sentence.

Three rules come with the live side:

- Nothing the live side does can cost the recording. If the decision model, On the Record or the panel fails, recording carries on untouched.
- During a meeting On Air only reads from On the Record.
- Nothing leaves the Mac that its setting doesn't allow. With a local model, nothing leaves at all.

### Working with On the Record

On Air reads from On the Record during a meeting, and hands it files afterwards. It needs no models of its own for the matching, because On the Record's own search does that.

| When | What On Air does | How |
| :-- | :-- | :-- |
| At the start of a call | Reads the open items with the people on it | `list_action_items`, through On the Record's read-only door on the Mac |
| During the meeting | Asks what the last minute connects to: earlier moments, notebook entries and action items | `find_connections`, through the same door. Nothing leaves the Mac |
| When asked | Gets a cited brief on a topic | `gather_context`, by way of an agent |
| After the meeting | Hands over the recording | A file in the folder On the Record watches |
| After the meeting, from milestone two | Hands over a first transcript | A file beside the recording, in a shape On the Record already reads |

On Air never changes On the Record's board or notebook by itself. An action item it spots waits for a person to accept it.

## Why

macOS's own screen recorder writes a recording's index only when it stops, so a failure in that last step costs the whole recording, however long it ran. A recorder left running also captures whatever follows the meeting. On Air exists so that neither can happen.

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
| 9 | Which decision model | Built for both from the start: a hosted model and a local one behind one interface. Decision 10 says which one runs. |
| 10 | What may leave the Mac | A local model only, until the archive's owners agree that a call's words may go to a hosted one. |
| 11 | Where context shows | A slim panel at the side of the screen. The menu-bar dot shows a count when the panel is closed. |
| 12 | How often it looks | Every 10 seconds, and at once when the screen changes. |

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

The live loop and the interface with On the Record are described under [The whole idea](#the-whole-idea). Decisions 9 to 12 belong to milestone three and can wait until then.

## Not in milestone one

- Stopping by itself, calendar names, the lighter copy, trimming and the transcript: milestone two.
- Anything live: milestone three.
- Recording one window, pausing, and a second display.
- Running on any Mac but the owner's. It isn't signed for anyone else to install.

**What nothing here checks.** Whether macOS keeps delivering the picture and the sound, and whether they stay in step over a long call. The first hand check is the only guard on that, so it is worth repeating whenever macOS updates.
