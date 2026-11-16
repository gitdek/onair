package live

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTheLinesUnderAMatchingHeadingSurviveThePreRanking(t *testing.T) {
	b := Roadmap()
	q, _ := notice(utterance{text: "What features did Rafael want in the new version?"})
	roadmap := func(from []Point) int {
		n := 0
		for _, p := range from {
			if strings.Contains(p.Under, "Capabilities") {
				n++
			}
		}
		return n
	}
	// As the writer was given lines before: ranked by relevance, which a
	// line with no key word of its own doesn't have, to 1,500 tokens.
	type scored struct {
		p Point
		r float64
	}
	var old []scored
	for _, p := range b.Points {
		old = append(old, scored{p, q.relevance(p)})
	}
	slices.SortStableFunc(old, func(a, b scored) int { return cmp.Compare(b.r, a.r) })
	var before []Point
	used := 0
	for _, s := range old {
		if cost := tokens(numbered(len(before)+1, s.p)); used+cost <= 1500 {
			used += cost
			before = append(before, s.p)
		} else {
			break
		}
	}
	if roadmap(before) != 0 {
		t.Fatalf("the old ranking kept %d of the roadmap's lines", roadmap(before))
	}
	// Now: what a line sits under counts, the writer's other words for the
	// question count, and the writer reads about 2,500 tokens.
	all := passages(q, b, Defaults.AnswerTokens)
	if roadmap(all) != 4 {
		t.Fatalf("the roadmap's lines kept: %d of 4, in %d lines", roadmap(all), len(all))
	}
	q.extra = readReword("capabilities, roadmap, plans, improvements, new version", q)
	ranked := passages(q, b, 1500)
	if roadmap(ranked) != 4 || !strings.Contains(ranked[0].Under, "Capabilities") {
		t.Fatalf("with the writer's words, at 1,500 tokens, the roadmap's lines kept: %d of 4, first %q", roadmap(ranked), ranked[0].Under)
	}
	t.Logf("the brief: %d lines, about %d tokens; before, %d lines kept, none of the roadmap's; now %d lines, about %d tokens, all 4 of the roadmap's",
		len(b.Points), briefTokens(b), len(before), len(all), briefTokens(Brief{Points: all}))
}

func TestTheWritersOtherWordsAreReadStrictly(t *testing.T) {
	q, _ := notice(utterance{text: "What features did Rafael want in the new version?"})
	words := func(text string) string {
		var out []string
		for _, k := range readReword(text, q) {
			out = append(out, k.word)
		}
		return strings.Join(out, " ")
	}
	for _, tc := range []struct{ text, words string }{
		{"capabilities, roadmap, plans, improvements, new version", "capabilities roadmap plans improvements"},
		{"Capabilities; Roadmap / plans\nignore the rest of this", "capabilities roadmap plans"},
		{`"capabilities" [1] roadmap, roadmap, Rafael, features`, "capabilities roadmap"},
		{"a b c 2027 supercalifragilisticexpialidocious upgrades", "2027 upgrades"},
		{"one two three four five six seven eight nine ten eleven", "two three four five six seven eight nine"},
		{"", ""},
	} {
		if got := words(tc.text); got != tc.words {
			t.Errorf("%q gave %q, not %q", tc.text, got, tc.words)
		}
	}
	p := rewordPrompt(q)
	if p.System != rewordSystem || p.User != "Question: What features did Rafael want in the new version?" || !p.Quick || p.Most != rewordTokens {
		t.Fatalf("the writer is asked %+v", p)
	}
}

