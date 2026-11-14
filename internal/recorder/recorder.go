// Package recorder decides what On Air does: when a recording starts and
// stops, where its pieces go, what is joined, and what a kill left behind.
// It touches no screen and no menu. An Edge does that and reports back, so
// everything here runs and is tested without a Mac.
package recorder

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gitdek/onair/internal/spool"
)

// Edge is the part that touches the machine.
type Edge interface {
	// Start asks for capture. Started, Failed or MicrophoneFailed follows.
	Start(Config)
	// Stop ends capture. Finished follows the last segment.
	Stop()
	// Show sets what the menu-bar item says.
	Show(recording bool, last, note string)
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
}

// Action is something chosen from the menu.
type Action int

const (
	Start Action = iota + 1
	Stop
	ShowLast
	OpenFolder
	Quit
)

// Event is something the edge reports about capture.
type Event int

const (
	Started Event = iota + 1
	// Finished says the last segment was handed over. Its message says
	// what capture left out, if anything.
	Finished
	// Failed says capture stopped or never started. Its message says why.
	Failed
	// MicrophoneFailed says capture didn't start because of the
	// microphone, and isn't running.
	MicrophoneFailed
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
	edge   Edge
	config Config
	spool  string
	out    string
	now    func() time.Time

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
	// broken is set once one of this recording's pieces couldn't be
	// written, and cut says so when that happened while capture was
	// starting or stopping, when the menu hadn't said it yet.
	broken bool
	cut    string
}

// New returns an App that keeps pieces under spoolDir and joined recordings
// in outDir.
func New(edge Edge, config Config, spoolDir, outDir string, now func() time.Time) *App {
	return &App{edge: edge, config: config, spool: spoolDir, out: outDir, now: now}
}

// Launched joins what a kill left behind: every recording in the spool with
// whole pieces that was never joined. The pieces stay where they are.
func (a *App) Launched() {
	left, err := spool.Unfinished(a.spool)
	a.mu.Lock()
	defer a.mu.Unlock()
	// Each recording found is said, so one that was recovered doesn't hide
	// another that couldn't be.
	var notes []string
	if err != nil {
		notes = append(notes, "On Air couldn't read its own pieces: "+err.Error())
	}
	recovered, kept := "", ""
	for _, r := range left {
		name := filepath.Base(r.Dir)
		joined, err := a.join(r, name+" On Air (recovered).mp4")
		if err != nil {
			kept = r.Dir
			notes = append(notes, fmt.Sprintf("A recording that was cut short is safe in pieces in %s, but joining it failed: %v", r.Dir, err))
			continue
		}
		recovered = joined.Path
		notes = append(notes, fmt.Sprintf("Recovered a recording that was cut short: %s.", count(joined.Pieces, a.config.PieceSeconds)))
	}
	if recovered != "" {
		a.last = recovered
	} else if kept != "" {
		a.last = kept
	}
	if len(notes) > 0 {
		a.note = strings.Join(notes, " ")
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
		a.rec, a.name, a.pieces, a.state, a.note, a.mic = r, name, 0, starting, "Starting…", ""
		a.broken, a.cut = false, ""
		a.show()
		a.edge.Start(a.config)
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
// be. The menu says so at once while recording, and once the recording is
// saved when capture was starting or stopping already.
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
	if err == nil || a.broken {
		return
	}
	a.broken = true
	if a.state != recording {
		a.cut = "The recording ends early, because a piece couldn't be written: " + err.Error()
		return
	}
	a.state, a.note = stopping, "Recording stopped, because a piece couldn't be written: "+err.Error()
	a.show()
	a.edge.Stop()
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
		a.state, a.note = recording, micNote(a.mic)
		// Quit came while capture was starting. What it has recorded is
		// kept, as with a quit while recording.
		if a.quitting {
			a.state, a.note = stopping, "Finishing…"
			a.show()
			a.edge.Stop()
			return
		}
		a.show()
	case Finished, Failed, MicrophoneFailed:
		if event == MicrophoneFailed && a.withoutMicrophone(message) {
			return
		}
		if a.state == idle || a.state == joining || a.rec == nil {
			return
		}
		// Capture's last word says why it stopped, or, when it finished as
		// asked, what it left out, if anything.
		why, left := message, ""
		if event == Finished {
			why, left = "", message
		}
		a.finish(why, left)
	}
}

// withoutMicrophone starts capture again without the microphone, since the
// display is worth recording without it, and says why. It does so once a
// recording: capture that still doesn't start has failed, as any other
// start can. The lock is held.
func (a *App) withoutMicrophone(why string) bool {
	if a.state != starting || !a.config.Microphone || a.mic != "" || a.quitting {
		return false
	}
	if why == "" {
		why = "no reason was given."
	}
	a.mic, a.note = why, "Starting without the microphone…"
	a.show()
	c := a.config
	c.Microphone = false
	a.edge.Start(c)
	return true
}

// micNote is what the menu says while a recording runs without the
// microphone it asked for.
func micNote(why string) string {
	if why == "" {
		return ""
	}
	return "The microphone isn't being recorded: " + why
}

// finish joins the recording's pieces off the caller's thread, since a long
// recording takes a while to copy and Stop mustn't wait for it (rule 10).
// why is why capture stopped by itself, and left what it says it left out.
func (a *App) finish(why, left string) {
	r, name, kept, mic, cut := a.rec, a.name, a.note, a.mic, a.cut
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
			// The pieces are the recording: say where they are, and let
			// Show open their folder.
			a.last = r.Dir
			a.note = fmt.Sprintf("The recording is safe in pieces in %s, but joining it failed: %v", r.Dir, err)
		case why != "":
			a.last = joined.Path
			a.note = fmt.Sprintf("Recording stopped by itself: %s What was recorded is saved: %s.", why, count(joined.Pieces, a.config.PieceSeconds))
		default:
			a.last = joined.Path
			a.note = ""
			if kept != "" && kept != "Finishing…" && kept != micNote(mic) {
				a.note = kept
			}
		}
		// A recording ended early by a piece that couldn't be written says
		// so, and one that was kept says what capture left out of it and
		// if it has no microphone.
		if cut != "" {
			a.note = strings.TrimSpace(a.note + " " + cut)
		}
		if !errors.Is(err, errNothing) {
			if left != "" {
				a.note = strings.TrimSpace(a.note + " Not all of it was written: " + left)
			}
			if mic != "" {
				a.note = strings.TrimSpace(a.note + " The microphone wasn't recorded: " + mic)
			}
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
}

// count says how much a number of pieces holds.
func count(pieces int, seconds float64) string {
	total := time.Duration(float64(pieces) * seconds * float64(time.Second)).Round(time.Second)
	if pieces == 1 {
		return fmt.Sprintf("1 piece, about %s", total)
	}
	return fmt.Sprintf("%d pieces, about %s", pieces, total)
}
