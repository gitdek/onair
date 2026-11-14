package live

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// judging is a follow that is on, recording, and has a judge, as from t0.
func judging() *follow {
	f := listening(Defaults)
	f.judge(true)
	return f
}

func roundAt(t *testing.T, f *follow, now time.Time) (Round, int) {
	t.Helper()
	f.hear(Heard{Speaker: Others, Text: words(Defaults.Least, "word"), To: now}, now)
	r, n, ok := f.roundDue(now)
	if !ok {
		t.Fatalf("no round at %v", now)
	}
	return r, n
}

func TestARoundWaitsForNewWordsAndItsTime(t *testing.T) {
	f := listening(Defaults)
	f.hear(Heard{Speaker: Others, Text: words(Defaults.Least, "word"), To: at(1)}, at(1))
	if _, _, ok := f.roundDue(at(1)); ok {
		t.Fatal("a round without a judge")
	}
	f.judge(true)
	r, n, ok := f.roundDue(at(1))
	if !ok || n == 0 || r.Text == "" {
		t.Fatalf("no round with a judge: %+v %v", r, ok)
	}
	f.hear(Heard{Speaker: Person, Text: "more", To: at(2)}, at(2))
	if _, _, ok := f.roundDue(at(20)); ok {
		t.Fatal("a round while one waited")
	}
	f.judgement(n, nil, Verdict{}, nil, at(3))
	if _, _, ok := f.roundDue(at(5)); ok {
		t.Fatal("a round before its time")
	}
	if _, _, ok := f.roundDue(at(11)); !ok {
		t.Fatal("no round once its time came with something new said")
	}
}

func TestADecisionAndADifferenceShowWhenTheyClearTheirThresholds(t *testing.T) {
	f := judging()
	f.answer(asked(t, f, at(10)), strong("m1"), nil, at(10))
	r, n := roundAt(t, f, at(11))
	if len(r.Records) != 1 || r.Records[0].Title != "Meeting m1" {
		t.Fatalf("a round asked about %v", r.Records)
	}
	v := Verdict{Decision: 0.9, Stance: map[string]float64{"agreed": 0.7, "none": 0.2}, Differs: 0.8, Compared: true, Topic: 0.1}
	f.judgement(n, r.Records, v, nil, at(12))
	p := f.panel(at(12))
	if strings.Join(p.Calls, " | ") != "Decision: agreed, at 15:00. | Differs from: Meeting m1 (2026-11-01)." || len(p.Links) != 1 {
		t.Fatalf("a verdict shows %v and %v", p.Calls, p.Links)
	}
	// Below the thresholds, nothing new shows, and what showed stays a
	// while.
	_, n = roundAt(t, f, at(30))
	f.judgement(n, nil, Verdict{Decision: 0.69, Stance: map[string]float64{"agreed": 0.9}, Differs: 0.9}, nil, at(31))
	if p := f.panel(at(31)); len(p.Calls) != 2 {
		t.Fatalf("after a weak verdict the panel shows %v", p.Calls)
	}
	if p := f.panel(at(12 + 120)); len(p.Calls) != 0 {
		t.Fatalf("two minutes on the panel still shows %v", p.Calls)
	}
	// A stance that doesn't clear its threshold, or none at all, isn't said.
	g := judging()
	_, n = roundAt(t, g, at(1))
	g.judgement(n, nil, Verdict{Decision: 0.95, Stance: map[string]float64{"agreed": 0.4, "proposed": 0.35, "none": 0.25}}, nil, at(2))
	_, n = roundAt(t, g, at(20))
	g.judgement(n, nil, Verdict{Decision: 0.95, Stance: map[string]float64{"none": 0.9}}, nil, at(21))
	if p := g.panel(at(21)); len(p.Calls) != 0 {
		t.Fatalf("an unsure stance shows %v", p.Calls)
	}
	// A newer decision replaces the older one.
	_, n = roundAt(t, g, at(40))
	g.judgement(n, nil, Verdict{Decision: 0.8, Stance: map[string]float64{"proposed": 0.6}}, nil, at(41))
	_, n = roundAt(t, g, at(60))
	g.judgement(n, nil, Verdict{Decision: 0.8, Stance: map[string]float64{"taken back": 0.6}}, nil, at(61))
	if p := g.panel(at(61)); strings.Join(p.Calls, " | ") != "Decision: taken back, at 15:01." {
		t.Fatalf("two decisions show %v", p.Calls)
	}
}

func TestAChangeOfTopicClearsThePanelAndStartsAFreshMinute(t *testing.T) {
	f := judging()
	f.answer(asked(t, f, at(10)), strong("m1"), nil, at(10))
	_, n := roundAt(t, f, at(11))
	f.judgement(n, nil, Verdict{Decision: 0.9, Stance: map[string]float64{"agreed": 0.9}}, nil, at(11))
	_, n = roundAt(t, f, at(30))
	f.judgement(n, nil, Verdict{Topic: 0.85}, nil, at(31))
	p := f.panel(at(31))
	if len(p.Links) != 0 || len(p.Calls) != 0 || p.Status != "Listening…" {
		t.Fatalf("after the topic changed the panel shows %+v", p)
	}
	if f.text() != "" {
		t.Fatalf("after the topic changed the window holds %q", f.text())
	}
	if _, _, ok := f.due(at(60)); ok {
		t.Fatal("asked On the Record with nothing said since the topic changed")
	}
}

