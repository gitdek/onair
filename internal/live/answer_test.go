package live

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestItNoticesQuestionsTopicsAndRecalls(t *testing.T) {
	cases := []struct {
		said, shown, topic string
		recall             bool
	}{
		{said: "so um how many mockups did Priya walk us through", shown: "How many mockups did Priya walk us through?", topic: "how many mockups did Priya walk us through"},
		{said: "what budget does Halcyon have for hiring", shown: "What budget does Halcyon have for hiring?"},
		{said: "Is the Halcyon beta still on track", shown: "Is the Halcyon beta still on track?"},
		{said: "did Priya send the pricing sheet", shown: "Did Priya send the pricing sheet?"},
		{said: "we moved the pilot for Halcyon?", shown: "We moved the pilot for Halcyon?"},
		{said: "okay let's talk about the Beacon assistant", shown: "Let's talk about the Beacon assistant", topic: "the Beacon assistant"},
		{said: "let's talk about pricing", shown: "Let's talk about pricing", topic: "pricing"},
		{said: "the last time we talked with Tomas about the Beacon assistant", shown: "The last time we talked with Tomas about the Beacon assistant", recall: true},
		{said: "where were we the last time we met Tomas", shown: "Where were we the last time we met Tomas?", recall: true},
		// Not worth asking: not a question, or too little to ask about.
		{said: "is going up next month"},
		{said: "the pilot is going well"},
		{said: "what do you mean"},
		{said: "how are you doing"},
		{said: "can you see my screen"},
		{said: "um so"},
	}
	for _, tc := range cases {
		q, ok := notice(utterance{text: tc.said})
		if ok != (tc.shown != "") || ok && (q.shown != tc.shown || q.recall != tc.recall || tc.topic != "" && q.topic != tc.topic) {
			t.Errorf("%q: noticed %t as %q, about %q, recall %t", tc.said, ok, q.shown, q.topic, q.recall)
		}
	}
}

func TestItKnowsWhatKindOfAnswerAQuestionWants(t *testing.T) {
	for said, want := range map[string]want{
		"how much cash does Halcyon need":            amount,
		"what did they quote Halcyon as a fee":       amount,
		"how many mockups did Priya share":           count,
		"when does the Halcyon pilot open":           moment,
		"what is the deadline for the Halcyon pilot": moment,
		"who owns the Halcyon contract":              person,
		"which capabilities did Priya list":          anything,
	} {
		if q, ok := notice(utterance{text: said}); !ok || q.want != want {
			t.Errorf("%q wants %d, not %d", said, q.want, want)
		}
	}
}

// seconds is a duration in seconds, for segments.
func seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

func TestAResultSplitsIntoSentencesAtMarksAndPauses(t *testing.T) {
	began := time.Date(2026, 11, 15, 15, 0, 0, 0, time.UTC)
	s := Said{Began: began, Step: 100 * time.Millisecond, Mic: make([]float64, 30), Call: make([]float64, 30)}
	for _, w := range []struct {
		text      string
		at, along float64
	}{{"how", 0, 0.2}, {"many", 0.25, 0.15}, {"mockups", 0.45, 0.35}, {"we", 1.8, 0.1}, {"said", 1.95, 0.15}, {"nine", 2.15, 0.3}, {".", 2.45, 0}, {"great", 2.6, 0.2}} {
		s.Segments = append(s.Segments, Segment{Text: w.text, At: seconds(w.at), Length: seconds(w.along)})
	}
	got := s.sentences()
	if len(got) != 3 || got[0].text != "how many mockups" || got[0].ended != byPause || !got[0].first ||
		got[1].text != "we said nine." || got[1].ended != byMark || got[1].first || !got[1].from.Equal(began.Add(seconds(1.8))) ||
		got[2].text != "great" || got[2].ended != open || !got[2].to.Equal(began.Add(seconds(2.8))) {
		t.Fatalf("sentences %+v", got)
	}
	if e := endings(got); e != "3 sentences, 1 ended by a mark, 1 by a pause, 1 open at the end" {
		t.Fatalf("the log says %q", e)
	}
	// A pause after the last word ends it too.
	s.Mic, s.Call = make([]float64, 40), make([]float64, 40)
	if got := s.sentences(); got[2].ended != byPause {
		t.Fatalf("with a second of silence after it, the last sentence ended %d", got[2].ended)
	}
	// With no times, only marks split it.
	untimed := Said{Began: began, Segments: []Segment{{Text: "how many mockups"}, {Text: "did Priya share?"}, {Text: "nine"}}}
	if got := untimed.sentences(); len(got) != 2 || got[0].text != "how many mockups did Priya share?" || got[0].ended != byMark || got[1].ended != open {
		t.Fatalf("untimed sentences %+v", got)
	}
}

