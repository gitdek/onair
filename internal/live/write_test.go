package live

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeWriter writes as write says, and counts its closes and warm-ups.
type fakeWriter struct {
	mu      sync.Mutex
	prompts []Prompt
	closes  int
	warms   int
	write   func(ctx context.Context, p Prompt) (string, error)
}

func (w *fakeWriter) Write(ctx context.Context, p Prompt) (string, error) {
	w.mu.Lock()
	w.prompts = append(w.prompts, p)
	write := w.write
	w.mu.Unlock()
	return write(ctx, p)
}
func (w *fakeWriter) Close() { w.mu.Lock(); w.closes++; w.mu.Unlock() }
func (w *fakeWriter) Warm()  { w.mu.Lock(); w.warms++; w.mu.Unlock() }
func (w *fakeWriter) counts() (prompts, closes, warms int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.prompts), w.closes, w.warms
}

// warmJudge is a judge that loads its model when told to, and counts it.
type warmJudge struct {
	fakeJudge
	warms int
}

func (j *warmJudge) Warm() { j.mu.Lock(); j.warms++; j.mu.Unlock() }

// numberOf is the number a prompt gives the note that holds part, or 0.
func numberOf(p Prompt, part string) int {
	for _, l := range strings.Split(p.User, "\n") {
		var n int
		if _, err := fmt.Sscanf(l, "[%d]", &n); err == nil && strings.Contains(l, part) {
			return n
		}
	}
	return 0
}

// statementOf is the number a check gives the statement that holds part.
func statementOf(p Prompt, part string) int {
	for _, l := range strings.Split(p.User, "\n") {
		var n int
		if _, err := fmt.Sscanf(l, "S%d.", &n); err == nil && strings.Contains(l, part) {
			return n
		}
	}
	return 0
}

// writing is a config with a writer's numbers.
func writingConfig(cfg Config) Config {
	cfg.AnswerTokens, cfg.WriteTimeout, cfg.CheckEvery, cfg.Recall, cfg.Hold = 1500, 8*time.Second, 15*time.Second, 5*time.Minute, 2*time.Minute
	return cfg
}

// writingCard is a follow with a writer, and a card whose brief came: the
// card's number and what the writer is asked.
func writingCard(t *testing.T, said string) (*follow, int, Prompt) {
	t.Helper()
	f := listening(writingConfig(Defaults))
	f.writes(true)
	q, ok := notice(utterance{text: said})
	if !ok {
		t.Fatalf("%q isn't noticed", said)
	}
	f.ask(q, t0)
	// Without the writer free to reword it, the question goes as heard.
	if _, _, ok := f.answerDue(t0); ok {
		t.Fatal("the question went before the writer could reword it")
	}
	_, n, _ := f.answerDue(t0.Add(RewordWait))
	if shown, taken, write := f.reply(n, madeUp(), nil, at(1)); !taken || !write || shown != 0 {
		t.Fatalf("a brief for the writer: shown %d, taken %t, write %t", shown, taken, write)
	}
	if a := f.panel(at(1)).Answers[0]; a.Note != "Reading what On the Record found…" || len(a.Lines) != 0 {
		t.Fatalf("a card waiting for the writer shows %+v", a)
	}
	p, wn, ok := f.writeDue(at(1))
	if !ok || wn != n {
		t.Fatalf("the writer is asked for card %d, %t", wn, ok)
	}
	return f, n, p
}

func TestTheWriterIsGivenTheBriefsLinesRankedAndNumbered(t *testing.T) {
	_, _, p := writingCard(t, "how much were they looking to collect for Halcyon")
	if p.System != answerSystem || p.Most != answerTokens || !strings.HasSuffix(p.User, "\n\nQuestion: How much were they looking to collect for Halcyon?") {
		t.Fatalf("asked %+v", p)
	}
	// The line most like the question comes first, and every other line
	// a browser can open follows, so the model can find an answer that
	// shares no word with the question.
	lines := strings.Split(strings.TrimPrefix(p.User, "Notes:\n"), "\n")
	if lines[0] != "[1] Halcyon partner sync, 2026-09-14, Investors · Discussion: Halcyon hopes to close a $750k pre-seed round" {
		t.Fatalf("the first passage is %q", lines[0])
	}
	if !strings.Contains(p.User, ": Doors open to the public on the 9th") || !strings.Contains(p.User, "said at 52:10: “we would rather have it out before the long weekend”") {
		t.Fatalf("lines are left out:\n%s", p.User)
	}
	if strings.Contains(p.User, "file nobody can open") || strings.Count(p.User, "\n[") != 10 {
		t.Fatalf("passages:\n%s", p.User)
	}
	// A topic is asked about as a question.
	q, _ := notice(utterance{text: "let's talk about the Beacon assistant"})
	if p := answerPrompt(q, madeUp().Points[:1]); !strings.HasSuffix(p.User, "Question: What do the notes say about the Beacon assistant?") {
		t.Fatalf("a topic is asked as %q", p.User)
	}
	// About as many tokens are given as the config says, the best lines
	// first.
	q, _ = notice(utterance{text: "how much were they looking to collect for Halcyon"})
	if from := passages(q, madeUp(), 40); len(from) != 1 || from[0].Text != "Halcyon hopes to close a $750k pre-seed round" {
		t.Fatalf("with 40 tokens: %+v", from)
	}
}

