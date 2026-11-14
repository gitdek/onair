package recorder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Live Context is off until it is turned on from the menu, and the choice
// is remembered beside the others, whichever is changed last.
func TestLiveContextIsOffUntilTurnedOnAndRemembered(t *testing.T) {
	f := newFixture(t)
	f.connected(builtIn, wide)
	if f.edge.latestChoices().Live {
		t.Fatal("Live Context is on before it was turned on")
	}
	f.app.Menu(ToggleLive)
	if !f.edge.latestChoices().Live {
		t.Fatal("the menu doesn't mark Live Context once it is turned on")
	}
	// Picking a display and a folder afterwards keeps it, and it keeps
	// them.
	f.app.PickDisplay("B")
	f.app.FolderChosen(t.TempDir())
	again := f.newApp()
	again.Displays([]Display{builtIn, wide})
	if c := f.edge.latestChoices(); !c.Live || len(picked_(c)) != 1 || picked_(c)[0] != "B" || c.Folder == ownFolder {
		t.Fatalf("after a relaunch the menu shows %+v", c)
	}
	again.Menu(ToggleLive)
	if f.edge.latestChoices().Live {
		t.Fatal("the menu marks Live Context once it is turned off")
	}
	third := f.newApp()
	third.Displays([]Display{builtIn, wide})
	if c := f.edge.latestChoices(); c.Live || picked_(c)[0] != "B" || c.Folder == ownFolder {
		t.Fatalf("turned off and relaunched, the menu shows %+v", c)
	}
	b, err := os.ReadFile(f.settings)
	if err != nil || strings.Contains(string(b), "live") {
		t.Fatalf("settings.json holds %s, %v", b, err)
	}
}

// While Live Context is on, capture leaves On Air's own windows out of a
// display's picture, so the panel isn't recorded. Turning it on or off says
// nothing in the menu, and changes nothing about a recording that runs.
func TestLiveContextLeavesOnAirsWindowsOutOfTheRecording(t *testing.T) {
	f := withMicrophone(t)
	f.connected(builtIn)
	f.started()
	if f.edge.lastAsked().LeaveOutOwnWindows {
		t.Fatal("capture leaves On Air's windows out with Live Context off")
	}
	before := f.edge.latest()
	f.app.Menu(ToggleLive)
	if s := f.edge.latest(); s != before {
		t.Fatalf("turning Live Context on while recording shows %+v, not %+v", s, before)
	}
	if starts, stops, _ := f.edge.counts(); starts != 1 || stops != 0 {
		t.Fatalf("turning Live Context on started %d and stopped %d", starts, stops)
	}
	f.stopped()
	f.started()
	if c := f.edge.lastAsked(); !c.LeaveOutOwnWindows || !c.Microphone || c.Target != (Target{}) {
		t.Fatalf("with Live Context on, capture was asked for %+v", c)
	}
	f.stopped()
}

// Live Context that can't be remembered still turns on, and says so.
func TestLiveContextThatCantBeRememberedIsSaid(t *testing.T) {
	f := newFixture(t)
	f.settings = filepath.Join(f.out, "in", "the", "way")
	if err := os.WriteFile(f.out, []byte("a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.app = f.newApp()
	f.app.Menu(ToggleLive)
	if s := f.edge.latest(); !f.edge.latestChoices().Live || !strings.HasPrefix(s.note, "On Air keeps Live Context as it is until it quits, but couldn't remember it") {
		t.Fatalf("settings that can't be written show %+v", s)
	}
}