func TestASentenceCutBetweenTwoResultsIsJoined(t *testing.T) {
	f := listening(Defaults)
	t0 := time.Date(2026, 11, 15, 15, 0, 0, 0, time.UTC)
	first := []utterance{{text: "we said nine", ended: byMark}, {text: "how many mockups did", from: t0.Add(13 * time.Second), to: t0.Add(14800 * time.Millisecond), ended: open}}
	if got := f.said(first); len(got) != 1 || got[0].text != "we said nine" {
		t.Fatalf("the first result ended %+v", got)
	}
	next := []utterance{{text: "Priya share in the walkthrough", from: t0.Add(15200 * time.Millisecond), to: t0.Add(17 * time.Second), first: true, ended: byPause}}
	got := f.said(next)
	if len(got) != 1 || got[0].text != "how many mockups did Priya share in the walkthrough" || got[0].ended != byPause || !got[0].from.Equal(t0.Add(13*time.Second)) {
		t.Fatalf("joined %+v", got)
	}
	// One that the next result doesn't carry on ends by itself.
	f.said([]utterance{{text: "and the price", to: t0.Add(30 * time.Second), ended: open}})
	if got := f.said([]utterance{{text: "Later", from: t0.Add(40 * time.Second), first: true, ended: byPause}}); len(got) != 2 || got[0].text != "and the price" {
		t.Fatalf("not carried on %+v", got)
	}
	// One held that nothing carries on ends after a result's length and
	// five seconds.
	f.said([]utterance{{text: "is the launch on", to: t0.Add(60 * time.Second), ended: open}})
	if got := f.flush(t0.Add(70 * time.Second)); got != nil {
		t.Fatalf("flushed early %+v", got)
	}
	if got := f.flush(t0.Add(81 * time.Second)); len(got) != 1 || got[0].ended != byTime {
		t.Fatalf("flushed %+v", got)
	}
}

// madeUp is a made-up answer, shaped as On the Record's briefs are: lines
// of four meetings' notes and one passage of a transcript.
func madeUp() Brief {
	m := func(id, at string) string { return "https://archive.example/m/" + id + at }
	return Brief{Passages: 9, Points: []Point{
		{Text: "Halcyon hopes to close a $750k pre-seed round", Under: "Investors · Discussion", Meeting: "Halcyon partner sync", Date: "2026-09-14", At: "6:12", URL: m("a", "?t=372")},
		{Text: "Halcyon will hire three engineers next quarter", Under: "Investors · Discussion", Meeting: "Halcyon partner sync", Date: "2026-09-14", At: "11:40", URL: m("a", "?t=700")},
		{Text: "A follow-up with the partners is planned", Under: "Next steps", Meeting: "Halcyon partner sync", Date: "2026-09-14", URL: m("a", "")},
		{Text: "Doors open to the public on the 9th", Under: "Release timeline", Meeting: "Halcyon product walkthrough", Date: "2026-10-08", At: "1:05:09", URL: m("b", "?t=3909")},
		{Text: "The press kit still needs a logo", Under: "Release timeline", Meeting: "Halcyon product walkthrough", Date: "2026-10-08", At: "1:06:30", URL: m("b", "?t=3990")},
		{Text: "Priya clicked through 9 mockups for the team", Under: "Walkthrough", Meeting: "Halcyon product walkthrough", Date: "2026-10-08", At: "4:21", URL: m("b", "?t=261")},
		{Text: "Priya listed the features she needs: offline sync and a calendar view", Under: "Requests", Meeting: "Halcyon product walkthrough", Date: "2026-10-08", At: "18:30", URL: m("b", "?t=1110")},
		{Text: "we would rather have it out before the long weekend", Said: true, Meeting: "Halcyon product walkthrough", Date: "2026-10-08", At: "52:10", URL: m("b", "?t=3130.4&tab=transcript")},
		{Text: "Tomas demoed the Beacon assistant to the board", Under: "Updates", Meeting: "Beacon check-in", Date: "2026-08-03", At: "0:55", URL: m("c", "?t=55")},
		{Text: "Tomas says the Beacon assistant waits on a new model", Under: "Updates", Meeting: "Beacon check-in", Date: "2026-10-30", At: "7:45", URL: m("d", "?t=465")},
		{Text: "Tomas on the Beacon assistant, in a file nobody can open", Meeting: "Beacon check-in", Date: "2026-10-31", URL: "file:///nowhere"},
	}}
}