func TestAWrittenAnswerShowsOnlyWhenItChecksOut(t *testing.T) {
	f, n, p := writingCard(t, "how much were they looking to collect for Halcyon")
	k := numberOf(p, "$750k")
	if o, why, taken := f.answerWritten(n, fmt.Sprintf("They hope to close a $750k pre-seed round [%d].", k), nil); !taken || o != answered || why != "" {
		t.Fatalf("an answer: %d, %q, %t", o, why, taken)
	}
	a := f.panel(at(2)).Answers[0]
	want := Answer{Question: "How much were they looking to collect for Halcyon?", Text: "They hope to close a $750k pre-seed round [1].",
		Sources: []AnswerLine{{Text: "[1] Halcyon partner sync · 2026-09-14 · 6:12", URL: "https://archive.example/m/a?t=372"}}}
	if fmt.Sprint(a) != fmt.Sprint(want) {
		t.Fatalf("the card shows %+v", a)
	}
	for text, note := range map[string]string{
		"Not in the archive.":                                                        "Not in the archive.",
		"They hope to close a $750k pre-seed round [12].":                            "The local model's answer didn't check out, so it isn't shown.",
		"They hope to close a $750k pre-seed round.":                                 "The local model's answer didn't check out, so it isn't shown.",
		"They hope to close a $750k pre-seed round [1]. Not in the archive, though.": "The local model's answer didn't check out, so it isn't shown.",
	} {
		f, n, _ := writingCard(t, "how much were they looking to collect for Halcyon")
		f.answerWritten(n, text, nil)
		if a := f.panel(at(2)).Answers[0]; a.Note != note || a.Text != "" || len(a.Lines) != 0 || len(a.Sources) != 0 {
			t.Fatalf("for %q the card shows %+v", text, a)
		}
	}
	// When the writer fails, or is late, the lines that share the
	// question's words show, and the status says why.
	for err, status := range map[error]string{
		errors.New("the local model stopped: Killed"): "Couldn't ask the local model for an answer: the local model stopped.",
		ErrLoading: "The local model is still loading, so the answer shows lines that share the question's words.",
	} {
		f, n, _ := writingCard(t, "how much were they looking to collect for Halcyon")
		f.answerWritten(n, "", err)
		p := f.panel(at(2))
		if a := p.Answers[0]; len(a.Lines) == 0 || a.Lines[0].Text != "Halcyon hopes to close a $750k pre-seed round" || a.Text != "" || a.Note != "" {
			t.Fatalf("after %v the card shows %+v", err, a)
		}
		if !strings.Contains(p.Status, status) {
			t.Fatalf("after %v the status says %q", err, p.Status)
		}
	}
	f, n, _ = writingCard(t, "how much were they looking to collect for Halcyon")
	if f.abandonWrite(at(1+8)) != 0 || f.abandonWrite(at(1+9.5)) != n || len(f.panel(at(11)).Answers[0].Lines) == 0 {
		t.Fatal("a late answer isn't given up on")
	}
	if _, _, taken := f.answerWritten(n, "They hope to close a $750k pre-seed round [1].", nil); taken {
		t.Fatal("an answer given up on was taken")
	}
}

