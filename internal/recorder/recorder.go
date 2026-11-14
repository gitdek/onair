// Package recorder decides what On Air does: what a recording captures,
// when it starts and stops, where its pieces go, what is joined, and what a
// kill left behind. It touches no screen and no menu. An Edge does that and
// reports back, so everything here runs and is tested without a Mac.
package recorder

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gitdek/onair/internal/settings"
	"github.com/gitdek/onair/internal/spool"
)

// Edge is the part that touches the machine.
type Edge interface {
	// Start asks for capture. Started, Failed, MicrophoneFailed or
	// TargetGone follows.
	Start(Config)
	// Stop ends capture. Finished follows the last segment.
	Stop()
	// Show sets what the menu-bar item says.
	Show(recording bool, last, note string)
	// Choices sets the part of the menu that picks what to record.
	Choices(Choices)
	// SelectArea lets a person drag out an area of a display. AreaPicked
	// follows, unless they cancel.
	SelectArea(display string)
	// SelectWindow lets a person pick a window, which the edge keeps.
	// WindowPicked follows, unless they cancel.
	SelectWindow()
	Reveal(path string)
	Quit()
}

// Config is what a recording is asked to be.
type Config struct {
	Rows         int     // the picture's height at most
	FPS          int     // frames a second at most
	PieceSeconds float64 // how long a piece is
	VideoBitrate int     // bits a second
	Microphone   bool    // mix the microphone into the display's sound
	Target       Target  // what to capture
}

// Action is something chosen from the menu.
type Action int

const (
	Start Action = iota + 1
	Stop
	ShowLast
	OpenFolder
	Quit
	SelectArea
	WholeDisplay
	SelectWindow
)

// Event is something the edge reports about capture.
type Event int

const (
	Started Event = iota + 1
	Finished
	Failed
	// MicrophoneFailed says capture didn't start because of the
	// microphone, and isn't running.
	MicrophoneFailed
	// TargetGone says capture didn't start because what it was asked to
	// record isn't there, and isn't running.
	TargetGone
	// WindowClosed says the picked window closed.
	WindowClosed
	// PickerFailed says the window picker didn't open.
	PickerFailed
)

type state int

const (
	idle state = iota
	starting
	recording
	stopping
	joining
)

// App is On Air's state: at most one recording at a time (docs/PRD.md,
// rule 1), whose pieces are in the spool until they are joined.
type App struct {
	edge     Edge
	config   Config
	spool    string
	out      string
	settings string
	now      func() time.Time

	mu       sync.Mutex
	state    state
	rec      *spool.Recording
	name     string
	pieces   int
	last     string
	note     string
	quitting bool
	// mic says why this recording has no microphone, when one was asked
	// for and couldn't be had.
	mic string

	displays []Display // connected, as the edge last reported
	picked   picked    // the display and area to record, remembered
	window   string    // the picked window's name, until it closes or On Air quits
	asked    Config    // what this recording's last start asked for
	fallback fallback  // why this recording isn't of what was picked
	stopped  string    // why On Air stopped this recording itself
}

// New returns an App that keeps pieces under spoolDir, joined recordings in
// outDir, and what to record in settingsFile.
func New(edge Edge, config Config, spoolDir, outDir, settingsFile string, now func() time.Time) *App {
	a := &App{edge: edge, config: config, spool: spoolDir, out: outDir, settings: settingsFile, now: now}
	if settingsFile != "" {
		if err := settings.Load(settingsFile, &a.picked); err != nil {
			a.picked = picked{}
			a.note = "On Air couldn't read what to record, so it records the main display: " + err.Error()
		}
	}
	return a
}

