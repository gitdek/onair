package recorder

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeEdge records what the app asked of the machine.
type fakeEdge struct {
	mu       sync.Mutex
	starts   int
	asked    []Config
	stops    int
	quits    int
	revealed []string
	shows    []shown
	choices  Choices
	areas    []string // displays an area was asked for on
	windows  int      // times a window was asked for
}

type shown struct {
	recording  bool
	last, note string
}

func (e *fakeEdge) Start(c Config) {
	e.mu.Lock()
	e.starts++
	e.asked = append(e.asked, c)
	e.mu.Unlock()
}
func (e *fakeEdge) Stop() { e.mu.Lock(); e.stops++; e.mu.Unlock() }
func (e *fakeEdge) Quit() { e.mu.Lock(); e.quits++; e.mu.Unlock() }
func (e *fakeEdge) Reveal(p string) {
	e.mu.Lock()
	e.revealed = append(e.revealed, p)
	e.mu.Unlock()
}
func (e *fakeEdge) Show(recording bool, last, note string) {
	e.mu.Lock()
	e.shows = append(e.shows, shown{recording, last, note})
	e.mu.Unlock()
}
func (e *fakeEdge) Choices(c Choices) { e.mu.Lock(); e.choices = c; e.mu.Unlock() }
func (e *fakeEdge) SelectArea(display string) {
	e.mu.Lock()
	e.areas = append(e.areas, display)
	e.mu.Unlock()
}
func (e *fakeEdge) SelectWindow() { e.mu.Lock(); e.windows++; e.mu.Unlock() }
func (e *fakeEdge) latest() shown {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.shows) == 0 {
		return shown{}
	}
	return e.shows[len(e.shows)-1]
}
func (e *fakeEdge) counts() (starts, stops, quits int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.starts, e.stops, e.quits
}

// microphones says, for each start, whether it asked for the microphone.
func (e *fakeEdge) microphones() []bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	var asked []bool
	for _, c := range e.asked {
		asked = append(asked, c.Microphone)
	}
	return asked
}

type fixture struct {
	t        *testing.T
	edge     *fakeEdge
	app      *App
	config   Config
	spool    string
	out      string
	settings string
	at       time.Time // the clock, which a fresh process carries on
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{t: t, edge: &fakeEdge{}, config: Config{PieceSeconds: 4}, spool: filepath.Join(dir, "spool"), out: filepath.Join(dir, "recordings"),
		settings: filepath.Join(dir, "settings.json"), at: time.Date(2026, 11, 13, 18, 30, 0, 0, time.UTC)}
	f.app = f.newApp()
	return f
}

// withMicrophone is a fixture whose recordings ask for the microphone.
func withMicrophone(t *testing.T) *fixture {
	f := newFixture(t)
	f.config.Microphone = true
	f.app = f.newApp()
	return f
}

// newApp is a fresh process over the same directories.
func (f *fixture) newApp() *App {
	return New(f.edge, f.config, f.spool, f.out, f.settings, func() time.Time { f.at = f.at.Add(time.Second); return f.at })
}

// idle waits for the join that follows a recording's end.
func (f *fixture) idle(app *App) {
	f.t.Helper()
	for i := 0; i < 2000; i++ {
		app.mu.Lock()
		done := app.state == idle
		app.mu.Unlock()
		if done {
			return
		}
		time.Sleep(time.Millisecond)
	}
	f.t.Fatal("the app never came back to idle")
}

