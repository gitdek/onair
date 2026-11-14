package live

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gitdek/onair/internal/recorder"
)

// edgeLog is the Mac as the recorder sees it, writing down every call the
// recorder makes of it, in order.
type edgeLog struct {
	mu    sync.Mutex
	dir   string // written as <dir>, so two runs compare
	calls []string
}

func (e *edgeLog) add(format string, args ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, strings.ReplaceAll(fmt.Sprintf(format, args...), e.dir, "<dir>"))
}
func (e *edgeLog) AskMicrophone()          { e.add("AskMicrophone") }
func (e *edgeLog) Start(c recorder.Config) { e.add("Start %+v", c) }
func (e *edgeLog) Stop()                   { e.add("Stop") }
func (e *edgeLog) Show(recording bool, last, note string) {
	e.add("Show %v %q %q", recording, last, note)
}
func (e *edgeLog) Choices(c recorder.Choices)  { e.add("Choices %+v", c) }
func (e *edgeLog) SelectArea(display string)   { e.add("SelectArea %s", display) }
func (e *edgeLog) SelectWindow()               { e.add("SelectWindow") }
func (e *edgeLog) ChooseFolder(current string) { e.add("ChooseFolder %s", current) }
func (e *edgeLog) Reveal(path string)          { e.add("Reveal %s", path) }
func (e *edgeLog) Quit()                       { e.add("Quit") }

func (e *edgeLog) said() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.calls...)
}

// session records a meeting with Live Context on, turned off and on again
// halfway, with the live side wrapped around the recorder's edge by wrap
// and failing in its own way at two points, by trouble. It returns every
// call the recorder made of the Mac, and the files it left.
func session(t *testing.T, wrap func(recorder.Edge) recorder.Edge, trouble func()) ([]string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	log := &edgeLog{dir: dir}
	var mu sync.Mutex
	clock := time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)
	now := func() time.Time { mu.Lock(); defer mu.Unlock(); clock = clock.Add(time.Second); return clock }
	app := recorder.New(wrap(log), recorder.Config{PieceSeconds: 4, Microphone: true}, filepath.Join(dir, "spool"), filepath.Join(dir, "out"), filepath.Join(dir, "settings.json"), now)
	// Every call the recorder takes returns at once, whatever the live
	// side is doing.
	do := func(what string, call func()) {
		start := time.Now()
		call()
		if took := time.Since(start); took > time.Second {
			t.Fatalf("%s took %v", what, took)
		}
	}
	do("Displays", func() {
		app.Displays([]recorder.Display{{ID: "d1", Name: "Built-in", Width: 1512, Height: 982, Main: true}})
	})
	do("Show Live Context", func() { app.Menu(recorder.ToggleLive) })
	do("Start", func() { app.Menu(recorder.Start) })
	do("the microphone allowed", func() { app.Event(recorder.MicrophoneAllowed, "") })
	do("Started", func() { app.Event(recorder.Started, "") })
	do("the opening segment", func() { app.Segment(true, []byte("OPEN|")) })
	trouble()
	do("a segment", func() { app.Segment(false, []byte("one|")) })
	do("Show Live Context, off", func() { app.Menu(recorder.ToggleLive) })
	do("Show Live Context, on", func() { app.Menu(recorder.ToggleLive) })
	trouble()
	do("a segment", func() { app.Segment(false, []byte("two|")) })
	do("Stop", func() { app.Menu(recorder.Stop) })
	do("the last segment", func() { app.Segment(false, []byte("three|")) })
	do("Finished", func() { app.Event(recorder.Finished, "") })
	// The join and the hand-over run on the recorder's own goroutine: wait
	// until they are done and nothing more is said.
	waitFor(t, "the hand-over", func() bool {
		calls := log.said()
		return len(calls) > 0 && strings.Contains(strings.Join(calls, "\n"), "Handed")
	})
	for n := -1; n != len(log.said()); {
		n = len(log.said())
		time.Sleep(100 * time.Millisecond)
	}
	files := map[string]string{}
	for _, sub := range []string{"out", "."} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Type().IsRegular() {
				b, err := os.ReadFile(filepath.Join(dir, sub, e.Name()))
				if err != nil {
					t.Fatal(err)
				}
				files[filepath.Join(sub, e.Name())] = string(b)
			}
		}
	}
	return log.said(), files
}

