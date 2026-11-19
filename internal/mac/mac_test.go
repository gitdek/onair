//go:build darwin

package mac

import (
	"math"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The main goroutine keeps the main thread, as On Air's does, and runs
// what the Mac's code hands the main queue while the tests run.
func init() { runtime.LockOSThread() }

func TestMain(m *testing.M) {
	code := make(chan int, 1)
	go func() { code <- m.Run() }()
	for {
		select {
		case c := <-code:
			os.Exit(c)
		default:
			runMain(0.01)
		}
	}
}

// The picture keeps its content's shape, at most the rows asked for, with
// even sides.
func TestThePictureKeepsItsShape(t *testing.T) {
	for _, c := range []struct {
		width, height, scale float64
		rows                 int
		wide, high           int
	}{
		{1512, 982, 2, 1080, 1662, 1080}, // a laptop's display, brought down
		{1280, 720, 2, 1080, 1920, 1080}, // an area of it
		{641, 361, 1, 1080, 640, 360},    // small enough already, made even
		{800, 600, 2, 1080, 1440, 1080},  // a window
		{2560, 1440, 1, 0, 2560, 1440},   // no limit asked for
		{1, 1, 1, 1080, 2, 2},            // never no picture at all
		{300, 200, 0, 1080, 300, 200},    // no scale is one
	} {
		if wide, high := frameSize(c.width, c.height, c.scale, c.rows); wide != c.wide || high != c.high {
			t.Errorf("%v × %v at %v, %d rows: %d × %d, want %d × %d", c.width, c.height, c.scale, c.rows, wide, high, c.wide, c.high)
		}
	}
}

// The live side adds the call's sound and the microphone into one for
// speech recognition, each placed by its stamps, a tenth of a second at a
// time, and measures how loud each was in each step before adding them.
func TestTheLiveMixAddsBothSoundsAndMeasuresEach(t *testing.T) {
	const rate, step = 16000, 1600
	// Each sound's loudness at a frame of the timeline. The call talks for
	// 0.3 s, then is silent. The microphone hisses, is loud enough with the
	// call to pass full scale, talks, sends nothing from 0.6 s to 1 s, and
	// talks again.
	call := func(f int) float32 {
		if f < 4800 {
			return 0.2
		}
		return 0
	}
	mic := func(f int) float32 {
		switch {
		case f < 3200:
			return 0.01
		case f < 4800:
			return 0.9
		}
		return 0.3
	}
	// A square wave at that loudness, whose RMS is the loudness, in step
	// with the other sound's.
	wave := func(level func(int) float32, at, n int) []float32 {
		frames := make([]float32, n)
		for i := range frames {
			if frames[i] = level(at + i); (at+i)%2 == 1 {
				frames[i] = -frames[i]
			}
		}
		return frames
	}
	// The call comes in buffers of 20 ms, the microphone in 10 ms ones
	// whose stamps wander a few frames, which places them nowhere new.
	var buffers []mixBuffer
	for at := 0; at < 19200; at += 320 {
		buffers = append(buffers, mixBuffer{0, at, wave(call, at, 320)})
		for m := at; m < at+320; m += 160 {
			if m < 9600 || m >= 16000 {
				buffers = append(buffers, mixBuffer{1, m + []int{0, 2, -3, 1}[m/160%4], wave(mic, m, 160)})
			}
		}
	}
	mix, loud, late, early := speechMix(rate, buffers, 27200+rate/2)
	if late != 0 || early != 0 || len(loud) != 17 || len(mix) != 17*step {
		t.Fatalf("%d steps, %d frames, %d late and %d early: want 17 steps, none left out", len(loud), len(mix), late, early)
	}
	for n, l := range loud {
		var c, m float32
		if n < 3 {
			c = 0.2
		}
		switch {
		case n < 2:
			m = 0.01
		case n == 2:
			m = 0.9
		case n < 6 || n == 10 || n == 11:
			m = 0.3
		}
		if math.Abs(float64(l[0]-c)) > 1e-6 || math.Abs(float64(l[1]-m)) > 1e-6 {
			t.Errorf("step %d: the call at %v and the microphone at %v, want %v and %v", n, l[0], l[1], c, m)
		}
		want := min(c+m, 1)
		for i := n * step; i < (n+1)*step; i++ {
			if w := map[bool]float32{true: want, false: -want}[i%2 == 0]; math.Abs(float64(mix[i]-w)) > 1e-6 {
				t.Fatalf("frame %d of step %d is %v, want %v", i, n, mix[i], w)
			}
		}
	}
	// A buffer stamped behind what was handed on, and one too far ahead.
	_, _, late, early = speechMix(rate, []mixBuffer{{0, 0, wave(call, 0, step)}, {1, 0, wave(mic, 0, step)}, {0, 0, make([]float32, 320)}, {1, step + 4*rate + 10, make([]float32, 100)}}, -1)
	if late != 320 || early != 100 {
		t.Fatalf("%d frames late and %d early, want 320 and 100", late, early)
	}
}

// The panel shows a difference from the record first, and then each card:
// its title, its word on it, and then each line followed by its sources. A
// word that introduces the lines comes before them.
func TestACardsWordComesBeforeTheLinesItIntroduces(t *testing.T) {
	rows := cardRows(AnswerLine{Text: "Differs from the record: said “a”; the record says “b”", Where: "Weekly · 2026-10-21 · 8:40", URL: "https://archive.example/m/c?t=520"}, []Card{
		{Title: "Halcyon", Note: "From the board:", Lines: []CardLine{
			{Text: "Priya to send the press kit", Sources: []Source{{Label: "Oct 21", URL: "https://archive.example/m/c?t=520"}}},
			{Text: "They hope to close a $750k pre-seed round", Sources: []Source{{Label: "Sep 14", URL: "https://archive.example/m/a?t=372"}, {Label: "Oct 8", URL: "https://archive.example/m/b"}}},
		}},
		{Title: "When do doors open?", Lines: []CardLine{{Text: "On the 9th"}}},
	})
	var got []string
	for _, r := range rows {
		got = append(got, r.kind+": "+r.text)
	}
	want := []string{
		"differs: Differs from the record: said “a”; the record says “b”",
		"title: Halcyon",
		"note: From the board:",
		"line: Priya to send the press kit",
		"source: Oct 21",
		"line: They hope to close a $750k pre-seed round",
		"source: Sep 14",
		"source: Oct 8",
		"title: When do doors open?",
		"line: On the 9th",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the panel's rows are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if r := rows[0]; r.where != "Weekly · 2026-10-21 · 8:40" || r.link != "https://archive.example/m/c?t=520" {
		t.Fatalf("the difference's row is %+v", r)
	}
	if r := rows[4]; r.link != "https://archive.example/m/c?t=520" {
		t.Fatalf("the source's row is %+v", r)
	}
}

// Go asks the panel to show something from threads of its own, which have
// no autorelease pool, and the panel changes many times a meeting. What
// each change is made of must be let go once the panel has shown it, so
// five thousand changes leave malloc holding no more than before.
func TestThePanelLetsGoOfWhatItShowed(t *testing.T) {
	long := strings.Repeat("a line of the notes, ", 40)
	show := func() {
		ShowPanel(false, "display", long, []string{long, long}, AnswerLine{Text: long, Where: long, URL: "https://archive.example/m/a"},
			[]Card{{Title: long, Note: long, Lines: []CardLine{{Text: long, Sources: []Source{{Label: "Nov 3", URL: "https://archive.example/m/b"}}}}},
				{Title: long, Lines: []CardLine{{Text: long, Sources: []Source{{Label: long, URL: "https://archive.example/m/c"}}}, {Text: long}}}})
	}
	grew := make(chan int64)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		show()
		waitMain()
		before := mallocInUse()
		for range 5000 {
			show()
		}
		waitMain()
		// What the main queue let go of may take a moment to be freed.
		time.Sleep(100 * time.Millisecond)
		grew <- mallocInUse() - before
	}()
	if n := <-grew; n > 1<<20 {
		t.Fatalf("after five thousand changes of the panel, malloc holds %d KB more", n>>10)
	}
}

