// Package recorder decides what On Air does: what a recording captures,
// when it starts and stops, where its pieces go, what is joined, where the
// joined file is handed over, and what a kill left behind. It touches no
// screen and no menu. An Edge does that and reports back, so everything
// here runs and is tested without a Mac.
package recorder

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gitdek/onair/internal/delivery"
	"github.com/gitdek/onair/internal/settings"
	"github.com/gitdek/onair/internal/spool"
)

// Edge is the part that touches the machine.
type Edge interface {
	// AskMicrophone asks whether On Air may use the microphone, which
	// macOS asks the person the first time. MicrophoneAllowed or
	// MicrophoneRefused follows, once there is an answer.
	AskMicrophone()
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
	// ChooseFolder lets a person pick the folder recordings go to, with
	// the system's folder picker, opened on current. FolderChosen follows,
	// unless they cancel.
	ChooseFolder(current string)
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
	QuietSeconds float64 // how long capture waits for the microphone's sound before saying none came
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
	ChooseFolder
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
	// TargetGone says capture didn't start because what it was asked to
	// record isn't there, and isn't running.
	TargetGone
	// WindowClosed says the picked window closed.
	WindowClosed
	// PickerFailed says the window picker didn't open.
	PickerFailed
	// MicrophoneAllowed says On Air may use the microphone.
	MicrophoneAllowed
	// MicrophoneRefused says it may not.
	MicrophoneRefused
	// MicrophoneQuiet says that capture, recording with the microphone, has
	// had none of its sound for QuietSeconds. Its message says what came
	// instead.
	MicrophoneQuiet
	// MicrophoneHeard says the microphone's sound came after
	// MicrophoneQuiet.
	MicrophoneHeard
)

type state int

const (
	idle state = iota
	// asking waits for the answer about the microphone, before capture
	// starts.
	asking
	starting
	recording
	stopping
	joining
)

// App is On Air's state: at most one recording at a time (docs/PRD.md,
// rule 1), whose pieces are in the spool until they are joined, and whose
// joined file is handed to the folder picked for recordings.
type App struct {
	edge     Edge
	config   Config
	spool    string
	out      string // On Air's own folder, where recordings go until one is picked
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
	// unheard is what capture said came from the microphone instead of its
	// sound, until its sound comes.
	unheard string

	displays []Display // connected, as the edge last reported
	picked   picked    // the display and area to record, remembered
	window   string    // the picked window's name, until it closes or On Air quits
	asked    Config    // what this recording's last start asked for
	fallback fallback  // why this recording isn't of what was picked
	stopped  string    // why On Air stopped this recording itself
	// broken is set once one of this recording's pieces couldn't be
	// written, and cut says so when that happened while capture was
	// starting or stopping, when the menu hadn't said it yet.
	broken bool
	cut    string

	folder string // the folder picked for recordings, remembered; empty until one is
	// handed is what the menu says of hand-overs: the one under way, or how
	// the last ones went. It follows the note, and a Start clears it.
	handed string
	// handing counts the runs of hand-overs that haven't finished, and
	// handMu has them run one at a time.
	handing int
	handMu  sync.Mutex
}

// kept is what settings.json holds: what to record, and where recordings
// go.
type kept struct {
	picked
	Folder string `json:"folder,omitempty"`
}

// New returns an App that keeps pieces under spoolDir, hands joined
// recordings to outDir until a folder is picked, and keeps what to record
// and where recordings go in settingsFile.
func New(edge Edge, config Config, spoolDir, outDir, settingsFile string, now func() time.Time) *App {
	a := &App{edge: edge, config: config, spool: spoolDir, out: outDir, settings: settingsFile, now: now}
	if settingsFile != "" {
		var k kept
		if err := settings.Load(settingsFile, &k); err != nil {
			a.note = "On Air couldn't read what to record or where recordings go, so it records the main display and hands recordings to its own folder: " + err.Error()
		} else {
			a.picked, a.folder = k.picked, k.Folder
		}
	}
	return a
}

// Launched joins what a kill left behind: every recording in the spool with
// whole pieces that was never joined. The pieces stay where they are. Then,
// in the background, it carries on every hand-over that isn't done: those
// of the recordings it joined, those a kill or a quit cut short, those that
// failed, and those whose three days are up.
func (a *App) Launched() {
	a.recover()
	a.handOverPending(true)
}