func TestTheWriterIsReadStrictly(t *testing.T) {
	for _, tc := range []struct {
		text, shown string
		cited       []int
		o           outcome
	}{
		{"Halcyon hopes to raise $750k [2].", "Halcyon hopes to raise $750k [1].", []int{2}, answered},
		// Two sentences at most, citations numbered again in the order
		// they are first cited.
		{"They plan offline sync [4] and a calendar view [2, 4]. Voice notes wait [4]. Then more [1].", "They plan offline sync [1] and a calendar view [2, 1]. Voice notes wait [1].", []int{4, 2}, answered},
		{"**The doors open on the 9th** [3]", "The doors open on the 9th [3]"[0:0] + "The doors open on the 9th [1]", []int{3}, answered},
		// A sentence that cites nothing isn't shown.
		{"The round is $750k [1]. It is led by angels.", "The round is $750k [1].", []int{1}, answered},
		{"Halcyon is a startup. The round is $750k [3]", "The round is $750k [1]", []int{3}, answered},
		{"Not in the archive", "", nil, notInArchive},
		{"not in the archive.", "", nil, notInArchive},
		{"“Not in the archive.”", "", nil, notInArchive},
		{"It is $750k [1]. Otherwise not in the archive.", "", nil, rejected},
		{"It is $750k.", "", nil, rejected},
		{"It is $750k [5].", "", nil, rejected},
		{"It is $750k [0].", "", nil, rejected},
		{"It is $750k [note 1].", "", nil, rejected},
		{"It is $750k [1-2].", "", nil, rejected},
		{"", "", nil, rejected},
		{strings.Repeat("very ", 70) + "long [1].", "", nil, rejected},
	} {
		shown, cited, o, why := readAnswer(tc.text, 4)
		if shown != tc.shown || fmt.Sprint(cited) != fmt.Sprint(tc.cited) || o != tc.o || (o == rejected) != (why != "") {
			t.Errorf("%q is read as %q %v %d (%s)", tc.text, shown, cited, o, why)
		}
	}
}

// checking is a follow with a writer and a brief's records, and a
// statement said at s seconds.
func checking(said string, s float64) *follow {
	f := listening(writingConfig(Defaults))
	f.writes(true)
	f.remember(madeUp(), t0)
	f.state(utterance{text: said, to: at(s)})
	return f
}

func TestWhatIsSaidIsCheckedAgainstTheRecordAndShownOnce(t *testing.T) {
	f := checking("so Halcyon is closing a $2 million seed round next month.", 1)
	p, n, said, records, ok := f.checkDue(at(2))
	if !ok || said != 1 || records == 0 || p.System != checkSystem || !strings.Contains(p.User, "\n\nJust said:\nS1. so Halcyon is closing a $2 million seed round next month.") {
		t.Fatalf("a check: %t, %d statements, %d records:\n%s", ok, said, records, p.User)
	}
	if strings.Contains(p.User, "Tomas") {
		t.Fatalf("a check gives records about something else:\n%s", p.User)
	}
	// Nothing new said, nothing to check.
	if _, _, _, _, ok := f.checkDue(at(30)); ok {
		t.Fatal("a check while one is out")
	}
	verdict := fmt.Sprintf(`{"differs": true, "statement": "S1", "note": "%d", "said": "$2 million seed round", "record": "$750k pre-seed round"}`, numberOf(p, "$750k"))
	if result, taken := f.checkedBy(n, verdict, nil, at(3)); !taken || result != "differs, shown" {
		t.Fatalf("the check came to %q, %t", result, taken)
	}
	want := AnswerLine{Text: "Differs from the record: said “so Halcyon is closing a $2 million seed round next month.”; the record says “Halcyon hopes to close a $750k pre-seed round”",
		Where: "Halcyon partner sync · 2026-09-14 · 6:12", URL: "https://archive.example/m/a?t=372"}
	if d := f.panel(at(3)).Differs; d != want {
		t.Fatalf("the panel shows %+v", d)
	}
	if _, _, _, _, ok := f.checkDue(at(10)); ok {
		t.Fatal("a check with nothing new said")
	}
	// Said again, it is checked again, but the record shown to differ
	// isn't asked about again within minutes.
	f.state(utterance{text: "Halcyon's seed round is $2 million, right.", to: at(16)})
	if _, _, _, _, ok := f.checkDue(at(16)); ok {
		t.Fatal("a check within 15 s of the last")
	}
	p, n, _, _, ok = f.checkDue(at(18))
	if !ok || strings.Contains(p.User, "$750k") {
		t.Fatalf("the next check, %t:\n%s", ok, p.User)
	}
	if result, _ := f.checkedBy(n, `{"differs": false}`, nil, at(19)); result != "nothing differs" {
		t.Fatalf("the next check came to %q", result)
	}
	if f.panel(at(3+119)).Differs != want || f.panel(at(3+120)).Differs.Text != "" {
		t.Fatal("a difference isn't shown for two minutes")
	}
}