func TestItPicksTheLinesThatAnswerAQuestion(t *testing.T) {
	cases := []struct{ said, first, where string }{
		{"how much were they looking to collect for Halcyon", "Halcyon hopes to close a $750k pre-seed round", "Halcyon partner sync · 2026-09-14 · 6:12"},
		// A name misheard past knowing still finds the sum, by the money
		// the question asks about.
		{"how much funding is Halsey getting", "Halcyon hopes to close a $750k pre-seed round", ""},
		// The answer shares no word with the question: its section and the
		// date it gives carry it.
		{"what day are we shipping it", "Doors open to the public on the 9th", "Halcyon product walkthrough · 2026-10-08 · 1:05:09"},
		{"how many pages did Priya go through in the walkthrough", "Priya clicked through 9 mockups for the team", ""},
		{"which capabilities did Priya ask for in the upcoming build", "Priya listed the features she needs: offline sync and a calendar view", ""},
		// About the last time, the newest meeting's line comes first.
		{"the last time we talked with Tomas about the Beacon assistant", "Tomas says the Beacon assistant waits on a new model", "Beacon check-in · 2026-10-30 · 7:45"},
	}
	for _, tc := range cases {
		q, ok := notice(utterance{text: tc.said})
		if !ok {
			t.Fatalf("%q isn't noticed", tc.said)
		}
		lines := answerLines(q, madeUp(), 3)
		if len(lines) == 0 || lines[0].Text != tc.first || tc.where != "" && lines[0].Where != tc.where || !strings.HasPrefix(lines[0].URL, "https://archive.example/") {
			t.Errorf("%q is answered by %+v", tc.said, lines)
		}
		if len(lines) > 3 {
			t.Errorf("%q is answered by %d lines", tc.said, len(lines))
		}
	}
	// A long line is cut short enough for a line or two.
	q, _ := notice(utterance{text: "how much were they looking to collect for Halcyon"})
	long := Brief{Points: []Point{{Text: "Halcyon hopes to close a $750k pre-seed round " + strings.Repeat("and then some more words ", 10), URL: "https://archive.example/m/a"}}}
	if lines := answerLines(q, long, 3); len(lines) != 1 || len([]rune(lines[0].Text)) > 110 || !strings.HasSuffix(lines[0].Text, "…") {
		t.Errorf("a long line is shown as %+v", lines)
	}
}

func TestItShowsNoWeakLines(t *testing.T) {
	for _, said := range []string{"what did they decide about the office lease", "who is hosting the summer party"} {
		q, ok := notice(utterance{text: said})
		if !ok {
			t.Fatalf("%q isn't noticed", said)
		}
		if lines := answerLines(q, madeUp(), 3); len(lines) != 0 {
			t.Errorf("%q is answered by %+v", said, lines)
		}
	}
	q, _ := notice(utterance{text: "how many mockups did Priya share"})
	if lines := answerLines(q, Brief{}, 3); len(lines) != 0 {
		t.Errorf("an empty brief gives %+v", lines)
	}
}

