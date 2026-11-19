//go:build darwin

package mac

import (
	"math"
	"strings"
	"testing"
)

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
// its question, its word on it, and then its answer and sources, or its
// lines. A word that introduces the lines, as "No direct answer; the
// closest notes:" does, comes before them.
func TestACardsWordComesBeforeTheLinesItIntroduces(t *testing.T) {
	rows := answerRows(AnswerLine{Text: "Differs from the record: said “a”; the record says “b”", Where: "Weekly · 2026-10-21 · 8:40", URL: "https://archive.example/m/c?t=520"}, []Answer{
		{Question: "How many sign ups so far?", Note: "No direct answer; the closest notes:",
			Lines: []AnswerLine{{Text: "The waitlist reached 2,300 sign ups", Where: "Weekly · 2026-10-21 · 8:40", URL: "https://archive.example/m/c?t=520"}}},
		{Question: "How much is the round?", Text: "They hope to close a $750k pre-seed round [1].",
			Sources: []AnswerLine{{Text: "[1] Partner sync · 2026-09-14 · 6:12", URL: "https://archive.example/m/a?t=372"}}},
		{Question: "When do doors open?", Note: "Looking…"},
	})
	var got []string
	for _, r := range rows {
		got = append(got, r.kind+": "+r.text)
	}
	want := []string{
		"differs: Differs from the record: said “a”; the record says “b”",
		"question: How many sign ups so far?",
		"note: No direct answer; the closest notes:",
		"line: The waitlist reached 2,300 sign ups",
		"question: How much is the round?",
		"answer: They hope to close a $750k pre-seed round [1].",
		"source: [1] Partner sync · 2026-09-14 · 6:12",
		"question: When do doors open?",
		"note: Looking…",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the panel's rows are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if r := rows[3]; r.where != "Weekly · 2026-10-21 · 8:40" || r.link != "https://archive.example/m/c?t=520" {
		t.Fatalf("the line's row is %+v", r)
	}
}