func (f *fixture) files() []string {
	f.t.Helper()
	entries, err := os.ReadDir(f.out)
	if err != nil && !os.IsNotExist(err) {
		f.t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func (f *fixture) record(app *App, media ...string) {
	f.t.Helper()
	app.Menu(Start)
	app.Event(Started, "")
	app.Segment(true, []byte("OPEN|"))
	for _, m := range media {
		app.Segment(false, []byte(m))
	}
}

func TestARecordingIsJoinedWhenItStops(t *testing.T) {
	f := newFixture(t)
	f.app.Launched()
	if s := f.edge.latest(); s.recording || s.last != "" || s.note != "" {
		t.Fatalf("a first launch shows %+v", s)
	}
	f.app.Menu(Start)
	if s := f.edge.latest(); s.recording || s.note != "Starting…" {
		t.Fatalf("before capture runs the dot shows %+v", s)
	}
	f.app.Event(Started, "")
	if s := f.edge.latest(); !s.recording || s.note != "" {
		t.Fatalf("while recording the dot shows %+v", s)
	}
	f.app.Segment(true, []byte("OPEN|"))
	f.app.Segment(false, []byte("one|"))
	f.app.Segment(false, []byte("two|"))
	f.app.Menu(Stop)
	if _, stops, _ := f.edge.counts(); stops != 1 {
		t.Fatalf("stop was asked %d times", stops)
	}
	// The writer hands its last segment over before it says it finished.
	f.app.Segment(false, []byte("three|"))
	f.app.Event(Finished, "")
	f.idle(f.app)
	names := f.files()
	if len(names) != 1 || names[0] != "2026-11-13 18.30.01 On Air.mp4" {
		t.Fatalf("recordings: %v", names)
	}
	path := filepath.Join(f.out, names[0])
	if got, _ := os.ReadFile(path); string(got) != "OPEN|one|two|three|" {
		t.Fatalf("the recording holds %q", got)
	}
	if s := f.edge.latest(); s.recording || s.last != path || s.note != "" {
		t.Fatalf("after stopping the dot shows %+v", s)
	}
	// Joined, so it isn't taken for one that was cut short.
	f.newApp().Launched()
	if names := f.files(); len(names) != 1 {
		t.Fatalf("a finished recording was recovered again: %v", names)
	}
	f.app.Menu(ShowLast)
	f.app.Menu(OpenFolder)
	if len(f.edge.revealed) != 2 || f.edge.revealed[0] != path || f.edge.revealed[1] != f.out {
		t.Fatalf("revealed %v", f.edge.revealed)
	}
}

func TestOneRecordingAtATime(t *testing.T) {
	f := newFixture(t)
	f.record(f.app, "one|")
	f.app.Menu(Start)
	if starts, _, _ := f.edge.counts(); starts != 1 {
		t.Fatalf("a second recording was started: %d", starts)
	}
	if s := f.edge.latest(); !s.recording || !strings.Contains(s.note, "already recording") {
		t.Fatalf("a refused start shows %+v", s)
	}
	entries, _ := os.ReadDir(f.spool)
	if len(entries) != 1 {
		t.Fatalf("a refused start made a second recording: %v", entries)
	}
	// Stop does nothing when nothing is recording.
	idleApp := newFixture(t)
	idleApp.app.Menu(Stop)
	if _, stops, _ := idleApp.edge.counts(); stops != 0 {
		t.Fatal("stop was asked with nothing recording")
	}
}

// A kill leaves the pieces and no word of how it ended. The next launch
// joins what is whole, once, and leaves the pieces where they are.
func TestAKilledRecordingIsRecoveredAtTheNextLaunch(t *testing.T) {
	f := newFixture(t)
	f.record(f.app, "one|", "two|")
	// Killed: no Stop, no Finished. A part of piece 3 never got its name.
	dirs, _ := os.ReadDir(f.spool)
	rec := filepath.Join(f.spool, dirs[0].Name())
	if err := os.WriteFile(filepath.Join(rec, ".piece-000003.mp4.part"), []byte("OPEN|thr"), 0o600); err != nil {
		t.Fatal(err)
	}
	if names := f.files(); len(names) != 0 {
		t.Fatalf("something was joined before the recording ended: %v", names)
	}
	again := f.newApp()
	again.Launched()
	names := f.files()
	if len(names) != 1 || names[0] != "2026-11-13 18.30.01 On Air (recovered).mp4" {
		t.Fatalf("recovered: %v", names)
	}
	path := filepath.Join(f.out, names[0])
	if got, _ := os.ReadFile(path); string(got) != "OPEN|one|two|" {
		t.Fatalf("the recovered recording holds %q", got)
	}
	if s := f.edge.latest(); s.recording || s.last != path || s.note != "Recovered a recording that was cut short: 2 pieces, about 8s." {
		t.Fatalf("after recovery the dot shows %+v", s)
	}
	if left, _ := os.ReadDir(rec); len(left) != 5 {
		t.Fatalf("recovery changed the pieces: %v", left)
	}
	f.newApp().Launched()
	if names := f.files(); len(names) != 1 {
		t.Fatalf("a recording was recovered twice: %v", names)
	}
}

func TestCaptureThatStopsByItselfKeepsWhatItRecorded(t *testing.T) {
	f := newFixture(t)
	f.record(f.app, "one|")
	f.app.Event(Failed, "capture stopped: the display went away.")
	f.idle(f.app)
	names := f.files()
	if len(names) != 1 {
		t.Fatalf("recordings: %v", names)
	}
	s := f.edge.latest()
	if s.recording || s.last != filepath.Join(f.out, names[0]) || !strings.Contains(s.note, "the display went away") || !strings.Contains(s.note, "1 piece, about 4s") {
		t.Fatalf("after capture stopped the dot shows %+v", s)
	}
	// Capture that never started leaves nothing behind, and says why.
	g := newFixture(t)
	g.app.Menu(Start)
	g.app.Event(Failed, "On Air can't see the screen.")
	g.idle(g.app)
	if s := g.edge.latest(); s.recording || s.last != "" || s.note != "On Air can't see the screen." {
		t.Fatalf("a start that failed shows %+v", s)
	}
	if entries, _ := os.ReadDir(g.spool); len(entries) != 0 {
		t.Fatalf("a start that failed left a recording: %v", entries)
	}
	g.app.Menu(Start)
	if starts, _, _ := g.edge.counts(); starts != 2 {
		t.Fatal("a failed start can't be tried again")
	}
}

func TestAPieceThatCantBeWrittenStopsTheRecording(t *testing.T) {
	f := newFixture(t)
	f.record(f.app, "one|")
	dirs, _ := os.ReadDir(f.spool)
	rec := filepath.Join(f.spool, dirs[0].Name())
	if err := os.Chmod(rec, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(rec, 0o700)
	f.app.Segment(false, []byte("two|"))
	if _, stops, _ := f.edge.counts(); stops != 1 {
		t.Fatalf("stop was asked %d times after a piece couldn't be written", stops)
	}
	if s := f.edge.latest(); !strings.Contains(s.note, "a piece couldn't be written") {
		t.Fatalf("a failed piece shows %+v", s)
	}
	os.Chmod(rec, 0o700)
	f.app.Event(Finished, "")
	f.idle(f.app)
	names := f.files()
	if len(names) != 1 {
		t.Fatalf("recordings: %v", names)
	}
	if got, _ := os.ReadFile(filepath.Join(f.out, names[0])); string(got) != "OPEN|one|" {
		t.Fatalf("the recording holds %q", got)
	}
	if s := f.edge.latest(); !strings.Contains(s.note, "a piece couldn't be written") {
		t.Fatalf("the reason was lost: %+v", s)
	}
}

func TestAFailedJoinLeavesThePieces(t *testing.T) {
	f := newFixture(t)
	f.record(f.app, "one|")
	// Something is where the recording's folder should be.
	if err := os.WriteFile(f.out, []byte("in the way"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.app.Menu(Stop)
	f.app.Event(Finished, "")
	f.idle(f.app)
	s := f.edge.latest()
	if s.recording || s.last != "" || !strings.Contains(s.note, "safe in pieces") {
		t.Fatalf("a failed join shows %+v", s)
	}
	dirs, _ := os.ReadDir(f.spool)
	rec := filepath.Join(f.spool, dirs[0].Name())
	if got, _ := os.ReadFile(filepath.Join(rec, "piece-000001.mp4")); string(got) != "OPEN|one|" {
		t.Fatalf("the piece is %q", got)
	}
	// With the way clear, the next launch joins it.
	if err := os.Remove(f.out); err != nil {
		t.Fatal(err)
	}
	f.newApp().Launched()
	if names := f.files(); len(names) != 1 || !strings.Contains(names[0], "(recovered)") {
		t.Fatalf("recordings: %v", names)
	}
}

func TestQuitFinishesTheRecordingFirst(t *testing.T) {
	f := newFixture(t)
	f.record(f.app, "one|")
	f.app.Menu(Quit)
	if _, stops, quits := f.edge.counts(); stops != 1 || quits != 0 {
		t.Fatalf("quit while recording: %d stops, %d quits", stops, quits)
	}
	f.app.Event(Finished, "")
	f.idle(f.app)
	for i := 0; i < 2000; i++ {
		if _, _, quits := f.edge.counts(); quits == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, _, quits := f.edge.counts(); quits != 1 || len(f.files()) != 1 {
		t.Fatalf("after the recording finished: %d quits, %v", quits, f.files())
	}
	g := newFixture(t)
	g.app.Menu(Quit)
	if _, _, quits := g.edge.counts(); quits != 1 {
		t.Fatal("quit with nothing recording didn't quit")
	}
}

// The display is recorded without the microphone rather than not at all,
// and the menu says so while it records and after.
func TestTheDisplayIsRecordedWhenTheMicrophoneCantBe(t *testing.T) {
	f := withMicrophone(t)
	f.app.Menu(Start)
	f.app.Event(MicrophoneFailed, "no microphone was found.")
	if asked := f.edge.microphones(); len(asked) != 2 || !asked[0] || asked[1] {
		t.Fatalf("starts asked for the microphone: %v", asked)
	}
	if s := f.edge.latest(); s.recording || s.note != "Starting without the microphone…" {
		t.Fatalf("starting again shows %+v", s)
	}
	f.app.Event(Started, "")
	if s := f.edge.latest(); !s.recording || s.note != "The microphone isn't being recorded: no microphone was found." {
		t.Fatalf("recording without the microphone shows %+v", s)
	}
	f.app.Segment(true, []byte("OPEN|"))
	f.app.Segment(false, []byte("one|"))
	f.app.Menu(Stop)
	f.app.Event(Finished, "")
	f.idle(f.app)
	names := f.files()
	if len(names) != 1 {
		t.Fatalf("recordings: %v", names)
	}
	if s := f.edge.latest(); s.recording || s.last != filepath.Join(f.out, names[0]) || s.note != "The microphone wasn't recorded: no microphone was found." {
		t.Fatalf("after stopping the dot shows %+v", s)
	}
	// The next recording asks for the microphone again.
	f.app.Menu(Start)
	if asked := f.edge.microphones(); len(asked) != 3 || !asked[2] {
		t.Fatalf("starts asked for the microphone: %v", asked)
	}
	f.app.Event(Started, "")
	if s := f.edge.latest(); !s.recording || s.note != "" {
		t.Fatalf("recording with the microphone shows %+v", s)
	}
}

func TestTheMicrophoneIsLeftOutOnceARecording(t *testing.T) {
	// Capture that doesn't start without the microphone either has failed,
	// and nothing is kept.
	f := withMicrophone(t)
	f.app.Menu(Start)
	f.app.Event(MicrophoneFailed, "On Air isn't allowed to use it.")
	f.app.Event(Failed, "On Air can't see the screen.")
	f.idle(f.app)
	if starts, _, _ := f.edge.counts(); starts != 2 {
		t.Fatalf("capture was started %d times", starts)
	}
	if s := f.edge.latest(); s.recording || s.last != "" || s.note != "On Air can't see the screen." {
		t.Fatalf("a start that failed shows %+v", s)
	}
	if entries, _ := os.ReadDir(f.spool); len(entries) != 0 {
		t.Fatalf("a start that failed left a recording: %v", entries)
	}
	// A second word about the microphone is a failure, not a third start.
	g := withMicrophone(t)
	g.app.Menu(Start)
	g.app.Event(MicrophoneFailed, "capture didn't start with it.")
	g.app.Event(MicrophoneFailed, "capture didn't start with it, again.")
	g.idle(g.app)
	if starts, _, _ := g.edge.counts(); starts != 2 {
		t.Fatalf("capture was started %d times", starts)
	}
	if s := g.edge.latest(); s.recording || s.note != "capture didn't start with it, again." {
		t.Fatalf("a second microphone failure shows %+v", s)
	}
	// So is any word about it when none was asked for.
	h := newFixture(t)
	h.app.Menu(Start)
	h.app.Event(MicrophoneFailed, "no microphone was found.")
	h.idle(h.app)
	if starts, _, _ := h.edge.counts(); starts != 1 {
		t.Fatalf("capture was started %d times", starts)
	}
}

// A recording that stops by itself says why, and that it has no microphone.
func TestARecordingWithoutTheMicrophoneThatStopsByItselfSaysBoth(t *testing.T) {
	f := withMicrophone(t)
	f.app.Menu(Start)
	f.app.Event(MicrophoneFailed, "no microphone was found.")
	f.app.Event(Started, "")
	f.app.Segment(true, []byte("OPEN|"))
	f.app.Segment(false, []byte("one|"))
	f.app.Event(Failed, "capture stopped: the display went away.")
	f.idle(f.app)
	s := f.edge.latest()
	if !strings.Contains(s.note, "the display went away") || !strings.HasSuffix(s.note, "The microphone wasn't recorded: no microphone was found.") {
		t.Fatalf("the dot shows %+v", s)
	}
}

// Quit while capture is starting keeps what it records, as Quit while
// recording does, and quits once it is joined.
func TestQuitWhileStartingFinishesTheRecording(t *testing.T) {
	f := newFixture(t)
	f.app.Menu(Start)
	f.app.Menu(Quit)
	if _, stops, quits := f.edge.counts(); stops != 0 || quits != 0 {
		t.Fatalf("quit while starting: %d stops, %d quits", stops, quits)
	}
	f.app.Event(Started, "")
	if _, stops, _ := f.edge.counts(); stops != 1 {
		t.Fatalf("capture that started after Quit was stopped %d times", stops)
	}
	f.app.Segment(true, []byte("OPEN|"))
	f.app.Segment(false, []byte("one|"))
	f.app.Event(Finished, "")
	f.idle(f.app)
	for i := 0; i < 2000; i++ {
		if _, _, quits := f.edge.counts(); quits == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, _, quits := f.edge.counts(); quits != 1 || len(f.files()) != 1 {
		t.Fatalf("after the recording finished: %d quits, %v", quits, f.files())
	}
	// Quit while the microphone is being left out doesn't start again.
	g := withMicrophone(t)
	g.app.Menu(Start)
	g.app.Menu(Quit)
	g.app.Event(MicrophoneFailed, "no microphone was found.")
	g.idle(g.app)
	for i := 0; i < 2000; i++ {
		if _, _, quits := g.edge.counts(); quits == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if starts, _, quits := g.edge.counts(); starts != 1 || quits != 1 {
		t.Fatalf("quit while starting with the microphone: %d starts, %d quits", starts, quits)
	}
}
