package live

import (
	"math"
	"strings"
	"testing"
	"time"
)

// word is a word of a made-up result: its text, and when it starts and
// ends, in seconds from the start of its request's sound.
type word struct {
	text     string
	from, to float64
}

// result is a made-up result of request req, partial or final, whose
// sound began at began and has run for sound seconds, with words timed as
// given.
func result(req int, partial bool, began time.Time, sound float64, ws ...word) Said {
	steps := int(math.Round(sound * 10))
	s := Said{Began: began, Request: req, Partial: partial, Step: 100 * time.Millisecond, Mic: make([]float64, steps), Call: make([]float64, steps)}
	for _, w := range ws {
		s.Segments = append(s.Segments, Segment{Text: w.text, At: seconds(w.from), Length: seconds(w.to - w.from)})
	}
	return s
}

// spoken is words said one after another, from a start, each lasting a
// quarter of a second with a tenth between them.
func spoken(from float64, text string) []word {
	var out []word
	for _, w := range strings.Fields(text) {
		out = append(out, word{w, from, from + 0.25})
		from += 0.35
	}
	return out
}

func texts(us []utterance) string {
	var out []string
	for _, u := range us {
		out = append(out, u.text)
	}
	return strings.Join(out, " | ")
}

func TestAQuestionIsFinishedOnceSettleAfterItsMark(t *testing.T) {
	var st stream
	t0 := time.Date(2026, 11, 15, 15, 0, 0, 0, time.UTC)
	q := spoken(1, "how many mockups did Priya share?") // ends at 3.0 s
	// Recognition hears each word about three tenths of a second after it
	// ends.
	for i := range q {
		if got := st.take(result(1, true, t0, q[i].to+0.3, q[:i+1]...)); got != nil {
			t.Fatalf("finished %q before the question was", texts(got))
		}
	}
	if g, ok := st.growing(); !ok || g.text != "how many mockups did Priya share?" {
		t.Fatalf("growing %q", g.text)
	}
	// Settle of sound after the mark came, with nothing new: finished.
	if got := st.take(result(1, true, t0, 3.6, q...)); got != nil {
		t.Fatalf("finished %q too soon", texts(got))
	}
	got := st.take(result(1, true, t0, 3.75, q...))
	if len(got) != 1 || got[0].text != "how many mockups did Priya share?" || got[0].ended != byMark || !got[0].to.Equal(t0.Add(seconds(3))) {
		t.Fatalf("finished %+v", got)
	}
	// However later results revise it, it isn't handed on again: only
	// what follows it is.
	revised := append(spoken(1, "how many mock-ups did Priya share?"), spoken(4, "she said nine.")...)
	if got := st.take(result(1, true, t0, 5.4, revised...)); got != nil {
		t.Fatalf("handed on %q", texts(got))
	}
	got = st.take(result(1, false, t0, 6, revised...))
	if len(got) != 1 || got[0].text != "she said nine." {
		t.Fatalf("the final handed on %q", texts(got))
	}
	if st.take(result(1, true, t0, 6.5, revised...)) != nil {
		t.Fatal("a result after the final was taken")
	}
	if r := st.read(); len(r) != 1 || r[0].early != 1 || r[0].atFinal != 1 || r[0].byMark != 2 || r[0].timed != 9 || r[0].behinds == 0 {
		t.Fatalf("counted %+v", r)
	}
}