// recover joins what a kill left behind, and says so.
func (a *App) recover() {
	left, err := spool.Unfinished(a.spool)
	a.mu.Lock()
	defer a.mu.Unlock()
	// Each recording found is said, so one that was recovered doesn't hide
	// another that couldn't be.
	var notes []string
	if err != nil {
		notes = append(notes, "On Air couldn't read its own pieces: "+err.Error())
	}
	recovered, safe := "", ""
	for _, r := range left {
		name := filepath.Base(r.Dir)
		_, joined, err := a.join(r, name+" On Air (recovered).mp4")
		if err != nil {
			safe = r.Dir
			notes = append(notes, fmt.Sprintf("A recording that was cut short is safe in pieces in %s, but joining it failed: %v", r.Dir, err))
			continue
		}
		recovered = joined.Path
		notes = append(notes, fmt.Sprintf("Recovered a recording that was cut short: %s.", count(joined.Pieces, a.config.PieceSeconds)))
	}
	if recovered != "" {
		a.last = recovered
	} else if safe != "" {
		a.last = safe
	}
	// After what New had to say about the settings, if anything.
	a.note = join(append([]string{a.note}, notes...)...)
	a.show()
}

// Menu acts on a choice from the menu.
func (a *App) Menu(action Action) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch action {
	case Start:
		if a.state == asking {
			a.note = askingNote
			a.show()
			return
		}
		if a.state != idle {
			a.note = "On Air is already recording. Stop it before starting another."
			a.show()
			return
		}
		if a.quitting {
			a.note = "On Air quits once the last recording is handed over."
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
		a.rec, a.name, a.pieces, a.state, a.note, a.handed = r, name, 0, starting, "Starting…", ""
		a.mic, a.unheard, a.fallback, a.stopped = "", "", f, ""
		a.broken, a.cut = false, ""
		a.asked = a.config
		a.asked.Target = t
		// The microphone is asked for before capture starts, so that a
		// question from macOS is answered before anything is recorded, and
		// the recording starts once it is.
		if a.asked.Microphone {
			a.state, a.note = asking, askingNote
			a.show()
			a.edge.AskMicrophone()
			return
		}
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
		folder := a.destination()
		if folder == a.out {
			if err := os.MkdirAll(a.out, 0o755); err != nil {
				return
			}
		} else if info, err := os.Stat(folder); err != nil || !info.IsDir() {
			a.note = fmt.Sprintf("The folder recordings go to isn't there: %s", folder)
			a.show()
			return
		}
		a.edge.Reveal(folder)
	case ChooseFolder:
		a.edge.ChooseFolder(a.destination())
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
		if a.state == asking {
			// Nothing was recorded, and capture never started: the
			// recording ends at once, and the answer finds nothing to start.
			a.finish("", "")
			return
		}
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
	case MicrophoneAllowed, MicrophoneRefused:
		if a.state != asking {
			return
		}
		a.state, a.note = starting, "Starting…"
		if event == MicrophoneRefused {
			// The display is worth recording without the microphone.
			a.mic = "On Air isn't allowed to use it. Allow On Air in System Settings, Privacy & Security, Microphone, then start a new recording."
			a.asked.Microphone = false
			a.note = "Starting without the microphone…"
		}
		a.show()
		a.edge.Start(a.asked)
	case MicrophoneQuiet:
		if !a.asked.Microphone || (a.state != recording && a.state != stopping) {
			return
		}
		if message == "" {
			message = "none of its sound came."
		}
		a.unheard = message
		if a.state == recording {
			a.note = a.recordingNote()
			a.show()
		}
	case MicrophoneHeard:
		if a.unheard == "" {
			return
		}
		a.unheard = ""
		if a.state == recording {
			a.note = a.recordingNote()
			a.show()
		}
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
		// Capture's last word says why it stopped, or, when it finished as
		// asked, what it left out, if anything.
		why, left := message, ""
		if event == Finished {
			why, left = a.stopped, message
		}
		a.finish(why, left)
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

// askingNote is what the menu says while the microphone is being asked for.
const askingNote = "Recording starts once macOS's question about the microphone is answered."

// recordingNote is what the menu says while this recording runs: whether it
// is of something else than was picked, whether it lacks the microphone,
// and whether the microphone's sound hasn't come. The lock is held.
func (a *App) recordingNote() string {
	mic := ""
	switch {
	case a.mic != "":
		mic = "The microphone isn't being recorded: " + a.mic
	case a.unheard != "":
		mic = "The microphone isn't being heard: " + a.unheard + " If macOS asked about the microphone, allow it, then stop and start again. Otherwise check that it's plugged in and not muted."
	}
	return join(a.fallback.now(), mic)
}

// finish joins the recording's pieces off the caller's thread, since a long
// recording takes a while to copy and Stop mustn't wait for it (rule 10).
// why is why capture stopped, or was stopped, by itself, and left what
// capture says it left out.
func (a *App) finish(why, left string) {
	r, name, kept, during, mic, f, cut := a.rec, a.name, a.note, a.recordingNote(), a.mic, a.fallback, a.cut
	unheard := a.unheard
	a.rec, a.state = nil, joining
	a.show()
	go func() {
		d, joined, err := a.join(r, name+" On Air.mp4")
		a.mu.Lock()
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
			if kept != "" && kept != "Finishing…" && kept != during {
				a.note = kept
			}
		}
		// A recording ended early by a piece that couldn't be written says
		// so, and one that was kept says if it isn't of what was picked,
		// what capture left out of it, if it has no microphone, and if the
		// microphone's sound never came.
		if errors.Is(err, errNothing) {
			a.note = join(a.note, cut)
		} else {
			if left != "" {
				left = "Not all of it was written: " + left
			}
			if mic != "" {
				mic = "The microphone wasn't recorded: " + mic
			}
			if unheard != "" {
				unheard = "The microphone wasn't heard: " + unheard
			}
			a.note = join(a.note, cut, f.then(), left, mic, unheard)
		}
		if d != nil {
			// Handed over next, through the same steps as any other.
			a.handing++
		}
		a.show()
		a.mu.Unlock()
		if d != nil {
			a.handOver([]string{r.Dir})
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.quitting {
			a.edge.Quit()
		}
	}()
}

var errNothing = errors.New("nothing was recorded")

// join joins a recording's whole pieces into one file in its own directory
// in the spool, under the name the file will have in the folder, and marks
// the recording done. The hand-over that takes the file there is kept
// first, so a kill anywhere on the way leaves one joined file at most. A
// failed join leaves the pieces as they are (rule 9), and no hand-over.
func (a *App) join(r *spool.Recording, file string) (*delivery.Delivery, spool.Joined, error) {
	if pieces, _, err := r.Pieces(); err == nil && len(pieces) == 0 {
		return nil, spool.Joined{}, errNothing
	}
	d, err := delivery.Prepare(r.Dir, file)
	if err != nil {
		return nil, spool.Joined{}, err
	}
	joined, err := joinOnce(r, d.Spool())
	if err != nil {
		d.Drop()
		return nil, spool.Joined{}, err
	}
	if err := r.MarkDone(joined.Path); err != nil {
		return nil, spool.Joined{}, err
	}
	return d, joined, nil
}

// joinOnce joins the pieces into dst, unless a join a kill cut short gave
// dst its name already, which a join does only once the file is whole.
func joinOnce(r *spool.Recording, dst string) (spool.Joined, error) {
	info, err := os.Lstat(dst)
	if errors.Is(err, fs.ErrNotExist) {
		return r.Join(dst)
	}
	if err != nil {
		return spool.Joined{}, err
	}
	pieces, after, err := r.Pieces()
	if err != nil {
		return spool.Joined{}, err
	}
	return spool.Joined{Path: dst, Pieces: len(pieces), Bytes: info.Size(), After: after}, nil
}

// destination is the folder recordings go to: the one picked, or On Air's
// own until one is. The lock is held.
func (a *App) destination() string {
	if a.folder != "" {
		return a.folder
	}
	return a.out
}

// FolderChosen keeps the folder a person picked for recordings to go to,
// and tries again each hand-over that failed, now to that folder. A folder
// in On Air's spool is refused: a recording handed to it would be handed
// to itself.
func (a *App) FolderChosen(path string) {
	a.mu.Lock()
	if path == "" {
		a.mu.Unlock()
		return
	}
	if a.inSpool(path) {
		a.note = fmt.Sprintf("“%s” is in On Air's spool, where it keeps its own copies, so recordings can't go there. Choose the folder On the Record watches.", filepath.Base(path))
		a.show()
		a.mu.Unlock()
		return
	}
	a.folder = path
	if a.state == idle {
		a.note = ""
	}
	if a.settings != "" {
		if err := settings.Save(a.settings, kept{picked: a.picked, Folder: path}); err != nil {
			a.note = "On Air hands recordings to this folder until it quits, but couldn't remember it: " + err.Error()
		}
	}
	a.show()
	a.mu.Unlock()
	a.handOverPending(true)
}

// inSpool says whether folder is On Air's spool or a folder in it, once
// links are followed.
func (a *App) inSpool(folder string) bool {
	resolve := func(path string) string {
		if real, err := filepath.EvalSymlinks(path); err == nil {
			return real
		}
		return filepath.Clean(path)
	}
	rel, err := filepath.Rel(resolve(a.spool), resolve(folder))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Tidy removes On Air's own copy of each recording the folder has held for
// three days, and tries again a removal that failed. A clock runs it.
func (a *App) Tidy() { a.handOverPending(false) }

// handOverPending carries on, in the background, each hand-over in the
// spool that isn't done. again carries on all of them, and tries again
// those that failed. Otherwise only those already handed over go on, to
// remove On Air's own copy once its days are up.
func (a *App) handOverPending(again bool) {
	a.mu.Lock()
	a.handing++
	a.mu.Unlock()
	go func() {
		dirs, err := a.pending(again)
		if err != nil {
			a.mu.Lock()
			a.handed = join(a.handed, "On Air couldn't read every hand-over in its spool: "+err.Error())
			a.show()
			a.mu.Unlock()
		}
		a.handOver(dirs)
	}()
}

// pending lists the recordings in the spool, oldest first, whose hand-over
// isn't done: all of them when again is set, and otherwise those handed
// over already. A recording joined before hand-overs existed has none, and
// is left as it is: its file is in On Air's own folder.
func (a *App) pending(again bool) ([]string, error) {
	entries, err := os.ReadDir(a.spool)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var dirs []string
	var problems []error
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(a.spool, e.Name())
		if _, done := spool.Open(dir).Done(); !done {
			continue
		}
		d, ok, err := delivery.Load(dir)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if ok && !d.Done() && (again || d.Delivered()) {
			dirs = append(dirs, dir)
		}
	}
	return dirs, errors.Join(problems...)
}

// handOver carries on the hand-overs of the recordings in dirs, one at a
// time and off the menu's thread, since copying a long recording and
// comparing the copy take a while. Each starts from the record it kept,
// which is the only word on where it stands. The menu says how each went,
// and Show opens a recording where it is now. The caller counted this run
// in a.handing.
func (a *App) handOver(dirs []string) {
	a.handMu.Lock()
	defer a.handMu.Unlock()
	// What the menu says of hand-overs is replaced only when there is
	// something to say: a run that only waits leaves it as it was.
	var said []string
	spoke := false
	for _, dir := range dirs {
		d, ok, err := delivery.Load(dir)
		a.mu.Lock()
		folder := a.destination()
		if err != nil || !ok {
			if err == nil {
				err = errors.New("its record is gone")
			}
			said, spoke = append(said, fmt.Sprintf("On Air couldn't hand over the recording in %s: %v", dir, err)), true
			a.handed = join(said...)
			a.show()
			a.mu.Unlock()
			continue
		}
		if !d.Delivered() {
			a.handed, spoke = join(append(said, fmt.Sprintf("Handing “%s” to “%s”…", d.File(), filepath.Base(folder)))...), true
			a.show()
		}
		own := folder == a.out
		a.mu.Unlock()
		if own {
			// On Air's own folder is made when it is needed. A picked one
			// never is: one that is gone is for the hand-over to say.
			os.MkdirAll(folder, 0o755)
		}
		r := d.Run(folder, a.clock)
		a.mu.Lock()
		w := word(r, folder)
		// Show follows the last recording to where it is now. After a
		// launch, before anything else was shown, it follows the newest
		// recording the menu speaks of.
		if a.last == d.Spool() || a.last == "" && w != "" {
			a.last = r.Path
		}
		if w != "" {
			said, spoke = append(said, w), true
		}
		if spoke {
			a.handed = join(said...)
		}
		a.show()
		a.mu.Unlock()
	}
	a.mu.Lock()
	a.handing--
	a.show()
	a.mu.Unlock()
}

// clock is the time now, for a hand-over, which runs without the lock.
func (a *App) clock() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.now()
}

// word is what the menu says of how a run of a hand-over went, if anything.
func word(r delivery.Report, folder string) string {
	if r.Folder != "" {
		folder = r.Folder
	}
	to, safe := filepath.Base(folder), filepath.Dir(r.Path)
	switch {
	case r.Handed:
		return fmt.Sprintf("Handed “%s” to “%s”.", r.File, to)
	case r.Failed != "":
		return fmt.Sprintf("“%s” couldn't be handed to “%s”: %s. It is safe in %s, and On Air tries again at the next launch and when a folder is picked.", r.File, to, r.Failed, safe)
	case r.Err != nil && !r.Delivered:
		return fmt.Sprintf("Handing “%s” to “%s” stopped: %v. It is safe in %s, and On Air carries on at the next launch and when a folder is picked.", r.File, to, r.Err, safe)
	case r.RemoveErr != nil:
		return fmt.Sprintf("On Air couldn't remove its own copy of “%s”, which “%s” has held for three days: %v. It tries again later.", r.File, to, r.RemoveErr)
	case r.Err != nil:
		return fmt.Sprintf("Removing On Air's own copy of “%s” stopped: %v. It tries again later.", r.File, r.Err)
	}
	return ""
}

// show tells the edge what to say. The lock is held.
func (a *App) show() {
	a.edge.Show(a.state == recording || a.state == stopping, a.last, join(a.note, a.handed))
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
