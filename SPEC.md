# On Air: what is built

This page describes On Air as it is. [`docs/PRD.md`](docs/PRD.md) describes what it is meant to become.

Milestone one is under way. Two parts exist: a menu-bar recorder that keeps each recording in pieces on disk, and the proved core of the hand-over, which nothing calls yet. Nothing here has been through the milestone's hand checks.

## The recorder

What it does:

- **The menu bar.** A dot, grey when idle and red with a running clock while recording. Its menu has Start or Stop, a note when there is something to say, what to record, the last recording, the recordings folder and Quit. There is no keyboard shortcut yet.
- **What it records.** What a person picks under Record in the menu: one of the connected displays, listed by name and size, an area of the picked display, or a single window. Until they pick, the whole main display. The picture keeps the shape of what it records, brought down to at most 1080 rows with even sides, at most 15 frames a second, in H.264 at 1.8 Mb/s, with the pointer. The sound and the microphone are mixed into one AAC track, 48 kHz stereo at 128 kb/s. On Air's own sound is left out.
- **Picking.** Picking a display, or "Whole Display", takes effect at the next Start. "Select Area…" dims the picked display and lets a person drag out a rectangle, at least 64 points on each side: Return or letting go of the mouse picks it, and Escape leaves the choice as it was. "Select Window…" opens the system's window picker, limited to one window. A display or an area is remembered between recordings and launches, in `settings.json` beside the spool, written whole under a hidden name and then renamed. A window lasts until it closes or On Air quits. Nothing can be picked while a recording runs.
- **When the pick isn't there.** If the picked display isn't connected at Start, On Air records the main display; if the area no longer fits its display, the whole display; if the picked window is gone, the whole display. The menu says so while recording and once the recording is saved, and a remembered choice never keeps a recording from starting. A window that closes while it is recorded ends the recording, as capture stopping by itself does: what was recorded is kept, and the menu says why.
- **A window's picture and sound.** The picture is the window alone, whatever covers it, at the size it had when the recording started. If it is resized, ScreenCaptureKit is asked to scale it to fit that size, keeping its shape. Apple's WWDC22 session "Take ScreenCaptureKit to the next level" says that a single-window filter captures all the sound of the application that owns the window, from its other windows too, and no other application's. The SDK's headers don't say. Neither has been tried here.
- **The microphone.** The first time capture can start, macOS asks for the microphone's permission. If it is refused, if there is no microphone, or if capture won't start with it, On Air starts capture again without it, so the picture and its sound are still recorded. The menu says that the microphone isn't being recorded and why, while recording and once the recording is saved. The next recording asks for it again.
- **Pieces.** Picture and sound go to disk in pieces 4 seconds long, in On Air's spool, `Library/Application Support/On Air/spool` in the home folder. A piece is the recording's opening segment followed by one fragment of fragmented MPEG-4, so each plays on its own, and it gets its name only once its bytes are on disk. When the screen is still, the last frame is written again each second, so pieces keep closing.
- **One timeline from zero.** A recording starts at time zero, in the joined file and in every piece, and its sound is on the same timeline as its picture. The file's own decode times put the first frame 26/600 s in, after the 2112 samples (44 ms) of priming the AAC encoder puts before the first sound, and AVFoundation reports both tracks from zero.
- **Stop.** Stop closes the last piece. The dot is grey again as soon as capture has stopped, and the pieces are joined into one file in the background, so Stop doesn't wait for the join however long the recording ran. If the join fails, the menu says where the pieces are, and its Show item opens their folder. Once a recording is saved, the menu also says if it ended early because a piece couldn't be written, and what capture says it left out: frames or sound buffers the writer dropped, and sound that came too late or too early to mix.
- **Where recordings go.** For now, to a folder of On Air's own, `Movies/On Air` in the home folder, named by the date and time they started. The pieces stay in the spool.
- **After a kill.** If On Air is killed mid-recording, the pieces written so far stay on disk. At the next launch it joins the whole ones into a file marked "(recovered)" and says how much it saved. It says so for each recording it finds, and for one it can't join, where its pieces are. The recording isn't carried on.
- **One at a time.** A second Start while a recording runs is refused, and says so. Quit while recording or starting finishes the recording first.

How it's built, in two layers, of which only the core decides:

| Part | What it does |
| :-- | :-- |
| `internal/recorder` | Decides: what a recording captures and what it falls back to, one recording at a time, when capture starts and stops, starting again without the microphone, what the menu says, joining at Stop off the menu's thread, and recovering what a kill left. It reaches the machine through an `Edge` it is given. |
| `internal/settings` | Keeps what was picked in a small JSON file, written whole under a hidden name and then renamed. |
| `internal/spool` | Keeps a recording's pieces: names them, tells a whole piece from a partial one, joins the whole ones from the first up to the first one missing, and marks a recording done. A file already at the join's name is never replaced. |
| `internal/mac` | The edge, cgo and Objective-C, built only on macOS. `app.m` draws the menu-bar item. `choose.m` lists the connected displays through `NSScreen`, which needs no permission, names each by its UUID, which outlasts reconnecting it, draws the overlay for an area, and keeps the window the system's picker hands back. `capture.m` takes the picture, the sound and the microphone from ScreenCaptureKit, on one serial queue: the display it is asked for, an area of it through `sourceRect`, or the picked window, and it reports what it can't find. `mixer.m` puts the two sounds on one 48 kHz stereo timeline and adds them. `writer.m` cuts picture and sound into segments with AVAssetWriter and hands each to Go as soon as it is whole. `selftest.m` drives the writer and the mixer with made-up frames and sound. It reports what happened and decides nothing. |
| `internal/mp4` | Reads, from a fragmented MPEG-4 file's own bytes, where each track's first fragment starts. |
| `internal/tone` | Measures how loud a tone is in decoded sound, for the self-test. |
| `cmd/onair` | Wires the recorder to the edge, and runs the self-test. |
| `tools/build-app` | Builds and signs `On Air.app`, the bundle macOS gives the screen-recording and microphone permissions to. |