func TestWhatWasSaidBeforeTheBriefCameIsChecked(t *testing.T) {
	f := listening(writingConfig(Defaults))
	f.writes(true)
	f.state(utterance{text: "so Halcyon is closing a $2 million seed round next month.", to: at(1)})
	if _, _, _, _, ok := f.checkDue(at(2)); ok {
		t.Fatal("a check with no record to check against")
	}
	f.remember(madeUp(), at(5))
	if p, _, said, _, ok := f.checkDue(at(18)); !ok || said != 1 || !strings.Contains(p.User, "$750k") {
		t.Fatalf("what was said before the brief came isn't checked: %t", ok)
	}
	// Once out of the window, it isn't.
	g := listening(writingConfig(Defaults))
	g.writes(true)
	g.state(utterance{text: "so Halcyon is closing a $2 million seed round next month.", to: at(1)})
	g.checkDue(at(2))
	g.remember(madeUp(), at(70))
	if _, _, _, _, ok := g.checkDue(at(71)); ok {
		t.Fatal("a check of what was said over a minute before the brief came")
	}
}

func TestFiguresAreReadAndFitted(t *testing.T) {
	for text, want := range map[string]string{
		"about $2 million for the round": "[{2e+06 true}]",
		"a $750k pre-seed round":         "[{750000 false}]",
		"past two thousand sign ups":     "[{2000 true}]",
		"2,300 sign ups, up from 1,400":  "[{2300 false} {1400 false}]",
		"twenty five testers on the 9th": "[{25 false} {9 false}]",
		"more than 1.5m and under 40%":   "[{1.5e+06 true} {40 true}]",
		"no figure here, only words":     "[]",
	} {
		if got := fmt.Sprint(figures(text)); got != want {
			t.Errorf("%q gives %s", text, got)
		}
	}
	if !fits("past two thousand now", "The waitlist reached 2,300") || fits("about $2 million", "a $750k round") || fits("$12 a seat", "$15 a seat") {
		t.Fatal("figures fit wrongly")
	}
}

func TestADifferenceMustCheckOut(t *testing.T) {
	records := func() []record {
		var out []record
		for _, p := range madeUp().Points[:2] {
			out = append(out, record{p: p, facts: facts(p.Text)})
		}
		return out
	}
	said := func(texts ...string) []statement {
		var out []statement
		for _, t := range texts {
			out = append(out, statement{text: t, facts: facts(t)})
		}
		return out
	}
	funding := said("Honestly the weather was great this week.", "So Halcyon is closing a two million dollar seed round next month.")
	for _, tc := range []struct {
		name, text string
		said       []statement
		ok         bool
	}{
		{"numbers", `{"differs": true, "statement": 2, "note": 1, "said": "two million dollar seed round", "record": "$750k pre-seed round"}`, funding, true},
		// As the model writes it, whole sentences with their numbers.
		{"sentences", "```json\n" + `{"differs": true, "statement": "S2. So Halcyon is closing a two million dollar seed round next month.", "note": "1", "said": "S2. So Halcyon is closing a two million dollar seed round next month.", "record": "Halcyon hopes to close a $750k pre-seed round"}` + "\n```", funding, true},
		{"nothing differs", `{"differs": false}`, funding, false},
		{"not JSON", `The statement differs from note 1.`, funding, false},
		{"no verdict", `{"statement": 2, "note": 1}`, funding, false},
		{"a statement it wasn't given", `{"differs": true, "statement": 3, "note": 1, "said": "two million", "record": "$750k"}`, funding, false},
		{"a note it wasn't given", `{"differs": true, "statement": 2, "note": 9, "said": "two million", "record": "$750k"}`, funding, false},
		{"words it made up", `{"differs": true, "statement": 2, "note": 1, "said": "three million", "record": "$750k"}`, funding, false},
		{"no quotes", `{"differs": true, "statement": 2, "note": 1}`, funding, false},
		{"about something else", `{"differs": true, "statement": 1, "note": 1, "said": "this week", "record": "$750k"}`, funding, false},
		{"it agrees", `{"differs": true, "statement": 1, "note": 1, "said": "$750k pre-seed", "record": "$750k pre-seed round"}`, said("Halcyon is still aiming for the $750k pre-seed round."), false},
		{"the same number in other words", `{"differs": true, "statement": 1, "note": 2, "said": "three engineers", "record": "three engineers"}`, said("Halcyon will hire three engineers next quarter."), false},
		{"a number said roughly that fits", `{"differs": true, "statement": 1, "note": 2, "said": "over two engineers", "record": "three engineers"}`, said("Halcyon will hire over two engineers."), false},
		{"a number said roughly that doesn't fit", `{"differs": true, "statement": 2, "note": 1, "said": "about $2 million", "record": "$750k"}`, said("Let's see.", "It's about $2 million for the Halcyon seed round."), true},
	} {
		s, r, _, _, why := readCheck(tc.text, tc.said, records())
		if ok := s != nil && r != nil && why == ""; ok != tc.ok || tc.ok && (s.text != tc.said[1].text || r.p.Text != "Halcyon hopes to close a $750k pre-seed round") {
			t.Errorf("%s: %v %v %q", tc.name, s, r, why)
		}
		if !tc.ok && tc.name != "nothing differs" && why == "" {
			t.Errorf("%s: rejected without a reason", tc.name)
		}
	}
}