// Launched joins what a kill left behind: every recording in the spool with
// whole pieces that was never joined. The pieces stay where they are.
func (a *App) Launched() {
	left, err := spool.Unfinished(a.spool)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.note = "On Air couldn't read its own pieces: " + err.Error()
	}
	for _, r := range left {
		name := filepath.Base(r.Dir)
		joined, err := a.join(r, name+" On Air (recovered).mp4")
		if err != nil {
			a.note = fmt.Sprintf("A recording that was cut short is safe in pieces, but joining it failed: %v", err)
			continue
		}
		a.last = joined.Path
		a.note = fmt.Sprintf("Recovered a recording that was cut short: %s.", count(joined.Pieces, a.config.PieceSeconds))
	}
	a.show()
}

// Menu acts on a choice from the menu.
func (a *App) Menu(action Action) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch action {
	case Start:
		if a.state != idle {
			a.note = "On Air is already recording. Stop it before starting another."
			a.show()
			return
		}
		name := a.now().Format("2006-01-02 15.04.05")
		r, err := spool.Create(a.spool, name)
		if err != nil {
			a.note = "On Air couldn't start: " + err.Error()
			a.show()
			return
		}
		t, f := a.target()
		a.rec, a.name, a.pieces, a.state, a.note = r, name, 0, starting, "Starting…"
		a.mic, a.fallback, a.stopped = "", f, ""
		a.asked = a.config
		a.asked.Target = t
		a.show()
		a.edge.Start(a.asked)
	case Stop:
		if a.state != recording {
			return
		}
		a.state, a.note = stopping, "Finishing…"
		a.show()
		a.edge.Stop()
	case ShowLast:
		if a.last != "" {
			a.edge.Reveal(a.last)
		}
	case OpenFolder:
		if err := os.MkdirAll(a.out, 0o755); err == nil {
			a.edge.Reveal(a.out)
		}
	case SelectArea:
		if a.canChoose() {
			a.edge.SelectArea(a.areaDisplay())
		}
	case WholeDisplay:
		if a.canChoose() {
			a.remember(picked{Display: a.picked.Display})
		}
	case SelectWindow:
		if a.canChoose() {
			a.edge.SelectWindow()
		}
	case Quit:
		if a.state == idle {
			a.edge.Quit()
			return
		}
		// Finish the recording first. If the app is killed instead, the
		// pieces are on disk and the next launch joins them.
		a.quitting = true
		if a.state == recording {
			a.state, a.note = stopping, "Finishing…"
			a.show()
			a.edge.Stop()
		}
	}
}

// Segment keeps a segment the writer handed over. A segment that can't be
// kept ends the recording: what is on disk is whole, and nothing more would
// be.
func (a *App) Segment(opening bool, data []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rec == nil {
		return
	}
	var err error
	if opening {
		err = a.rec.PutOpening(data)
	} else {
		a.pieces++
		err = a.rec.PutPiece(a.pieces, data)
	}
	if err != nil && a.state == recording {
		a.state, a.note = stopping, "Recording stopped, because a piece couldn't be written: "+err.Error()
		a.show()
		a.edge.Stop()
	}
}

// Event acts on what the edge reports about capture.
func (a *App) Event(event Event, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch event {
	case Started:
		if a.state != starting {
			return
		}
		a.state, a.note = recording, a.recordingNote()
		// Quit came while capture was starting. What it has recorded is
		// kept, as with a quit while recording.
		if a.quitting {
			a.state, a.note = stopping, "Finishing…"
			a.show()
			a.edge.Stop()
			return
		}
		a.show()
	case WindowClosed:
		a.window = ""
		// What was recorded of it is kept, as when capture stops by itself.
		if a.state == recording && a.asked.Target.Window {
			a.stopped = "the window being recorded closed."
			a.state, a.note = stopping, "Finishing…"
			a.show()
			a.edge.Stop()
			return
		}
		a.show()
	case PickerFailed:
		a.note = "The window picker didn't open: " + message
		a.show()
	case Finished, Failed, MicrophoneFailed, TargetGone:
		if event == MicrophoneFailed && a.withoutMicrophone(message) {
			return
		}
		if event == TargetGone && a.withoutTarget() {
			return
		}
		if a.state == idle || a.state == joining || a.rec == nil {
			return
		}
		why := a.stopped
		if event != Finished {
			why = message
		}
		a.finish(why)
	}
}