The mixer waits for whichever source is behind, half a second at most, then carries on without it, and advances with the clock when neither sends anything, so the sound track has no hole. A source's buffers are laid end to end, and its stamps only place the first one and any that strays by more than 20 ms. What the writer isn't ready for waits in order, up to four seconds, because a sound buffer left out closes up in the track and everything after it plays early. What is left out all the same is counted, and the menu says so once the recording is saved.

### How it's checked

- **CI, on Linux.** Every pull request runs `gofmt`, `go vet`, `go build` and `go test` in a pinned Go image. The tests cover the spool, the recorder against a fake edge (one recording at a time, Stop, Quit while recording or starting, a piece that can't be written while recording and after Stop, capture that stops by itself, what capture says it left out, a failed join and where it says the pieces are, recovery after a kill with every recording it finds said, the microphone left out, and what is picked: the default, a display, an area, a window, what each falls back to and says, what is remembered, and that nothing changes while recording), the settings file, the MPEG-4 reader against hand-made boxes, and the tone measurement. The Mac part doesn't build there. On a Mac, `go test ./...` also checks the picture's size for a display, an area and a window.
- **On a Mac, with no permission.** `onair selftest` writes ten seconds of made-up frames and sound through the same mixer and writer capture uses, stamped by the host clock as capture's are and given in the order capture would get them. Its writer isn't in real time: it waits for each input to be ready, where capture's writer holds sound back or drops a frame, so that part is only checked by a person. The display's sound comes as 48 kHz stereo planes carrying 440 Hz, louder on the left. The microphone's comes as 44.1 kHz mono carrying 880 Hz, a few milliseconds off the display's, with a second where it sends nothing and a switch to 48 kHz later on. The self-test checks that the mixer left nothing out, that no piece came after the writer finished, that a piece plays on its own, and that the joined file plays for the ten seconds written with one picture and one sound track. It checks that the file starts at zero, as AVFoundation reads it and as its own bytes say, and it decodes the sound and measures it in 50 ms blocks: both tones in both channels at the loudness they were made, the microphone's silent for its quiet second, with both edges within 50 ms of where they were made, and nothing at 660 Hz, which nothing played. `onair selftest 1` sends one sound track straight to the writer, and `onair selftest 2` shows the writer refusing a second one.
- **Only a person can check** that macOS keeps delivering the picture and both sounds, that they stay in step over a long call, whether the writer ever holds sound back or leaves anything out, that voices come through the mix, that the microphone's permission prompt and the recording without it behave as described, and what a kill leaves. The same goes for the display list, the area overlay, the cropped picture, the window picker, and what is recorded of a window, its sound included. The milestone's hand checks are in [`docs/PRD.md`](docs/PRD.md#trying-milestone-one-by-hand), and none has been done with this build.

### What it doesn't do yet

- Hand recordings over to the folder On the Record watches. They stay in On Air's own folder, and the core below isn't called.
- Guard the disk (rule 8) or the length of a recording (decision 4).
- Carry a recording on after On Air or its recorder dies (decision 3). A killed recording is recovered as it stands at the next launch.
- Start capture again when it stops mid-recording, for the microphone or anything else. The recording ends there and what was recorded is kept.
- Record more than one display or window at once, or change what is recorded while a recording runs.
- Stop by itself, or anything in milestones two and three.

## The hand-over core

[`core/handover`](core/handover) decides each step of handing a finished recording to the folder another program watches: copy it there under a hidden name, compare the copy with the original, give it its real name without replacing anything, and remove the original once its time to keep it has passed. It touches no file itself. Its caller carries out each step and reports what happened, so a hand-over can be interrupted at any step and carried on from its record. It is its own Go module.

Invariant's factory built it from [#1](https://github.com/gitdek/onair/issues/1), and it merged through [#3](https://github.com/gitdek/onair/pull/3):

- 42 statements, ratified on the issue and pinned by hash in [`ratified.lock`](core/handover/.invariant/ratified.lock): 18 invariants, 13 witnesses, 5 known bugs, 4 fairness conditions, 1 liveness property and the spec.
- A TLA+ model, [`RecordingHandover.tla`](core/handover/.invariant/specs/RecordingHandover.tla), which TLC explores exhaustively within `MaxAttempts = 2`, `MaxCleanupAttempts = 2` and `MaxDifferences = 2`: 7,296 distinct states to depth 60, no violation and no deadlock, all 13 witnesses reached, the liveness property holding, and all 5 known bugs caught. One size larger, the code reaches the model's states again: 32,955 of them.
- Go code that Gobra proves against its contracts (10 of 10 functions), and that is tested against the model: 247,592 steps recorded, all 7,296 model states visited.
- A second review of its explorer, the code that walks the model's states, left two notes on the pull request, which aren't taken up yet: `MaxDifferences` is missing from the manifest's parameters, and a refused step's returned record isn't observed.

The gate verifies it again on every pull request. Nothing in the recorder calls it yet, so rules 5, 6, 7 and 11 have a proved core but aren't held by the app.

## The repository

- Its checks, in `.invariant/repository.json`: formatting, vet, build and tests, run in a pinned Go image.
- Its gate, `.github/workflows/gate.yml`, which runs those checks and verifies each modeled project on every pull request.
- The README's graphics, drawn by `docs/assets/generate.py` in a light and a dark theme.