func TestWhatGoesWrongWithTheModelIsSaidInThePanel(t *testing.T) {
	f := judging()
	_, n := roundAt(t, f, at(1))
	f.judgement(n, nil, Verdict{}, errors.New("the local model is still loading"), at(2))
	if p := f.panel(at(2)); !strings.Contains(p.Status, "Couldn't ask the model: the local model is still loading.") {
		t.Fatalf("a model that failed shows %q", p.Status)
	}
	_, n = roundAt(t, f, at(20))
	f.abandonRound(at(20).Add(Defaults.JudgeTimeout + 1001*time.Millisecond))
	if p := f.panel(at(24)); !strings.Contains(p.Status, "Couldn't ask the model: it didn't answer within 2s.") {
		t.Fatalf("a round given up on shows %q", p.Status)
	}
	f.judgement(n, nil, Verdict{Topic: 1}, nil, at(25))
	if p := f.panel(at(25)); p.Status == "Listening…" {
		t.Fatal("a late verdict was taken")
	}
	f.judge(false)
	if p := f.panel(at(26)); strings.Contains(p.Status, "model") || len(p.Calls) != 0 {
		t.Fatalf("with no judge the panel shows %+v", p)
	}
}

// fakeJudge answers as verdict says, and counts its closes.
type fakeJudge struct {
	mu      sync.Mutex
	rounds  []Round
	closes  int
	verdict func(ctx context.Context, r Round) (Verdict, error)
}

func (j *fakeJudge) Judge(ctx context.Context, r Round) (Verdict, error) {
	j.mu.Lock()
	j.rounds = append(j.rounds, r)
	verdict := j.verdict
	j.mu.Unlock()
	return verdict(ctx, r)
}
func (j *fakeJudge) Close() { j.mu.Lock(); j.closes++; j.mu.Unlock() }
func (j *fakeJudge) counts() (rounds, closes int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.rounds), j.closes
}

func TestTheControllerAsksTheChosenJudgeAndClosesItWhenUnchosen(t *testing.T) {
	screen := &fakeScreen{}
	judge := &fakeJudge{verdict: func(context.Context, Round) (Verdict, error) {
		return Verdict{Decision: 0.9, Stance: map[string]float64{"agreed": 0.8}}, nil
	}}
	cfg := fast
	cfg.JudgeEvery, cfg.JudgeTimeout, cfg.Hold = 20*time.Millisecond, 50*time.Millisecond, time.Minute
	c := Start(cfg, &fakeArchive{answer: func(context.Context, string) (Found, error) { return Found{}, nil }}, screen, time.Now, map[string]Judge{"local": judge})
	c.Enabled(true)
	c.Capturing("", true)
	c.Recording(true)
	c.Heard(Heard{Speaker: Person, Text: "we agree on Thursday", From: time.Now(), To: time.Now()})
	time.Sleep(50 * time.Millisecond)
	if rounds, _ := judge.counts(); rounds != 0 {
		t.Fatal("a judge was asked before it was chosen")
	}
	c.Judging("local")
	c.Heard(Heard{Speaker: Others, Text: "yes Thursday", From: time.Now(), To: time.Now()})
	waitFor(t, "the decision in the panel", func() bool { return len(screen.last().Calls) == 1 })
	if !strings.HasPrefix(screen.last().Calls[0], "Decision: agreed, at ") {
		t.Fatalf("the panel shows %v", screen.last().Calls)
	}
	c.Judging("")
	waitFor(t, "the judge to close", func() bool { _, closes := judge.counts(); return closes == 1 })
	waitFor(t, "the decision to go", func() bool { return len(screen.last().Calls) == 0 })
	c.Judging("local")
	before, _ := judge.counts()
	c.Heard(Heard{Speaker: Person, Text: "and Friday for the review", From: time.Now(), To: time.Now()})
	waitFor(t, "the judge to be asked again", func() bool { rounds, _ := judge.counts(); return rounds > before })
	c.Enabled(false)
	waitFor(t, "the judge to close with Live Context off", func() bool { _, closes := judge.counts(); return closes == 2 })
}

func TestAJudgeThatFailsIsSaidInThePanelOnly(t *testing.T) {
	never := make(chan struct{})
	defer close(never)
	cfg := fast
	cfg.JudgeEvery, cfg.JudgeTimeout = 20*time.Millisecond, 50*time.Millisecond
	for name, tc := range map[string]struct {
		verdict func(ctx context.Context, r Round) (Verdict, error)
		says    string
	}{
		"slow":   {func(ctx context.Context, _ Round) (Verdict, error) { <-ctx.Done(); return Verdict{}, ctx.Err() }, "Couldn't ask the model: context deadline exceeded."},
		"never":  {func(context.Context, Round) (Verdict, error) { <-never; return Verdict{}, nil }, "Couldn't ask the model: it didn't answer within 50ms."},
		"panics": {func(context.Context, Round) (Verdict, error) { panic("the model broke") }, "Couldn't ask the model: the model failed: the model broke."},
	} {
		t.Run(name, func(t *testing.T) {
			screen := &fakeScreen{}
			c := Start(cfg, &fakeArchive{answer: func(context.Context, string) (Found, error) { return Found{}, nil }}, screen, time.Now, map[string]Judge{"local": &fakeJudge{verdict: tc.verdict}})
			c.Enabled(true)
			c.Judging("local")
			c.Capturing("", true)
			c.Recording(true)
			stop := make(chan struct{})
			defer close(stop)
			say(c, stop)
			waitFor(t, "the panel to say what went wrong", func() bool { return strings.Contains(screen.last().Status, tc.says) })
			if n := c.judging.Load(); n > inFlight {
				t.Fatalf("%d rounds out at once", n)
			}
		})
	}
}