func TestAPassageOfTheTranscriptIsQuoted(t *testing.T) {
	q, ok := notice(utterance{text: "do you remember when Halcyon wanted the release before the long weekend"})
	if !ok {
		t.Fatal("the question isn't noticed")
	}
	for _, l := range answerLines(q, madeUp(), 3) {
		if l.Text == "“we would rather have it out before the long weekend”" && l.Where == "Halcyon product walkthrough · 2026-10-08 · 52:10" {
			return
		}
	}
	t.Fatalf("lines %+v", answerLines(q, madeUp(), 3))
}

func TestTheSameQuestionIsntAskedAgainWithinMinutes(t *testing.T) {
	f := listening(Defaults)
	t0 := time.Date(2026, 11, 15, 15, 0, 0, 0, time.UTC)
	q := func(said string) question {
		q, ok := notice(utterance{text: said})
		if !ok {
			t.Fatalf("%q isn't noticed", said)
		}
		return q
	}
	if n, again := f.ask(q("how many mockups did Priya share"), t0); n != 1 || again {
		t.Fatalf("the first question: card %d, again %t", n, again)
	}
	if _, again := f.ask(q("so how many mockups did Priya share in the walkthrough"), t0.Add(time.Minute)); !again {
		t.Fatal("the same question was asked again a minute later")
	}
	if n, again := f.ask(q("how much cash does Halcyon need"), t0.Add(time.Minute)); n != 2 || again {
		t.Fatalf("another question: card %d, again %t", n, again)
	}
	if n, again := f.ask(q("how many mockups did Priya share"), t0.Add(4*time.Minute)); n != 3 || again {
		t.Fatalf("the same question four minutes later: card %d, again %t", n, again)
	}
	f.ask(q("which capabilities did Priya list for the build"), t0.Add(5*time.Minute))
	p := f.panel(t0.Add(5 * time.Minute))
	if len(p.Answers) != 3 || p.Answers[0].Question != "Which capabilities did Priya list for the build?" || p.Answers[2].Question != "How much cash does Halcyon need?" {
		t.Fatalf("cards %+v", p.Answers)
	}
	if p.Answers[0].Note != "Looking in On the Record…" {
		t.Fatalf("a card not yet answered says %q", p.Answers[0].Note)
	}
}

func TestACardsQuestionIsAskedOnceAtATimeOldestFirst(t *testing.T) {
	f := listening(Defaults)
	t0 := time.Date(2026, 11, 15, 15, 0, 0, 0, time.UTC)
	for _, said := range []string{"how many mockups did Priya share", "how much cash does Halcyon need"} {
		q, _ := notice(utterance{text: said})
		f.ask(q, t0)
	}
	topic, n, ok := f.answerDue(t0)
	if !ok || n != 1 || topic != "how many mockups did Priya share" {
		t.Fatalf("asked %q, card %d", topic, n)
	}
	if _, _, ok := f.answerDue(t0); ok {
		t.Fatal("a second question was asked while the first waits")
	}
	if shown, taken, _ := f.reply(n, madeUp(), nil, t0); !taken || shown == 0 {
		t.Fatalf("the answer was taken %t, with %d lines", taken, shown)
	}
	_, n, _ = f.answerDue(t0.Add(time.Second))
	f.reply(n, Brief{}, errors.New("On the Record didn't answer in time"), t0)
	p := f.panel(t0.Add(2 * time.Second))
	if p.Answers[0].Note != "On the Record didn't answer in time." || p.Answers[1].Lines[0].Text != "Priya clicked through 9 mockups for the team" || p.Answers[1].Note != "" {
		t.Fatalf("cards %+v", p.Answers)
	}
	// A card with no lines says so, and one whose answer never comes is
	// given up on.
	q, _ := notice(utterance{text: "what did they decide about the office lease"})
	f.ask(q, t0.Add(3*time.Second))
	_, n, _ = f.answerDue(t0.Add(3 * time.Second))
	f.reply(n, madeUp(), nil, t0)
	if note := f.panel(t0.Add(4 * time.Second)).Answers[0].Note; note != "No answer in the archive." {
		t.Fatalf("a card with no answer says %q", note)
	}
	q, _ = notice(utterance{text: "who is hosting the summer party"})
	f.ask(q, t0.Add(5*time.Second))
	_, n, _ = f.answerDue(t0.Add(5 * time.Second))
	if f.abandonAnswer(t0.Add(15 * time.Second)); f.answering != n {
		t.Fatal("given up on before its time")
	}
	if got := f.abandonAnswer(t0.Add(17 * time.Second)); got != n || f.panel(t0.Add(17 * time.Second)).Answers[0].Note != "On the Record didn't answer within 10s." {
		t.Fatalf("gave up on %d: %+v", got, f.panel(t0.Add(17 * time.Second)).Answers[0])
	}
	if _, taken, _ := f.reply(n, madeUp(), nil, t0); taken {
		t.Fatal("an answer given up on was taken")
	}
}

