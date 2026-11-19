package live

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

// A result over a second of sound, in steps of 100 ms: the person talks,
// then the call, then both at once, then the call goes silent while the
// microphone hisses.
var second = Said{
	Began: t0,
	Step:  ms(100),
	Mic:   []float64{0.3, 0.3, 0.3, 0.02, 0.02, 0.02, 0.1, 0.1, 0.001, 0.001},
	Call:  []float64{0.01, 0.01, 0.01, 0.2, 0.2, 0.2, 0.1, 0.1, 0, 0},
}

func TestEachWordIsWhoseSoundWasClearlyLouder(t *testing.T) {
	s := second
	s.Segments = []Segment{
		{Text: "hello", At: ms(50), Length: ms(200)},
		{Text: "there", At: ms(250)}, // no length: the step it starts in
		{Text: "how", At: ms(300), Length: ms(200)},
		{Text: "are", At: ms(500), Length: ms(100)},
		{Text: "you", At: ms(600), Length: ms(200)},  // as loud as each other
		{Text: "fine", At: ms(850), Length: ms(100)}, // the call silent
		{Text: "later", At: ms(1200), Length: ms(100)},
	}
	want := []Heard{
		{Speaker: Person, Text: "hello there", From: t0.Add(ms(50)), To: t0.Add(ms(250))},
		{Speaker: Others, Text: "how are you", From: t0.Add(ms(300)), To: t0.Add(ms(800))},
		{Speaker: Person, Text: "fine", From: t0.Add(ms(850)), To: t0.Add(ms(950))},
		{Speaker: Others, Text: "later", From: t0.Add(ms(1200)), To: t0.Add(ms(1300))},
	}
	if got := s.heard(); !reflect.DeepEqual(got, want) {
		t.Fatalf("heard %+v\nwant %+v", got, want)
	}
}

func TestAWordIsEveryoneElsesUnlessTheMicrophoneIsClearlyLouder(t *testing.T) {
	for _, c := range []struct {
		name      string
		mic, call float64
		want      Speaker
	}{
		{"both silent", 0, 0, Others},
		{"a tie", 0.1, 0.1, Others},
		{"louder, but not twice", 0.19, 0.1, Others},
		{"more than twice", 0.21, 0.1, Person},
		{"the call silent", 0.0001, 0, Person},
		{"the call louder", 0.05, 0.2, Others},
	} {
		s := Said{Step: ms(100), Mic: []float64{c.mic, c.mic}, Call: []float64{c.call, c.call}}
		if got := s.whose(ms(20), ms(180)); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// A stretch outside what was measured, or with no steps at all.
	s := Said{Step: ms(100), Mic: []float64{1}, Call: []float64{0}}
	if s.whose(ms(-300), ms(-100)) != Others || s.whose(ms(100), ms(200)) != Others || (Said{Mic: []float64{1}, Call: []float64{0}}).whose(0, ms(50)) != Others {
		t.Fatal("a stretch outside the loudness was taken as the person's")
	}
	// A stretch that starts before the sound did takes the steps it reaches.
	if s.whose(ms(-50), ms(50)) != Person {
		t.Fatal("a stretch reaching into the first step wasn't the person's")
	}
}

// Recognition sometimes gives no times at all. Then the whole result goes
// by all its sound.
func TestAResultWithNoTimesGoesByAllItsSound(t *testing.T) {
	s := second
	s.Mic = []float64{0.3, 0.3, 0.3, 0.3}
	s.Call = []float64{0, 0.1, 0, 0}
	s.Segments = []Segment{{Text: " the  launch "}, {Text: ""}, {Text: "date"}}
	want := []Heard{{Speaker: Person, Text: "the launch date", From: t0, To: t0.Add(ms(400))}}
	if got := s.heard(); !reflect.DeepEqual(got, want) {
		t.Fatalf("heard %+v, want %+v", got, want)
	}
	s.Call = []float64{0.3, 0.3, 0.3, 0.3}
	if got := s.heard(); len(got) != 1 || got[0].Speaker != Others {
		t.Fatalf("with the call as loud, heard %+v", got)
	}
}

func TestTheLogCountsAResultWithoutItsWords(t *testing.T) {
	s := second
	s.Segments = []Segment{{Text: "hello", At: ms(100), Length: ms(200)}, {Text: "how", At: ms(300), Length: ms(200)}, {Text: "now", At: ms(500)}}
	got := s.counts(s.heard())
	want := "said 3 words in 3 segments (1 with no length), from 0.1 s to 0.5 s of 1.0 s measured: 2 turns, 1 words yours and 2 others; " +
		"the microphone clearly louder in 5 of 10 steps, at -15 dBFS on average, the call's sound at -19 dBFS"
	if got != want {
		t.Fatalf("logged %q\nwant %q", got, want)
	}
}

func TestTheControllerHearsEachTurnOfAResult(t *testing.T) {
	screen := &fakeScreen{}
	archive := &fakeArchive{answer: func(context.Context, string) (Found, error) { return Found{}, nil }}
	c := Start(fast, archive, screen, time.Now, nil, nil)
	c.Enabled(true)
	c.Capturing("", true)
	c.Recording(true)
	waitFor(t, "listening", func() bool { _, l := screen.said(); return len(l) == 1 && l[0] })
	s := second
	s.Began = time.Now().Add(-time.Second)
	s.Segments = []Segment{{Text: "hello", At: ms(50), Length: ms(200)}, {Text: "how are you", At: ms(300), Length: ms(400)}}
	c.Said(s)
	c.Said(Said{Segments: []Segment{{Text: "no loudness"}}, Mic: []float64{1}}) // mismatched series: taken as everyone else's
	waitFor(t, "the words", func() bool {
		return strings.HasPrefix(screen.last().Status, "Heard 6 words in the last minute, 1 of them yours.")
	})
	waitFor(t, "a question in the order they were said", func() bool {
		q := archive.questions()
		return len(q) > 0 && q[len(q)-1] == "hello\nhow are you\nno loudness"
	})
}