// TestTheRecorderIsTheSameWhateverTheLiveSideDoes records the same meeting
// with no live side, and then with the live side failing in each way it
// can: On the Record down, slow, never answering, answering nonsense or
// panicking; the panel and recognition panicking or hanging; speech
// recognition refused, unsupported or failing, sound dropped, and a flood
// of words. Every call the recorder makes of the Mac, in order, and every
// file it leaves are the same each time, and no call it takes waits.
func TestTheRecorderIsTheSameWhateverTheLiveSideDoes(t *testing.T) {
	never := make(chan struct{})
	defer close(never)
	want, wantFiles := session(t, func(e recorder.Edge) recorder.Edge { return e }, func() {})
	if wantFiles["out/2026-11-14 15.00.01 On Air.mp4"] != "OPEN|one|two|three|" || !strings.Contains(wantFiles["settings.json"], `"live": true`) {
		t.Fatalf("the recording without the live side left %v", wantFiles)
	}
	nonsense := Found{Semantic: true, StrongMeetings: []string{"x", ""}, StrongItems: []string{"y"},
		Meetings: []Meeting{{ID: "x", Title: strings.Repeat("t", 1<<20), URL: "javascript:void(0)", Passages: []Passage{{}}}, {}},
		Items:    []Item{{ID: "y", Status: "open", URL: "http://[::1]:1/" + strings.Repeat("y", 1<<16)}}, Notes: []Note{{ID: "z", Why: []string{"close in meaning (NaN)"}}}}
	answers := map[string]func(context.Context, string) (Found, error){
		"down": func(context.Context, string) (Found, error) {
			return Found{}, fmt.Errorf("On the Record isn't answering on this Mac")
		},
		"slow":     func(ctx context.Context, _ string) (Found, error) { <-ctx.Done(); return Found{}, ctx.Err() },
		"never":    func(context.Context, string) (Found, error) { <-never; return Found{}, nil },
		"nonsense": func(context.Context, string) (Found, error) { return nonsense, nil },
		"panics":   func(context.Context, string) (Found, error) { panic("the archive broke") },
	}
	words := func(c *Controller) func() {
		return func() {
			for i := 0; i < 2000; i++ {
				c.Heard(Heard{Speaker: Speaker(i % 4), Text: fmt.Sprintf("word %d and more words", i), From: time.Now(), To: time.Now()})
			}
			for _, p := range []Problem{SpeechRefused, SpeechUnsupported, SpeechFailed, SoundDropped, SpeechAllowed, 99} {
				c.Report(p, strings.Repeat("why ", 1000))
			}
			time.Sleep(60 * time.Millisecond)
		}
	}
	type failure struct {
		answer func(context.Context, string) (Found, error)
		screen *fakeScreen
	}
	failures := map[string]failure{
		"the panel panics":     {answer: answers["nonsense"], screen: &fakeScreen{broken: true}},
		"the panel hangs":      {answer: answers["nonsense"], screen: &fakeScreen{hang: never}},
		"everything at once":   {answer: answers["panics"], screen: &fakeScreen{broken: true}},
		"recognition is fine":  {answer: func(context.Context, string) (Found, error) { return strong("m1", "m2"), nil }, screen: &fakeScreen{}},
		"no archive is set up": {answer: nil, screen: &fakeScreen{}},
	}
	for name, answer := range answers {
		failures["On the Record "+name] = failure{answer: answer, screen: &fakeScreen{}}
	}
	for name, f := range failures {
		t.Run(name, func(t *testing.T) {
			var archive Archive
			if f.answer != nil {
				archive = &fakeArchive{answer: f.answer}
			}
			c := Start(fast, archive, f.screen, time.Now)
			got, files := session(t, func(e recorder.Edge) recorder.Edge { return Tap(e, c) }, words(c))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("the recorder did something else with the live side:\n%s\n\nwithout it:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
			if !reflect.DeepEqual(files, wantFiles) {
				t.Fatalf("the recorder left %v with the live side, and %v without it", files, wantFiles)
			}
		})
	}
}

// TestTheTapTellsTheLiveSideWhatTheRecorderDoes checks the other half:
// with the live side listening in, it learns that Live Context is on, what
// capture records, and when a recording runs.
func TestTheTapTellsTheLiveSideWhatTheRecorderDoes(t *testing.T) {
	screen := &fakeScreen{}
	c := Start(fast, &fakeArchive{answer: func(context.Context, string) (Found, error) { return Found{}, nil }}, screen, time.Now)
	dir := t.TempDir()
	app := recorder.New(Tap(&edgeLog{dir: dir}, c), recorder.Config{PieceSeconds: 4}, filepath.Join(dir, "spool"), filepath.Join(dir, "out"), filepath.Join(dir, "settings.json"), time.Now)
	app.Displays([]recorder.Display{{ID: "d1", Name: "Built-in", Width: 1512, Height: 982, Main: true}, {ID: "d2", Name: "Studio", Width: 2560, Height: 1440}})
	app.PickDisplay("d2")
	app.Menu(recorder.ToggleLive)
	waitFor(t, "the question about speech recognition", func() bool { asks, _ := screen.said(); return asks == 1 })
	app.Menu(recorder.Start)
	app.Event(recorder.Started, "")
	waitFor(t, "listening", func() bool { _, l := screen.said(); return len(l) == 1 && l[0] })
	if p := screen.last(); !p.Visible || p.Display != "d2" || p.Status != "Listening…" {
		t.Fatalf("while recording the panel shows %+v", p)
	}
	// Recording runs until capture has finished, after Stop.
	app.Menu(recorder.Stop)
	app.Event(recorder.Finished, "")
	waitFor(t, "listening to stop", func() bool { _, l := screen.said(); return len(l) == 2 && !l[1] })
}