func TestASentenceWithNoMarkIsFinishedAfterAPause(t *testing.T) {
	var st stream
	t0 := time.Date(2026, 11, 15, 15, 0, 0, 0, time.UTC)
	q := spoken(0.5, "what features did Rafael want in the new version") // ends at 3.55 s
	st.take(result(1, true, t0, 3.6, q...))
	// The pause rule: Pause after its last word, and Settle more.
	if got := st.take(result(1, true, t0, 4.6, q...)); got != nil {
		t.Fatalf("finished %q at 1.05 s after its last word", texts(got))
	}
	got := st.take(result(1, true, t0, 4.7, q...))
	if len(got) != 1 || got[0].ended != byPause || got[0].text != "what features did Rafael want in the new version" {
		t.Fatalf("finished %+v", got)
	}
	// One followed by more words after a pause is finished at once, and a
	// mark followed by more words too.
	var next stream
	more := append(spoken(0, "so what about the budget"), spoken(2.5, "we said nine. And then")...)
	got = next.take(result(2, true, t0, 4.2, more...))
	if texts(got) != "so what about the budget | we said nine." || got[1].ended != byMark {
		t.Fatalf("finished %q", texts(got))
	}
	if g, _ := next.growing(); g.text != "And then" {
		t.Fatalf("growing %q", g.text)
	}
}

func TestPartialResultsWithNoTimesAreFinishedWhenNothingNewComes(t *testing.T) {
	var st stream
	t0 := time.Date(2026, 11, 15, 15, 0, 0, 0, time.UTC)
	untimed := func(sound float64, text string) Said {
		s := result(1, true, t0, sound)
		for _, w := range strings.Fields(text) {
			s.Segments = append(s.Segments, Segment{Text: w})
		}
		return s
	}
	st.take(untimed(1, "how many"))
	st.take(untimed(2, "how many mockups did Priya share"))
	if got := st.take(untimed(2.6, "how many mockups did Priya share")); got != nil {
		t.Fatalf("finished %q", texts(got))
	}
	got := st.take(untimed(2.7, "how many mockups did Priya share"))
	if texts(got) != "how many mockups did Priya share" || got[0].ended != byPause {
		t.Fatalf("finished %+v", got)
	}
	if got := st.take(untimed(4, "how many mockups did Priya share and the price")); got != nil {
		t.Fatalf("finished %q", texts(got))
	}
	if got := st.take(untimed(5, "how many mockups did Priya share and the price")); texts(got) != "and the price" {
		t.Fatalf("finished %q", texts(got))
	}
	if r := st.take(untimed(6, "how many mockups did Priya share and the price")); r != nil {
		t.Fatalf("handed on again %q", texts(r))
	}
}

func TestASentenceCutBetweenTwoRequestsIsJoined(t *testing.T) {
	var st stream
	t0 := time.Date(2026, 11, 15, 15, 0, 0, 0, time.UTC)
	first := append(spoken(10, "we said nine."), spoken(13.6, "how many mockups did")...) // ends at 14.9 s
	if got := st.take(result(1, true, t0, 15, first...)); texts(got) != "we said nine." {
		t.Fatalf("the first request handed on %q", texts(got))
	}
	if got := st.take(result(1, false, t0, 15, first...)); got != nil {
		t.Fatalf("its final handed on %q", texts(got))
	}
	t1 := t0.Add(15 * time.Second)
	rest := spoken(0.1, "Priya share in the walkthrough") // ends at 1.85 s
	st.take(result(2, true, t1, 1.2, rest[:3]...))
	if g, _ := st.growing(); g.text != "how many mockups did Priya share in" {
		t.Fatalf("growing %q", g.text)
	}
	if got := st.flush(t1.Add(time.Minute), 20*time.Second); got != nil {
		t.Fatalf("flushed %q while it was carried on", texts(got))
	}
	if got := st.take(result(2, true, t1, 2.1, rest...)); got != nil {
		t.Fatalf("handed on %q", texts(got))
	}
	got := st.take(result(2, true, t1, 2.9, rest...))
	if len(got) != 1 || got[0].text != "how many mockups did Priya share in the walkthrough" || got[0].ended != byPause || !got[0].from.Equal(t0.Add(seconds(13.6))) {
		t.Fatalf("joined %+v", got)
	}
	// One that the next request starts too late to carry on is handed on
	// by itself, and one that nothing follows once the next request has
	// been given enough sound with no word.
	st.take(result(2, false, t1, 15, append(rest, spoken(14, "and the price")...)...))
	t2 := t1.Add(15 * time.Second)
	if got := st.take(result(3, true, t2, 1.4)); got != nil {
		t.Fatalf("handed on %q", texts(got))
	}
	if got := st.flush(t2.Add(2*time.Second), 20*time.Second); got != nil {
		t.Fatalf("flushed %q too soon", texts(got))
	}
	st.take(result(3, true, t2, 2.5))
	if got := st.flush(t2.Add(3*time.Second), 20*time.Second); texts(got) != "and the price" || got[0].ended != byPause {
		t.Fatalf("flushed %+v", got)
	}
	st.take(result(3, false, t2, 15, spoken(13.8, "is the launch on")...))
	if got := st.flush(t2.Add(25*time.Second), 20*time.Second); got != nil {
		t.Fatalf("flushed %q too soon", texts(got))
	}
	if got := st.flush(t2.Add(41*time.Second), 20*time.Second); texts(got) != "is the launch on" || got[0].ended != byTime {
		t.Fatalf("flushed %+v", got)
	}
}