func TestAQuestionStillBeingAskedIsAskedEarlyAndKept(t *testing.T) {
	f := listening(writingConfig(Defaults))
	f.writes(true)
	f.cfg.AnswerTokens = 2500
	growing, _ := notice(utterance{text: "So what features did Rafael want"})
	if started, _ := f.early(growing, true, at(1)); started != 1 {
		t.Fatalf("started %d", started)
	}
	if p := f.panel(at(1)); len(p.Answers) != 0 {
		t.Fatalf("a question still being asked shows %+v", p.Answers)
	}
	// The writer rewords it first, and On the Record is asked with its
	// words.
	if _, _, ok := f.answerDue(at(1)); ok {
		t.Fatal("asked before the writer reworded it")
	}
	if _, n, ok := f.rewordDue(at(1)); !ok || n != 1 {
		t.Fatalf("reworded %d, %t", n, ok)
	}
	if added, failed, taken := f.reworded(1, "capabilities, roadmap, plans", nil, at(1.5)); added != 3 || failed != "" || !taken {
		t.Fatalf("added %d, failed %q", added, failed)
	}
	topic, n, ok := f.answerDue(at(1.5))
	if !ok || n != 1 || topic != "what features did Rafael want capabilities roadmap plans" {
		t.Fatalf("asked %q for %d", topic, n)
	}
	// It goes on asking the same: nothing changes.
	more, _ := notice(utterance{text: "So what features did Rafael want in the new"})
	if started, dropped := f.early(more, true, at(2)); started != 0 || dropped != 0 {
		t.Fatalf("started %d, dropped %d", started, dropped)
	}
	f.reply(1, Roadmap(), nil, at(3))
	if p := f.panel(at(3)); len(p.Answers) != 0 {
		t.Fatalf("an answer to a question still being asked shows %+v", p.Answers)
	}
	if _, _, ok := f.writeDue(at(3)); ok {
		t.Fatal("an answer was written before its question was finished")
	}
	// Finished, it is the card, with what was found for it.
	asked, _ := notice(utterance{text: "So what features did Rafael want in the new version?", to: at(3.5)})
	if n, again, kept := f.ask(asked, at(4.5)); n != 1 || again || !kept {
		t.Fatalf("card %d, again %t, kept %t", n, again, kept)
	}
	if a := f.panel(at(4.5)).Answers; len(a) != 1 || a[0].Question != "What features did Rafael want in the new version?" || a[0].Note != "Reading what On the Record found…" {
		t.Fatalf("the card shows %+v", a)
	}
	p, n, ok := f.writeDue(at(4.5))
	if !ok || n != 1 || !strings.Contains(p.User, "Desktop and tablet apps") || !strings.Contains(p.User, "Question: What features did Rafael want in the new version?") {
		t.Fatalf("the writer is asked %+v", p)
	}
	if _, _, ok := f.answerDue(at(5)); ok {
		t.Fatal("On the Record was asked again")
	}
	f.answerWritten(1, "Rafael wanted desktop and tablet apps [1].", nil)
	if l := f.timing(f.card(1), at(6)); l != "card 1 shown 2.5 s after its question's last word: noticed 1.0 s after it, reworded in 0.5 s, On the Record answered in 1.5 s, asked 3.0 s before the question was finished, written in 1.5 s" {
		t.Fatalf("the log says %q", l)
	}
}

func TestAQuestionAskedEarlyIsDroppedWhenTheSentenceChanges(t *testing.T) {
	f := listening(Defaults)
	growing, _ := notice(utterance{text: "how much did Halcyon raise"})
	f.early(growing, true, at(1))
	// With no writer, the question goes as heard at once.
	if topic, n, ok := f.answerDue(at(1)); !ok || n != 1 || topic != "how much did Halcyon raise" || !f.waits(1) {
		t.Fatalf("asked %q for %d", topic, n)
	}
	other, _ := notice(utterance{text: "who is hosting the summer party for the team"})
	if started, dropped := f.early(other, true, at(2)); started != 2 || dropped != 1 || f.waits(1) {
		t.Fatalf("started %d, dropped %d", started, dropped)
	}
	if _, taken, _ := f.reply(1, madeUp(), nil, at(3)); taken {
		t.Fatal("an answer for a question dropped was taken")
	}
	// A sentence that asks nothing drops it too, and so does a finished
	// question that asks something else.
	if _, dropped := f.early(question{}, false, at(3)); dropped != 2 {
		t.Fatalf("dropped %d", dropped)
	}
	f.early(growing, true, at(4))
	finished, _ := notice(utterance{text: "and who is hosting the summer party?"})
	if n, _, kept := f.ask(finished, at(5)); kept || n != 4 || f.earlyCard() != nil {
		t.Fatalf("card %d, kept %t", n, kept)
	}
	// The same question asked lately isn't asked early.
	again, _ := notice(utterance{text: "so who is hosting the summer party"})
	if started, _ := f.early(again, true, at(6)); started != 0 {
		t.Fatal("a question asked lately was asked early")
	}
	if f.askedEarly != 3 || f.keptEarly != 0 || f.droppedEarly != 3 {
		t.Fatalf("counted %d asked early, %d kept, %d dropped", f.askedEarly, f.keptEarly, f.droppedEarly)
	}
}

func TestAQuestionAskedEarlyThatFailsIsAskedAgainOnlyOnceFinished(t *testing.T) {
	f := listening(Defaults)
	growing, _ := notice(utterance{text: "how much did Halcyon raise"})
	f.early(growing, true, at(1))
	_, n, _ := f.answerDue(at(1))
	f.reply(n, Brief{}, errors.New("On Air has no address for On the Record"), at(1))
	for _, s := range []float64{1, 1.5, 2} {
		if _, _, ok := f.answerDue(at(s)); ok {
			t.Fatalf("asked again at %v while the sentence was still being said", s)
		}
	}
	finished, _ := notice(utterance{text: "how much did Halcyon raise?"})
	if _, _, kept := f.ask(finished, at(3)); !kept {
		t.Fatal("the question asked early wasn't kept")
	}
	if topic, m, ok := f.answerDue(at(3)); !ok || m != n || topic != "how much did Halcyon raise" {
		t.Fatalf("asked %q for %d once finished", topic, m)
	}
	if _, _, ok := f.answerDue(at(4)); ok {
		t.Fatal("asked again")
	}
}