// The recorder tells the menu what to say from threads of its own, which
// have no autorelease pool, at each change and each piece of a recording.
// What each call is made of must be let go once the menu has it, so five
// thousand calls leave malloc holding no more than before.
func TestTheMenuLetsGoOfWhatItShowed(t *testing.T) {
	long := strings.Repeat("a note about the recording, ", 30)
	displays := []Choice{{ID: "display one", Title: long, Picked: true}, {ID: "display two", Title: long}}
	tell := func() {
		Show(true, long, long)
		Choices(true, displays, long, long, long, true, "local")
	}
	grew := make(chan int64)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		tell()
		waitMain()
		before := mallocInUse()
		for range 5000 {
			tell()
		}
		waitMain()
		// What the main queue let go of may take a moment to be freed.
		time.Sleep(100 * time.Millisecond)
		grew <- mallocInUse() - before
	}()
	if n := <-grew; n > 1<<20 {
		t.Fatalf("after five thousand calls to the menu, malloc holds %d KB more", n>>10)
	}
}

// The panel lays out a difference from the record and its cards, each
// line's sources a chip a click opens, and grows with each card.
func TestThePanelLaysOutItsCardsWithASourceChipEach(t *testing.T) {
	differs := AnswerLine{Text: "Differs from the record: said “a”; the record says “b”", Where: "Weekly · 2026-10-21 · 8:40", URL: "https://archive.example/m/c?t=520"}
	card := Card{Title: "Halcyon", Lines: []CardLine{
		{Text: "They hope to close a $750k pre-seed round", Sources: []Source{{Label: "Sep 14", URL: "https://archive.example/m/a?t=372"}, {Label: "Oct 8", URL: "https://archive.example/m/b"}}},
		{Text: "Priya owes the press kit, with no date set", Sources: []Source{{Label: "Oct 21", URL: "https://archive.example/m/c?t=520"}}},
	}}
	done := make(chan struct{})
	var one, two float64
	var clickable int
	go func() {
		defer close(done)
		one, _ = panelLayout(differs, []Card{card})
		two, clickable = panelLayout(differs, []Card{card, card})
	}()
	<-done
	if one < 120 || two <= one+40 {
		t.Errorf("one card lays out %.0f points tall, two %.0f", one, two)
	}
	// The difference, and three chips on each card.
	if clickable != 1+3+3 {
		t.Errorf("%d parts a click opens", clickable)
	}
}
