package recorder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	builtIn = Display{ID: "A", Name: "Built-in Display", Width: 1512, Height: 982, Main: true}
	wide    = Display{ID: "B", Name: "Wide Display", Width: 2560, Height: 1440}
)

// lastAsked is what the latest start asked the edge to record.
func (e *fakeEdge) lastAsked() Config {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.asked) == 0 {
		return Config{}
	}
	return e.asked[len(e.asked)-1]
}

func (e *fakeEdge) latestChoices() Choices {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.choices
}

// picked names the displays the menu marks.
func picked_(c Choices) []string {
	var on []string
	for _, d := range c.Displays {
		if d.Picked {
			on = append(on, d.ID)
		}
	}
	return on
}

func (f *fixture) connected(ds ...Display) { f.app.Displays(ds) }

// startsOn presses Start on app and says what capture was asked to record.
// A Start that asks nothing fails the test.
func (f *fixture) startsOn(app *App) Target {
	f.t.Helper()
	before, _, _ := f.edge.counts()
	app.Menu(Start)
	if after, _, _ := f.edge.counts(); after != before+1 {
		f.t.Fatalf("Start didn't ask for capture: %+v", f.edge.latest())
	}
	return f.edge.lastAsked().Target
}

// started records from Start until capture runs, and says what it was asked
// to record.
func (f *fixture) started() Target {
	f.t.Helper()
	t := f.startsOn(f.app)
	f.app.Event(Started, "")
	return t
}

// stopped stops a recording with one piece and waits for its join.
func (f *fixture) stopped() {
	f.t.Helper()
	f.app.Segment(true, []byte("OPEN|"))
	f.app.Segment(false, []byte("one|"))
	f.app.Menu(Stop)
	f.app.Event(Finished, "")
	f.idle(f.app)
}

func TestUntilAPersonPicksTheMainDisplayIsRecorded(t *testing.T) {
	f := newFixture(t)
	f.connected(builtIn, wide)
	c := f.edge.latestChoices()
	if !c.Open || len(c.Displays) != 2 || c.Displays[0].Title != "Built-in Display, 1512 × 982" || c.Displays[1].Title != "Wide Display, 2560 × 1440" {
		t.Fatalf("the menu offers %+v", c)
	}
	if on := picked_(c); len(on) != 1 || on[0] != "A" || c.Area != "" || c.Window != "" {
		t.Fatalf("the menu marks %v, %+v", on, c)
	}
	if got := f.started(); got != (Target{}) {
		t.Fatalf("Start recorded %+v", got)
	}
	if c := f.edge.latestChoices(); c.Open {
		t.Fatal("a choice could be made while recording")
	}
	f.stopped()
	if s := f.edge.latest(); s.note != "" || !f.edge.latestChoices().Open {
		t.Fatalf("after the recording: %+v, %+v", s, f.edge.latestChoices())
	}
}

// A display or an area is remembered between recordings and launches.
func TestAPickedDisplayIsRecordedAndRemembered(t *testing.T) {
	f := newFixture(t)
	f.connected(builtIn, wide)
	f.app.PickDisplay("B")
	if on := picked_(f.edge.latestChoices()); len(on) != 1 || on[0] != "B" {
		t.Fatalf("the menu marks %v", on)
	}
	if got := f.started(); got != (Target{Display: "B"}) {
		t.Fatalf("Start recorded %+v", got)
	}
	f.stopped()
	if got := f.started(); got != (Target{Display: "B"}) {
		t.Fatalf("the next Start recorded %+v", got)
	}
	f.stopped()
	again := f.newApp()
	again.Displays([]Display{builtIn, wide})
	if got := f.startsOn(again); got != (Target{Display: "B"}) {
		t.Fatalf("after a relaunch Start recorded %+v", got)
	}
	// A display that went away while the menu was open isn't picked.
	g := newFixture(t)
	g.connected(builtIn)
	g.app.PickDisplay("B")
	if s := g.edge.latest(); !strings.Contains(s.note, "isn't connected") || g.app.picked.Display != "" {
		t.Fatalf("picking a display that is gone: %+v, %+v", s, g.app.picked)
	}
}