func TestARequestWhoseFinalNeverCameIsTakenAsLastHeard(t *testing.T) {
	var st stream
	t0 := time.Date(2026, 11, 15, 15, 0, 0, 0, time.UTC)
	st.take(result(1, true, t0, 3, spoken(0, "how much did Halcyon raise")...))
	got := st.take(result(2, true, t0.Add(15*time.Second), 0.5))
	if texts(got) != "how much did Halcyon raise" {
		t.Fatalf("handed on %q", texts(got))
	}
	if r := st.read(); len(r) != 1 || !r[0].noFinal || !strings.Contains(r[0].String(), "when the next began, with no final") {
		t.Fatalf("counted %+v", r)
	}
	// Its final, coming late, and its partial results are left alone.
	if got := st.take(result(1, false, t0, 3, spoken(0, "how much did Halcyon raise")...)); got != nil {
		t.Fatalf("a late final handed on %q", texts(got))
	}
	if got := st.take(result(1, true, t0, 3.5, spoken(0, "how much did Halcyon raise in")...)); got != nil || !st.open || st.req != 2 {
		t.Fatalf("a late partial result handed on %q, and the request followed is %d", texts(got), st.req)
	}
	// Results with no request number stand alone, as finals did before
	// partial results.
	var old stream
	a := old.take(result(0, false, t0, 3, spoken(0, "how many mockups?")...))
	b := old.take(result(0, false, t0.Add(3*time.Second), 3, spoken(0, "how many mockups?")...))
	if texts(a) != "how many mockups?" || texts(b) != "how many mockups?" {
		t.Fatalf("handed on %q and %q", texts(a), texts(b))
	}
}

func TestResultsWithAndWithoutTimesHandOnEachSentenceOnce(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 15, 0, 0, 0, time.UTC)
	untimed := func(partial bool, sound float64, text string) Said {
		s := result(1, partial, t0, sound)
		for _, w := range strings.Fields(text) {
			s.Segments = append(s.Segments, Segment{Text: w})
		}
		return s
	}
	// Partial results without times, then a final one with them.
	var st stream
	st.take(untimed(true, 2, "how many mockups did Priya share?"))
	if got := st.take(untimed(true, 2.5, "how many mockups did Priya share? We")); texts(got) != "how many mockups did Priya share?" {
		t.Fatalf("handed on %q", texts(got))
	}
	got := st.take(result(1, false, t0, 4, append(spoken(0, "how many mockups did Priya share?"), spoken(2.4, "we said nine.")...)...))
	if texts(got) != "we said nine." {
		t.Fatalf("the final handed on %q", texts(got))
	}
	// And the other way round.
	var other stream
	if got := other.take(result(1, true, t0, 2.5, append(spoken(0, "how many mockups?"), spoken(1.2, "Nine")...)...)); texts(got) != "how many mockups?" {
		t.Fatalf("handed on %q", texts(got))
	}
	if got := other.take(untimed(false, 3, "how many mockups? Nine of them.")); texts(got) != "Nine of them." {
		t.Fatalf("the final handed on %q", texts(got))
	}
}