func TestTheWriterTheJudgeAndTheArchiveTakeTurns(t *testing.T) {
	f := listening(writingConfig(Defaults))
	f.judge(true)
	f.writes(true)
	f.remember(madeUp(), t0)
	// A round is out: no answer is written and nothing is checked.
	_, round := roundAt(t, f, at(1))
	q, _ := notice(utterance{text: "how much were they looking to collect for Halcyon"})
	f.ask(q, at(1))
	if _, _, ok := f.rewordDue(at(1)); ok {
		t.Fatal("a question is reworded while a round is out")
	}
	_, n, _ := f.answerDue(at(2))
	f.reply(n, madeUp(), nil, at(2))
	f.state(utterance{text: "so Halcyon is closing a $2 million seed round next month.", to: at(2)})
	if _, _, ok := f.writeDue(at(2)); ok {
		t.Fatal("an answer is written while a round is out")
	}
	f.judgement(round, nil, Verdict{}, nil, at(2))
	// The answer goes first, and then the check, and no round meanwhile.
	if _, _, _, _, ok := f.checkDue(at(3)); ok {
		t.Fatal("a check while an answer waits to be written")
	}
	if _, _, ok := f.writeDue(at(3)); !ok {
		t.Fatal("the answer isn't written")
	}
	f.hear(Heard{Speaker: Others, Text: words(Defaults.Least, "word"), To: at(20)}, at(20))
	if _, _, ok := f.roundDue(at(20)); ok {
		t.Fatal("a round while an answer is written")
	}
	f.answerWritten(n, "Not in the archive", nil)
	if _, _, _, _, ok := f.checkDue(at(20)); !ok {
		t.Fatal("nothing is checked after the answer")
	}
	if _, _, ok := f.roundDue(at(20)); ok {
		t.Fatal("a round while a check is out")
	}
	// Unchosen, the writer leaves what waited for it to the lines.
	g, _, _ := writingCard(t, "how much were they looking to collect for Halcyon")
	g.writes(false)
	if a := g.panel(at(2)).Answers[0]; len(a.Lines) == 0 || a.Note != "" {
		t.Fatalf("with the writer gone the card shows %+v", a)
	}
}

func TestTheDecisionModelLoadingIsNoFailure(t *testing.T) {
	f := judging()
	_, n := roundAt(t, f, at(1))
	f.judgement(n, nil, Verdict{}, fmt.Errorf("asking: %w", ErrLoading), at(1))
	p := f.panel(at(1))
	if !strings.Contains(p.Status, "The local model is loading.") || strings.Contains(p.Status, "Couldn't") {
		t.Fatalf("the status says %q", p.Status)
	}
	if shows := f.shows(p, at(1)); !strings.Contains(shows, "status: model loading") || strings.Contains(shows, "model failed") {
		t.Fatalf("the log says the panel shows %q", shows)
	}
	// The next round asks again, without waiting for something new.
	if _, _, ok := f.roundDue(at(1 + 10)); !ok {
		t.Fatal("no round after the model was loading")
	}
}