func TestTheControllerAnswersAQuestionHeardInTheCall(t *testing.T) {
	screen := &logScreen{}
	archive := &fakeArchive{
		answer: func(context.Context, string) (Found, error) { return Found{}, nil },
		brief:  func(context.Context, string) (Brief, error) { return madeUp(), nil },
	}
	c := Start(fast, archive, screen, time.Now, nil, nil)
	defer c.Enabled(false)
	c.Enabled(true)
	c.Capturing("", true)
	c.Recording(true)
	waitFor(t, "listening", func() bool { _, l := screen.said(); return len(l) == 1 && l[0] })
	say := func(words ...string) {
		s := Said{Began: time.Now(), Step: 100 * time.Millisecond, Mic: make([]float64, 30), Call: make([]float64, 30)}
		for i, w := range words {
			s.Segments = append(s.Segments, Segment{Text: w, At: time.Duration(i) * 300 * time.Millisecond, Length: 250 * time.Millisecond})
		}
		c.Said(s)
	}
	say("so", "how", "many", "mockups", "did", "Priya", "share?")
	waitFor(t, "an answer", func() bool { a := screen.last().Answers; return len(a) == 1 && len(a[0].Lines) > 0 })
	a := screen.last().Answers[0]
	if a.Question != "How many mockups did Priya share?" || a.Lines[0] != (AnswerLine{Text: "Priya clicked through 9 mockups for the team", Where: "Halcyon product walkthrough · 2026-10-08 · 4:21", URL: "https://archive.example/m/b?t=261"}) {
		t.Fatalf("the card shows %+v", a)
	}
	say("what", "did", "they", "decide", "about", "the", "office", "lease?")
	waitFor(t, "a second card", func() bool {
		a := screen.last().Answers
		return len(a) == 2 && a[0].Note == "No answer in the archive."
	})
	archive.mu.Lock()
	topics := strings.Join(archive.topics, " | ")
	archive.mu.Unlock()
	if topics != "how many mockups did Priya share? | what did they decide about the office lease?" {
		t.Fatalf("asked %q", topics)
	}
	screen.mu.Lock()
	lines := strings.Join(screen.lines, "\n")
	screen.mu.Unlock()
	for _, said := range []string{"priya", "mockups", "halcyon", "lease", "clicked"} {
		if strings.Contains(strings.ToLower(lines), said) {
			t.Fatalf("the log holds what was said or answered:\n%s", lines)
		}
	}
	for _, want := range []string{"1 ended by a mark", "question 1 noticed: 6 words, 3 key words, someone else's, its sentence ended by a mark", "answer 1 asked, 6 words",
		"answer 1 came back in", "11 lines from 5 meetings, 9 passages; 2 shown", "answer 2 came back in", "0 shown; so far 2 asked and 2 lines shown", "2 answer cards with 2 lines"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("the log doesn't say %q:\n%s", want, lines)
		}
	}
}