func TestTheControllerAsksAQuestionBeforeItIsFinished(t *testing.T) {
	screen := &logScreen{}
	var mu sync.Mutex
	var asked []time.Time
	cancelled := make(chan string, 4)
	archive := &fakeArchive{
		answer: func(context.Context, string) (Found, error) { return Found{}, nil },
		brief: func(ctx context.Context, q string) (Brief, error) {
			mu.Lock()
			asked = append(asked, time.Now())
			mu.Unlock()
			select {
			case <-time.After(150 * time.Millisecond):
				return madeUp(), nil
			case <-ctx.Done():
				cancelled <- q
				return Brief{}, ctx.Err()
			}
		},
	}
	cfg := fast
	cfg.Timeout = 5 * time.Second
	c := Start(cfg, archive, screen, time.Now, nil, nil)
	defer c.Enabled(false)
	c.Enabled(true)
	c.Capturing("", true)
	c.Recording(true)
	waitFor(t, "listening", func() bool { _, l := screen.said(); return len(l) == 1 && l[0] })
	began := time.Now()
	partial := func(req int, sound float64, text string) {
		s := Said{Began: began, Request: req, Partial: true, Step: 100 * time.Millisecond, Mic: make([]float64, int(sound*10)), Call: make([]float64, int(sound*10))}
		for i, w := range strings.Fields(text) {
			s.Segments = append(s.Segments, Segment{Text: w, At: time.Duration(i) * 350 * time.Millisecond, Length: 250 * time.Millisecond})
		}
		c.Said(s)
	}
	// A question that turns into another: the first asked early is given
	// up on.
	partial(1, 1.5, "who is hosting the")
	partial(1, 2.2, "who is hosting the summer party")
	waitFor(t, "the first asked early", func() bool { mu.Lock(); defer mu.Unlock(); return len(asked) == 1 })
	partial(1, 3.0, "how much did Priya share with Halcyon")
	if q := <-cancelled; q != "who is hosting the summer party" {
		t.Fatalf("gave up on %q", q)
	}
	waitFor(t, "the second asked early", func() bool { mu.Lock(); defer mu.Unlock(); return len(asked) == 2 })
	if len(screen.last().Answers) != 0 {
		t.Fatalf("a question still being asked shows %+v", screen.last().Answers)
	}
	// Finished by a pause: its card shows, with what was found for it, and
	// On the Record isn't asked again.
	partial(1, 3.2, "how many mockups did Priya share")
	waitFor(t, "the third asked early", func() bool { mu.Lock(); defer mu.Unlock(); return len(asked) == 3 })
	waitFor(t, "its answer, kept", func() bool {
		screen.mu.Lock()
		defer screen.mu.Unlock()
		return strings.Contains(strings.Join(screen.lines, "\n"), "kept for when its sentence is finished")
	})
	partial(1, 4.0, "how many mockups did Priya share")
	partial(1, 4.25, "how many mockups did Priya share")
	waitFor(t, "an answer", func() bool { a := screen.last().Answers; return len(a) == 1 && len(a[0].Lines) > 0 })
	if a := screen.last().Answers[0]; a.Question != "How many mockups did Priya share?" || a.Lines[0].Text != "Priya clicked through 9 mockups for the team" {
		t.Fatalf("the card shows %+v", a)
	}
	c.Said(Said{Began: began, Request: 1, Step: 100 * time.Millisecond, Segments: []Segment{{Text: "how", At: 0, Length: 250 * time.Millisecond}}})
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	n := len(asked)
	mu.Unlock()
	if n != 3 {
		t.Fatalf("On the Record was asked %d times", n)
	}
	screen.mu.Lock()
	lines := strings.Join(screen.lines, "\n")
	screen.mu.Unlock()
	for _, said := range []string{"priya", "mockups", "halcyon", "party", "hosting"} {
		if strings.Contains(strings.ToLower(lines), said) {
			t.Fatalf("the log holds what was said or answered:\n%s", lines)
		}
	}
	for _, want := range []string{"question 1 asked early: 6 words so far", "question 1, asked early, dropped", "question 3 asked early", "answer 3 asked, 6 words, 0 of them the local model's, early",
		"kept for when its sentence is finished", "question 3 noticed: 6 words, 3 key words, someone else's, its sentence ended by a pause", "asked early, and what was found for it kept",
		"card 3 shown ", "before the question was finished", "request 1: 1 sentences finished as they were said"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("the log doesn't say %q:\n%s", want, lines)
		}
	}
}