func TestAPickedDisplayThatIsntConnectedGivesWayToTheMainOne(t *testing.T) {
	f := newFixture(t)
	f.connected(builtIn, wide)
	f.app.PickDisplay("B")
	f.connected(builtIn)
	if on := picked_(f.edge.latestChoices()); len(on) != 1 || on[0] != "A" {
		t.Fatalf("with the picked display gone the menu marks %v", on)
	}
	if got := f.started(); got != (Target{}) {
		t.Fatalf("Start recorded %+v", got)
	}
	if s := f.edge.latest(); !s.recording || s.note != "The picked display isn't connected, so On Air is recording the main display." {
		t.Fatalf("while recording: %+v", s)
	}
	f.stopped()
	if s := f.edge.latest(); s.note != "The picked display wasn't connected, so On Air recorded the main display." {
		t.Fatalf("after the recording: %+v", s)
	}
	// It is still the choice, once it is back.
	f.connected(builtIn, wide)
	if got := f.started(); got != (Target{Display: "B"}) {
		t.Fatalf("with the display back Start recorded %+v", got)
	}
	if s := f.edge.latest(); s.note != "" {
		t.Fatalf("while recording: %+v", s)
	}
}

func TestAnAreaIsRecordedRememberedAndCleared(t *testing.T) {
	f := newFixture(t)
	f.connected(builtIn, wide)
	f.app.Menu(SelectArea)
	f.app.PickDisplay("B")
	f.app.Menu(SelectArea)
	if f.edge.areas[0] != "A" || f.edge.areas[1] != "B" {
		t.Fatalf("areas were drawn on %v", f.edge.areas)
	}
	area := Area{X: 100, Y: 50, Width: 1280, Height: 720}
	f.app.AreaPicked("B", area)
	if c := f.edge.latestChoices(); c.Area != "Area 1280 × 720" || len(picked_(c)) != 1 || picked_(c)[0] != "B" {
		t.Fatalf("the menu offers %+v", c)
	}
	if got := f.started(); got != (Target{Display: "B", Area: area}) {
		t.Fatalf("Start recorded %+v", got)
	}
	f.stopped()
	again := f.newApp()
	again.Displays([]Display{builtIn, wide})
	again.Menu(WholeDisplay)
	if c := f.edge.latestChoices(); c.Area != "" {
		t.Fatalf("Whole Display left %+v", c)
	}
	if got := f.startsOn(again); got != (Target{Display: "B"}) {
		t.Fatalf("after Whole Display Start recorded %+v", got)
	}
}

func TestAnAreaThatNoLongerFitsGivesWayToTheWholeDisplay(t *testing.T) {
	f := newFixture(t)
	f.connected(builtIn, wide)
	f.app.AreaPicked("B", Area{X: 1920, Y: 0, Width: 640, Height: 480})
	smaller := wide
	smaller.Width, smaller.Height = 1920, 1080
	f.connected(builtIn, smaller)
	if got := f.started(); got != (Target{Display: "B"}) {
		t.Fatalf("Start recorded %+v", got)
	}
	if s := f.edge.latest(); s.note != "The picked area doesn't fit the display any more, so On Air is recording the whole display." {
		t.Fatalf("while recording: %+v", s)
	}
	f.stopped()
	if s := f.edge.latest(); s.note != "The picked area didn't fit the display, so On Air recorded the whole display." {
		t.Fatalf("after the recording: %+v", s)
	}
}

func TestAnAreaTooSmallOrOffItsDisplayIsRefused(t *testing.T) {
	f := newFixture(t)
	f.connected(builtIn, wide)
	f.app.AreaPicked("A", Area{X: 10, Y: 10, Width: 40, Height: 300})
	if s := f.edge.latest(); !strings.Contains(s.note, "too small") || f.app.picked.Area != nil {
		t.Fatalf("a small area: %+v, %+v", s, f.app.picked)
	}
	f.app.AreaPicked("A", Area{X: 1000, Y: 10, Width: 640, Height: 300})
	if s := f.edge.latest(); !strings.Contains(s.note, "isn't all on the display") || f.app.picked.Area != nil {
		t.Fatalf("an area off the display: %+v, %+v", s, f.app.picked)
	}
	f.app.AreaPicked("C", Area{Width: 640, Height: 300})
	if s := f.edge.latest(); !strings.Contains(s.note, "isn't connected") || f.app.picked.Area != nil {
		t.Fatalf("an area on no display: %+v, %+v", s, f.app.picked)
	}
	if got := f.started(); got != (Target{}) {
		t.Fatalf("Start recorded %+v", got)
	}
}