// withoutMicrophone starts capture again without the microphone, since the
// display is worth recording without it, and says why. It does so once a
// recording: capture that still doesn't start has failed, as any other
// start can. The lock is held.
func (a *App) withoutMicrophone(why string) bool {
	if a.state != starting || !a.asked.Microphone || a.mic != "" || a.quitting {
		return false
	}
	if why == "" {
		why = "no reason was given."
	}
	a.mic, a.note = why, "Starting without the microphone…"
	a.asked.Microphone = false
	a.show()
	a.edge.Start(a.asked)
	return true
}

// recordingNote is what the menu says while this recording runs: whether it
// is of something else than was picked, and whether it lacks the
// microphone. The lock is held.
func (a *App) recordingNote() string {
	mic := ""
	if a.mic != "" {
		mic = "The microphone isn't being recorded: " + a.mic
	}
	return join(a.fallback.now(), mic)
}

// finish joins the recording's pieces off the caller's thread, since a long
// recording takes a while to copy and Stop mustn't wait for it (rule 10).
func (a *App) finish(why string) {
	r, name, kept, during, mic, f := a.rec, a.name, a.note, a.recordingNote(), a.mic, a.fallback
	a.rec, a.state = nil, joining
	a.show()
	go func() {
		joined, err := a.join(r, name+" On Air.mp4")
		a.mu.Lock()
		defer a.mu.Unlock()
		a.state = idle
		switch {
		case err != nil && errors.Is(err, errNothing):
			// Nothing was recorded, so there is nothing to keep.
			os.Remove(r.Dir)
			a.note = "Nothing was recorded."
			if why != "" {
				a.note = why
			}
		case err != nil:
			a.note = fmt.Sprintf("The recording is safe in pieces, but joining it failed: %v", err)
		case why != "":
			a.last = joined.Path
			a.note = fmt.Sprintf("Recording stopped by itself: %s What was recorded is saved: %s.", why, count(joined.Pieces, a.config.PieceSeconds))
		default:
			a.last = joined.Path
			a.note = ""
			if kept != "" && kept != "Finishing…" && kept != during {
				a.note = kept
			}
		}
		// A recording that was kept says if it isn't of what was picked,
		// and if it has no microphone.
		if !errors.Is(err, errNothing) {
			if mic != "" {
				mic = "The microphone wasn't recorded: " + mic
			}
			a.note = join(a.note, f.then(), mic)
		}
		a.show()
		if a.quitting {
			a.edge.Quit()
		}
	}()
}

var errNothing = errors.New("nothing was recorded")

// join writes a recording's whole pieces into the folder for recordings and
// marks it done. A failed join leaves the pieces as they are (rule 9).
func (a *App) join(r *spool.Recording, file string) (spool.Joined, error) {
	if pieces, _, err := r.Pieces(); err == nil && len(pieces) == 0 {
		return spool.Joined{}, errNothing
	}
	joined, err := r.Join(filepath.Join(a.out, file))
	if err != nil {
		return spool.Joined{}, err
	}
	return joined, r.MarkDone(joined.Path)
}

// show tells the edge what to say. The lock is held.
func (a *App) show() {
	a.edge.Show(a.state == recording || a.state == stopping, a.last, a.note)
	a.edge.Choices(a.choices())
}

// count says how much a number of pieces holds.
func count(pieces int, seconds float64) string {
	total := time.Duration(float64(pieces) * seconds * float64(time.Second)).Round(time.Second)
	if pieces == 1 {
		return fmt.Sprintf("1 piece, about %s", total)
	}
	return fmt.Sprintf("%d pieces, about %s", pieces, total)
}
