package recorder

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gitdek/onair/internal/delivery"
	"github.com/gitdek/onair/internal/spool"
)

// first is the name of a fixture's first recording.
const first = "2026-11-13 18.30.01 On Air.mp4"

// picks gives a fixture a folder of On the Record's, picked from the menu.
func (f *fixture) picks() string {
	f.t.Helper()
	folder := filepath.Join(filepath.Dir(f.out), "On the Record")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		f.t.Fatal(err)
	}
	f.app.FolderChosen(folder)
	f.idle(f.app)
	return folder
}

// recording is the directory in the spool of the fixture's only recording.
func (f *fixture) recording() string {
	f.t.Helper()
	dirs, err := os.ReadDir(f.spool)
	if err != nil || len(dirs) != 1 {
		f.t.Fatalf("the spool holds %v, %v", dirs, err)
	}
	return filepath.Join(f.spool, dirs[0].Name())
}

func list(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// Until a folder is picked, recordings go to On Air's own folder, by the
// same steps, and the menu says so. A folder picked from the menu is where
// they go from then on, and it is remembered with the other settings.
func TestRecordingsGoToThePickedFolder(t *testing.T) {
	f := newFixture(t)
	f.connected(builtIn, wide)
	if c := f.edge.latestChoices(); c.Folder != "On Air's own folder, until one is picked" {
		t.Fatalf("the menu offers %+v", c)
	}
	f.app.Menu(ChooseFolder)
	if len(f.edge.folders) != 1 || f.edge.folders[0] != f.out {
		t.Fatalf("the folder picker opened on %v", f.edge.folders)
	}
	f.started()
	f.stopped()
	if names := list(t, f.out); len(names) != 1 || names[0] != first {
		t.Fatalf("On Air's own folder holds %v", names)
	}

	folder := f.picks()
	if c := f.edge.latestChoices(); c.Folder != "On the Record" {
		t.Fatalf("the menu offers %+v", c)
	}
	f.app.PickDisplay("B")
	var k struct {
		Display string `json:"display"`
		Folder  string `json:"folder"`
	}
	if b, err := os.ReadFile(f.settings); err != nil || json.Unmarshal(b, &k) != nil || k.Display != "B" || k.Folder != folder {
		t.Fatalf("the settings: %+v, %v", k, err)
	}
	f.started()
	f.stopped()
	names := list(t, folder)
	if len(names) != 1 || !strings.HasSuffix(names[0], " On Air.mp4") {
		t.Fatalf("On the Record's folder holds %v", names)
	}
	path := filepath.Join(folder, names[0])
	if got, _ := os.ReadFile(path); string(got) != "OPEN|one|" {
		t.Fatalf("the recording holds %q", got)
	}
	if s := f.edge.latest(); s.last != path || s.note != "Handed “"+names[0]+"” to “On the Record”." {
		t.Fatalf("after the recording: %+v", s)
	}
	handing := false
	for _, s := range f.edge.shows {
		handing = handing || s.note == "Handing “"+names[0]+"” to “On the Record”…"
	}
	if !handing {
		t.Fatal("the menu never said where the recording was going")
	}
	if names := list(t, f.out); len(names) != 1 {
		t.Fatalf("On Air's own folder holds %v", names)
	}
	// Show opens it in the folder, and so does opening the folder.
	f.app.Menu(ShowLast)
	f.app.Menu(OpenFolder)
	f.app.Menu(ChooseFolder)
	if r := f.edge.revealed; len(r) != 2 || r[0] != path || r[1] != folder || f.edge.folders[1] != folder {
		t.Fatalf("revealed %v, the picker opened on %v", r, f.edge.folders)
	}

	// Remembered across launches.
	f.app = f.launch()
	f.connected(builtIn, wide)
	if c := f.edge.latestChoices(); c.Folder != "On the Record" {
		t.Fatalf("after a launch the menu offers %+v", c)
	}
	if got := f.started(); got != (Target{Display: "B"}) {
		t.Fatalf("after a launch Start recorded %+v", got)
	}
	f.stopped()
	if names := list(t, folder); len(names) != 2 {
		t.Fatalf("On the Record's folder holds %v", names)
	}
}

// A hand-over that fails keeps the recording in the spool, and the menu
// says why and where it is. It is tried again at the next launch, and when
// a folder is picked.
func TestAFailedHandOverIsSaidAndTriedAgain(t *testing.T) {
	f := newFixture(t)
	folder := f.picks()
	// Unplugged, say.
	if err := os.Remove(folder); err != nil {
		t.Fatal(err)
	}
	f.record(f.app, "one|")
	f.app.Menu(Stop)
	f.app.Event(Finished, "")
	f.idle(f.app)
	rec := f.recording()
	own := filepath.Join(rec, first)
	failed := "“" + first + "” couldn't be handed to “On the Record”: the folder isn't there. It is safe in " + rec +
		", and On Air tries again at the next launch and when a folder is picked."
	if s := f.edge.latest(); s.last != own || s.note != failed {
		t.Fatalf("a failed hand-over shows %+v", s)
	}
	f.app.Menu(ShowLast)
	if len(f.edge.revealed) != 1 || f.edge.revealed[0] != own {
		t.Fatalf("revealed %v", f.edge.revealed)
	}
	if got, _ := os.ReadFile(own); string(got) != "OPEN|one|" {
		t.Fatalf("On Air's own copy holds %q", got)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatalf("a picked folder that was gone was made: %v", err)
	}
	// Opening the folder says it isn't there.
	f.app.Menu(OpenFolder)
	if s := f.edge.latest(); len(f.edge.revealed) != 1 || !strings.Contains(s.note, "isn't there: "+folder) {
		t.Fatalf("opening a folder that is gone: %+v, revealed %v", s, f.edge.revealed)
	}

	// Tried again at the next launch, and said again, and Show opens it.
	f.app = f.launch()
	if s := f.edge.latest(); s.last != own || !strings.Contains(s.note, failed) {
		t.Fatalf("after a launch: %+v", s)
	}

	// Tried again when a folder is picked, and handed over.
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	f.app.FolderChosen(folder)
	f.idle(f.app)
	if got, _ := os.ReadFile(filepath.Join(folder, first)); string(got) != "OPEN|one|" {
		t.Fatalf("the recording holds %q", got)
	}
	if s := f.edge.latest(); s.note != "Handed “"+first+"” to “On the Record”." {
		t.Fatalf("handed over at last: %+v", s)
	}
	if names := list(t, folder); len(names) != 1 {
		t.Fatalf("On the Record's folder holds %v", names)
	}
}

// A folder in On Air's spool, such as the directory the menu says a
// recording is safe in, is never where recordings go. Picking one is
// refused and said, the folder picked before stays, and the recording stays
// in the spool, three days on too.
func TestAFolderInOnAirsSpoolIsRefused(t *testing.T) {
	f := newFixture(t)
	folder := f.picks()
	if err := os.Remove(folder); err != nil {
		t.Fatal(err)
	}
	f.record(f.app, "one|")
	f.app.Menu(Stop)
	f.app.Event(Finished, "")
	f.idle(f.app)
	rec := f.recording()
	own := filepath.Join(rec, first)
	for _, picked := range []string{rec, f.spool} {
		f.app.FolderChosen(picked)
		f.idle(f.app)
		if s := f.edge.latest(); !strings.HasPrefix(s.note, "“"+filepath.Base(picked)+"” is in On Air's spool, where it keeps its own copies, so recordings can't go there.") {
			t.Fatalf("picking %s shows %+v", picked, s)
		}
		if c := f.edge.latestChoices(); c.Folder != "On the Record" {
			t.Fatalf("picking %s, the menu offers %+v", picked, c)
		}
	}
	var k struct {
		Folder string `json:"folder"`
	}
	if b, err := os.ReadFile(f.settings); err != nil || json.Unmarshal(b, &k) != nil || k.Folder != folder {
		t.Fatalf("the settings: %+v, %v", k, err)
	}
	f.at = f.at.Add(73 * time.Hour)
	f.app.Tidy()
	f.idle(f.app)
	f.app = f.launch()
	if got, _ := os.ReadFile(own); string(got) != "OPEN|one|" {
		t.Fatalf("On Air's own copy holds %q", got)
	}
}

// A file already under the recording's name is left exactly as it is, and
// the menu says so.
func TestAFileUnderTheRecordingsNameIsLeftAlone(t *testing.T) {
	f := newFixture(t)
	folder := f.picks()
	taken := filepath.Join(folder, first)
	if err := os.WriteFile(taken, []byte("another meeting"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.record(f.app, "one|")
	f.app.Menu(Stop)
	f.app.Event(Finished, "")
	f.idle(f.app)
	if got, _ := os.ReadFile(taken); string(got) != "another meeting" {
		t.Fatalf("the file under the name holds %q", got)
	}
	if names := list(t, folder); len(names) != 1 {
		t.Fatalf("On the Record's folder holds %v", names)
	}
	s := f.edge.latest()
	if s.last != filepath.Join(f.recording(), first) || !strings.Contains(s.note, "couldn't be handed to “On the Record”: a different file has its name there, and On Air leaves that file as it is.") {
		t.Fatalf("a name that's taken shows %+v", s)
	}
}

// A recording recovered after a kill is handed over as any other.
func TestARecoveredRecordingIsHandedOver(t *testing.T) {
	f := newFixture(t)
	folder := f.picks()
	f.record(f.app, "one|", "two|")
	f.app = f.launch()
	names := list(t, folder)
	if len(names) != 1 || names[0] != "2026-11-13 18.30.01 On Air (recovered).mp4" {
		t.Fatalf("On the Record's folder holds %v", names)
	}
	if got, _ := os.ReadFile(filepath.Join(folder, names[0])); string(got) != "OPEN|one|two|" {
		t.Fatalf("the recording holds %q", got)
	}
	if s := f.edge.latest(); s.last != filepath.Join(folder, names[0]) ||
		s.note != "Recovered a recording that was cut short: 2 pieces, about 8s. Handed “"+names[0]+"” to “On the Record”." {
		t.Fatalf("after the launch: %+v", s)
	}
}

// A hand-over that stops at a step it can't carry out is carried on from
// that step at the next launch.
func TestAHandOverThatStoppedIsCarriedOnAtTheNextLaunch(t *testing.T) {
	f := newFixture(t)
	folder := f.picks()
	// Something of another program's is where the copy goes, under the
	// hidden name, and On Air won't write through it.
	in := filepath.Join(folder, "."+first, "in the way")
	if err := os.MkdirAll(in, 0o755); err != nil {
		t.Fatal(err)
	}
	f.record(f.app, "one|")
	f.app.Menu(Stop)
	f.app.Event(Finished, "")
	f.idle(f.app)
	rec := f.recording()
	if s := f.edge.latest(); !strings.HasPrefix(s.note, "Handing “"+first+"” to “On the Record” stopped: ") ||
		!strings.HasSuffix(s.note, "It is safe in "+rec+", and On Air carries on at the next launch and when a folder is picked.") {
		t.Fatalf("a hand-over that stopped shows %+v", s)
	}
	if _, err := os.Stat(filepath.Join(folder, first)); !os.IsNotExist(err) {
		t.Fatalf("something is under the recording's name: %v", err)
	}
	if err := os.RemoveAll(filepath.Dir(in)); err != nil {
		t.Fatal(err)
	}
	f.app = f.launch()
	if got, _ := os.ReadFile(filepath.Join(folder, first)); string(got) != "OPEN|one|" {
		t.Fatalf("the recording holds %q", got)
	}
	if names := list(t, folder); len(names) != 1 {
		t.Fatalf("On the Record's folder holds %v", names)
	}
}

// A hand-over stopped where the core's model doesn't go says where the
// recording is safe, and that On Air can't carry the hand-over on by
// itself, since carried on it stops there again. Any other step that
// stopped is carried on at the next launch and when a folder is picked.
func TestAStuckHandOverSaysOnAirCantCarryItOn(t *testing.T) {
	rec := filepath.Join("spool", "2026-11-13 18.30.01")
	r := delivery.Report{File: first, Folder: filepath.Join("folders", "On the Record"), Path: filepath.Join(rec, first),
		Err: fmt.Errorf("the copy under the hidden name is gone: %w", delivery.ErrStuck)}
	want := "Handing “" + first + "” to “On the Record” stopped: " + r.Err.Error() + ". It is safe in " + rec +
		", and stays there: On Air can't carry this hand-over on by itself."
	if got := word(r, filepath.Join("folders", "Elsewhere")); got != want {
		t.Fatalf("a stuck hand-over says %q", got)
	}
	r.Err = errors.New("permission denied")
	if got := word(r, filepath.Join("folders", "Elsewhere")); !strings.HasSuffix(got, "It is safe in "+rec+", and On Air carries on at the next launch and when a folder is picked.") {
		t.Fatalf("a hand-over that stopped says %q", got)
	}
}

// On Air's own copy goes three days after the folder was found to hold the
// recording, on the clock, and the pieces stay.
func TestOnAirsOwnCopyGoesThreeDaysAfterTheHandOver(t *testing.T) {
	f := newFixture(t)
	folder := f.picks()
	f.record(f.app, "one|")
	f.app.Menu(Stop)
	f.app.Event(Finished, "")
	f.idle(f.app)
	rec := f.recording()
	own := filepath.Join(rec, first)
	shown := f.edge.latest()
	f.at = f.at.Add(72*time.Hour - time.Minute)
	f.app.Tidy()
	f.idle(f.app)
	if _, err := os.Stat(own); err != nil {
		t.Fatalf("On Air's own copy went early: %v", err)
	}
	f.at = f.at.Add(time.Minute)
	f.app.Tidy()
	f.idle(f.app)
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Fatalf("On Air's own copy is still there: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(folder, first)); string(got) != "OPEN|one|" {
		t.Fatalf("the recording holds %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(rec, "piece-000001.mp4")); string(got) != "OPEN|one|" {
		t.Fatalf("the piece holds %q", got)
	}
	if s := f.edge.latest(); s.last != shown.last || s.note != shown.note {
		t.Fatalf("removing On Air's own copy changed the menu: %+v", s)
	}

	// A launch removes it too.
	g := newFixture(t)
	g.record(g.app, "one|")
	g.app.Menu(Stop)
	g.app.Event(Finished, "")
	g.idle(g.app)
	g.at = g.at.Add(72 * time.Hour)
	g.app = g.launch()
	if _, err := os.Stat(filepath.Join(g.recording(), first)); !os.IsNotExist(err) {
		t.Fatalf("On Air's own copy is still there: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(g.out, first)); string(got) != "OPEN|one|" {
		t.Fatalf("the recording holds %q", got)
	}
}

// A recording joined before hand-overs existed is left as it is: its file
// is in On Air's own folder already, and nothing of it is removed.
func TestARecordingFromBeforeHandOversIsLeftAlone(t *testing.T) {
	f := newFixture(t)
	r, err := spool.Create(f.spool, "2026-11-12 09.00.00")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.PutOpening([]byte("OPEN|")); err != nil {
		t.Fatal(err)
	}
	if err := r.PutPiece(1, []byte("old|")); err != nil {
		t.Fatal(err)
	}
	joined, err := r.Join(filepath.Join(f.out, "2026-11-12 09.00.00 On Air.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.MarkDone(joined.Path); err != nil {
		t.Fatal(err)
	}
	f.picks()
	f.at = f.at.Add(30 * 24 * time.Hour)
	f.app = f.launch()
	f.app.Tidy()
	f.idle(f.app)
	if got, _ := os.ReadFile(joined.Path); string(got) != "OPEN|old|" {
		t.Fatalf("the recording holds %q", got)
	}
	if names := list(t, r.Dir); len(names) != 3 {
		t.Fatalf("its pieces: %v", names)
	}
	if names := list(t, filepath.Join(filepath.Dir(f.out), "On the Record")); len(names) != 0 {
		t.Fatalf("On the Record's folder holds %v", names)
	}
}

// Quit while recording hands the recording over before On Air quits, and
// nothing new starts meanwhile.
func TestQuitHandsTheRecordingOverFirst(t *testing.T) {
	f := newFixture(t)
	folder := f.picks()
	f.record(f.app, "one|")
	f.app.Menu(Quit)
	f.app.Event(Finished, "")
	f.idle(f.app)
	for i := 0; i < 2000; i++ {
		if _, _, quits := f.edge.counts(); quits == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if starts, _, quits := f.edge.counts(); starts != 1 || quits != 1 {
		t.Fatalf("%d starts, %d quits", starts, quits)
	}
	if got, _ := os.ReadFile(filepath.Join(folder, first)); string(got) != "OPEN|one|" {
		t.Fatalf("the recording holds %q", got)
	}
	// Once the recording is joined, a Start while it is handed over before
	// On Air quits is refused.
	g := newFixture(t)
	g.app.mu.Lock()
	g.app.quitting = true
	g.app.mu.Unlock()
	g.app.Menu(Start)
	if starts, _, _ := g.edge.counts(); starts != 0 || g.edge.latest().note != "On Air quits once the last recording is handed over." {
		t.Fatalf("a Start while quitting: %d starts, %+v", starts, g.edge.latest())
	}
}