func TestTheControllerWritesAnswersAndChecksWhatIsSaid(t *testing.T) {
	screen := &logScreen{}
	archive := &fakeArchive{
		answer: func(context.Context, string) (Found, error) { return Found{}, nil },
		brief:  func(context.Context, string) (Brief, error) { return madeUp(), nil },
	}
	writer := &fakeWriter{write: func(ctx context.Context, p Prompt) (string, error) {
		if p.System == rewordSystem {
			return "funding, round, investors", nil
		}
		if p.System == answerSystem {
			return fmt.Sprintf("They hope to close a $750k pre-seed round [%d].", numberOf(p, "$750k")), nil
		}
		return fmt.Sprintf(`{"differs": true, "statement": %d, "note": %d, "said": "$2 million seed round", "record": "$750k pre-seed round"}`, statementOf(p, "$2 million"), numberOf(p, "$750k")), nil
	}}
	judge := &warmJudge{fakeJudge: fakeJudge{verdict: func(context.Context, Round) (Verdict, error) { return Verdict{}, nil }}}
	cfg := writingConfig(fast)
	cfg.CheckEvery, cfg.JudgeEvery, cfg.JudgeTimeout = 10*time.Millisecond, time.Hour, time.Second
	c := Start(cfg, archive, screen, time.Now, map[string]Judge{"local": judge}, map[string]Writer{"local": writer})
	defer c.Enabled(false)
	c.Judging("local")
	c.Enabled(true)
	c.Capturing("", true)
	c.Recording(true)
	waitFor(t, "listening", func() bool { _, l := screen.said(); return len(l) == 1 && l[0] })
	// Both models are told to load as listening starts.
	waitFor(t, "the models to warm", func() bool {
		_, _, warms := writer.counts()
		judge.mu.Lock()
		defer judge.mu.Unlock()
		return warms == 1 && judge.warms == 1
	})
	say := func(words ...string) {
		s := Said{Began: time.Now(), Step: 100 * time.Millisecond, Mic: make([]float64, 40), Call: make([]float64, 40)}
		for i, w := range words {
			s.Segments = append(s.Segments, Segment{Text: w, At: time.Duration(i) * 300 * time.Millisecond, Length: 250 * time.Millisecond})
		}
		c.Said(s)
	}
	say("how", "much", "were", "they", "looking", "to", "collect", "for", "Halcyon?")
	waitFor(t, "a written answer", func() bool { a := screen.last().Answers; return len(a) == 1 && a[0].Text != "" })
	if a := screen.last().Answers[0]; a.Text != "They hope to close a $750k pre-seed round [1]." || len(a.Sources) != 1 || a.Sources[0].Text != "[1] Halcyon partner sync · 2026-09-14 · 6:12" || len(a.Lines) != 0 {
		t.Fatalf("the card shows %+v", a)
	}
	say("so", "Halcyon", "is", "closing", "a", "$2", "million", "seed", "round", "next", "month.")
	waitFor(t, "a difference", func() bool { return screen.last().Differs.Text != "" })
	if d := screen.last().Differs; d.Text != "Differs from the record: said “so Halcyon is closing a $2 million seed round next month.”; the record says “Halcyon hopes to close a $750k pre-seed round”" || d.URL != "https://archive.example/m/a?t=372" {
		t.Fatalf("the panel shows %+v", d)
	}
	waitFor(t, "the log", func() bool {
		screen.mu.Lock()
		defer screen.mu.Unlock()
		return strings.Contains(strings.Join(screen.lines, "\n"), "status: differs from the record")
	})
	c.Judging("")
	waitFor(t, "the writer to close", func() bool { _, closes, _ := writer.counts(); return closes == 1 })
	screen.mu.Lock()
	lines := strings.Join(screen.lines, "\n")
	screen.mu.Unlock()
	for _, said := range []string{"halcyon", "750k", "million", "seed", "collect", "pre-seed", "partner"} {
		if strings.Contains(strings.ToLower(lines), said) {
			t.Fatalf("the log holds what was said, asked or written:\n%s", lines)
		}
	}
	archive.mu.Lock()
	topics := strings.Join(archive.topics, " | ")
	archive.mu.Unlock()
	if topics != "how much were they looking to collect for Halcyon? funding round investors" {
		t.Fatalf("On the Record was asked %q", topics)
	}
	for _, want := range []string{"reword 1 asked: about", "reword 1 came back in", "3 words added", "answer 1 asked, 12 words, 3 of them the local model's, for a card shown", "write 1 asked: 10 passages, about",
		"card 1 shown ", "reworded in ", "written in ", "write 1 came back in", "answered, citing 1 source; so far 1 written, 1 answered, 0 not in the archive, 0 rejected",
		"check 1 asked: 1 statements, ", "check 1 came back in", "differs, shown; so far 1 checked, 1 differ, 0 rejected", "1 written; status"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("the log doesn't say %q:\n%s", want, lines)
		}
	}
}