func TestAPickedWindowIsRecordedUntilItIsGone(t *testing.T) {
	f := newFixture(t)
	f.connected(builtIn, wide)
	f.app.PickDisplay("B")
	f.app.Menu(SelectWindow)
	if f.edge.windows != 1 {
		t.Fatalf("a window was asked for %d times", f.edge.windows)
	}
	f.app.WindowPicked("Video Call — Weekly Review")
	if c := f.edge.latestChoices(); c.Window != "Window: Video Call — Weekly Review" || len(picked_(c)) != 0 {
		t.Fatalf("the menu offers %+v", c)
	}
	if got := f.started(); got != (Target{Window: true}) {
		t.Fatalf("Start recorded %+v", got)
	}
	f.stopped()
	// Closed since, which only capture can tell: the picked display, whole,
	// is recorded instead, and the window is forgotten.
	f.startsOn(f.app)
	f.app.Event(TargetGone, "the picked window is gone.")
	if got := f.edge.lastAsked().Target; got != (Target{Display: "B"}) {
		t.Fatalf("with the window gone Start recorded %+v", got)
	}
	f.app.Event(Started, "")
	if s := f.edge.latest(); s.note != "The picked window is gone, so On Air is recording the whole display." {
		t.Fatalf("while recording: %+v", s)
	}
	if c := f.edge.latestChoices(); c.Window != "" {
		t.Fatalf("the window is still offered: %+v", c)
	}
	f.stopped()
	if s := f.edge.latest(); s.note != "The picked window was gone, so On Air recorded the whole display." {
		t.Fatalf("after the recording: %+v", s)
	}
	if got := f.started(); got != (Target{Display: "B"}) {
		t.Fatalf("the next Start recorded %+v", got)
	}
	// A window isn't remembered across launches.
	f.stopped()
	f.app.WindowPicked("Video Call")
	again := f.newApp()
	again.Displays([]Display{builtIn, wide})
	if got := f.startsOn(again); got != (Target{Display: "B"}) {
		t.Fatalf("after a relaunch Start recorded %+v", got)
	}
}

// A window that closes while it is recorded ends the recording as capture
// stopping by itself does: what was recorded is kept, and the menu says why.
func TestAWindowThatClosesMidRecordingEndsIt(t *testing.T) {
	f := newFixture(t)
	f.connected(builtIn)
	f.app.WindowPicked("Video Call")
	f.started()
	f.app.Segment(true, []byte("OPEN|"))
	f.app.Segment(false, []byte("one|"))
	f.app.Event(WindowClosed, "")
	if _, stops, _ := f.edge.counts(); stops != 1 {
		t.Fatalf("capture was stopped %d times", stops)
	}
	f.app.Event(Finished, "")
	f.idle(f.app)
	names := f.files()
	if len(names) != 1 {
		t.Fatalf("recordings: %v", names)
	}
	s := f.edge.latest()
	if s.recording || s.note != "Recording stopped by itself: the window being recorded closed. What was recorded is saved: 1 piece, about 4s." {
		t.Fatalf("after the window closed: %+v", s)
	}
	if c := f.edge.latestChoices(); c.Window != "" || !c.Open {
		t.Fatalf("the menu offers %+v", c)
	}
	// Closing while nothing records only forgets it.
	f.app.WindowPicked("Another Call")
	f.app.Event(WindowClosed, "")
	if _, stops, _ := f.edge.counts(); stops != 1 || f.edge.latestChoices().Window != "" {
		t.Fatalf("a window that closed while idle: %d stops, %+v", stops, f.edge.latestChoices())
	}
	f.app.Event(PickerFailed, "the system refused.")
	if s := f.edge.latest(); s.note != "The window picker didn't open: the system refused." {
		t.Fatalf("a picker that failed: %+v", s)
	}
}

func TestAChoiceCantChangeWhileRecording(t *testing.T) {
	f := newFixture(t)
	f.connected(builtIn, wide)
	f.started()
	f.app.PickDisplay("B")
	f.app.AreaPicked("A", Area{Width: 640, Height: 480})
	f.app.WindowPicked("Video Call")
	f.app.Menu(SelectArea)
	f.app.Menu(SelectWindow)
	f.app.Menu(WholeDisplay)
	if s := f.edge.latest(); !s.recording || s.note != "What is recorded can't change while a recording runs." {
		t.Fatalf("a choice while recording shows %+v", s)
	}
	if len(f.edge.areas) != 0 || f.edge.windows != 0 || f.app.picked != (picked{}) || f.app.window != "" {
		t.Fatalf("a choice was made while recording: %v, %d, %+v, %q", f.edge.areas, f.edge.windows, f.app.picked, f.app.window)
	}
	if _, err := os.Stat(f.settings); !os.IsNotExist(err) {
		t.Fatalf("the settings were written while recording: %v", err)
	}
	f.stopped()
	f.app.PickDisplay("B")
	if f.app.picked.Display != "B" {
		t.Fatal("a choice couldn't be made after the recording")
	}
}

// Start never fails because of a remembered choice: what the edge can't
// find gives way, once each, down to the main display. That is the end.
func TestWhatTheEdgeCantFindGivesWay(t *testing.T) {
	f := withMicrophone(t)
	f.connected(builtIn, wide)
	f.app.AreaPicked("B", Area{X: 0, Y: 0, Width: 800, Height: 600})
	f.app.WindowPicked("Video Call")
	f.startsOn(f.app)
	f.app.Event(MicrophoneFailed, "no microphone was found.")
	f.app.Event(TargetGone, "the picked window is gone.")
	f.app.Event(TargetGone, "the picked display isn't connected.")
	asked := f.edge.asked
	want := []Config{
		{PieceSeconds: 4, Microphone: true, Target: Target{Window: true}},
		{PieceSeconds: 4, Target: Target{Window: true}},
		{PieceSeconds: 4, Target: Target{Display: "B"}},
		{PieceSeconds: 4, Target: Target{}},
	}
	if len(asked) != len(want) {
		t.Fatalf("starts: %+v", asked)
	}
	for i := range want {
		if asked[i] != want[i] {
			t.Fatalf("start %d asked %+v, want %+v", i+1, asked[i], want[i])
		}
	}
	// What gave way for one recording stays picked for the next.
	if f.app.picked.Area == nil || f.app.picked.Display != "B" {
		t.Fatalf("the picked area was forgotten: %+v", f.app.picked)
	}
	f.app.Event(TargetGone, "no display was found.")
	f.idle(f.app)
	if starts, _, _ := f.edge.counts(); starts != 4 {
		t.Fatalf("capture was started %d times", starts)
	}
	if s := f.edge.latest(); s.recording || s.note != "no display was found." {
		t.Fatalf("with nothing left to record: %+v", s)
	}
	// An area the edge can't fit gives way to its whole display.
	g := newFixture(t)
	g.connected(builtIn, wide)
	g.app.AreaPicked("B", Area{X: 10, Y: 10, Width: 800, Height: 600})
	g.startsOn(g.app)
	g.app.Event(TargetGone, "the picked area isn't on the display.")
	if got := g.edge.lastAsked().Target; got != (Target{Display: "B"}) {
		t.Fatalf("Start recorded %+v", got)
	}
	g.app.Event(Started, "")
	if s := g.edge.latest(); s.note != "The picked area doesn't fit the display any more, so On Air is recording the whole display." {
		t.Fatalf("while recording: %+v", s)
	}
}

// Settings that can't be read or written never stop a recording.
func TestSettingsThatCantBeKeptDontStopARecording(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(f.settings, []byte("not settings"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.app = f.newApp()
	f.connected(builtIn, wide)
	if s := f.edge.latest(); !strings.Contains(s.note, "couldn't read what to record") {
		t.Fatalf("unreadable settings show %+v", s)
	}
	if got := f.started(); got != (Target{}) {
		t.Fatalf("Start recorded %+v", got)
	}
	f.stopped()
	g := newFixture(t)
	g.settings = filepath.Join(g.out, "in", "the", "way")
	if err := os.WriteFile(g.out, []byte("a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.app = g.newApp()
	g.connected(builtIn, wide)
	g.app.PickDisplay("B")
	if s := g.edge.latest(); !strings.HasPrefix(s.note, "On Air records this until it quits, but couldn't remember it") {
		t.Fatalf("settings that can't be written show %+v", s)
	}
	if got := g.startsOn(g.app); got != (Target{Display: "B"}) {
		t.Fatalf("Start recorded %+v", got)
	}
}
