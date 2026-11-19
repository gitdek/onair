package live_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gitdek/onair/internal/live"
	"github.com/gitdek/onair/internal/localmodel"
)

// fictional is a made-up brief about a made-up company, about as long as
// the archive's: 25 lines of four meetings' notes.
func fictional() live.Brief {
	var b live.Brief
	add := func(meeting, date, under, at, text string) {
		b.Points = append(b.Points, live.Point{Text: text, Under: under, Meeting: meeting, Date: date, At: at, URL: "https://archive.example/m/" + date + "?t=" + at})
	}
	for _, l := range [][4]string{
		{"Investors · Discussion", "6:12", "Halcyon hopes to close a $750k pre-seed round before the end of the year, led by two angel investors who already know the founders"},
		{"Investors · Discussion", "11:40", "Halcyon will hire three engineers next quarter once the round closes, starting with a mobile developer"},
		{"Next steps", "", "A follow-up with the partners is planned for the first week of October to go over the term sheet"},
		{"Pricing", "15:02", "The team settled on $12 a seat per month for the starter plan, with a discount for annual billing"},
		{"Pricing", "16:30", "Enterprise pricing stays custom and is quoted by the sales lead after a call"},
	} {
		add("Halcyon partner sync", "2026-09-14", l[0], l[1], l[2])
	}
	for _, l := range [][4]string{
		{"Release timeline", "1:05:09", "Doors open to the public on the 9th of December, after a two week private beta with forty testers"},
		{"Release timeline", "1:06:30", "The press kit still needs a logo and three screenshots before the public opening"},
		{"Walkthrough", "4:21", "Priya clicked through 9 mockups for the team, covering sign up, the dashboard and settings"},
		{"Requests", "18:30", "Priya listed the features she needs next: offline sync, a calendar view and shared folders"},
		{"Requests", "19:10", "Voice notes were asked for again, but the team left them for next year"},
		{"Decisions", "40:02", "The team agreed to drop the Android tablet layout from the first release"},
		{"Decisions", "41:15", "Support will run through email only until the public opening"},
		{"Risks", "47:30", "The payment provider review may take three weeks, which could push the paid plans back"},
	} {
		add("Halcyon product walkthrough", "2026-10-08", l[0], l[1], l[2])
	}
	for _, l := range [][4]string{
		{"Hiring", "2:10", "Marco accepted the offer for the mobile developer role and starts on November 3"},
		{"Hiring", "3:05", "The second engineering role is still open, with five candidates in the pipeline"},
		{"Metrics", "8:40", "The waitlist reached 2,300 sign ups, up from 1,400 at the start of the month"},
		{"Metrics", "9:15", "Weekly active testers held at about 35 of the 40 in the private beta"},
		{"Marketing", "14:00", "The launch post will go out on the company blog and two newsletters on opening day"},
		{"Infrastructure", "20:20", "Hosting moves to a second region before the opening to keep the app up during an outage"},
		{"Budget", "25:45", "Monthly cloud spend is about $4,800 and should stay under $6,000 after the opening"},
	} {
		add("Halcyon weekly", "2026-10-21", l[0], l[1], l[2])
	}
	for _, l := range [][4]string{
		{"Updates", "0:55", "Tomas demoed the Beacon assistant to the board, who asked for a clearer privacy page"},
		{"Updates", "7:45", "Tomas says the Beacon assistant waits on a new model before its next release"},
		{"Partnership", "12:00", "Beacon and Halcyon talked about a shared calendar integration, with no date set"},
		{"Next steps", "30:10", "Tomas will send the integration outline to Priya by the end of next week"},
		{"Open questions", "33:00", "Nobody has decided who owns the shared customer list between the two companies"},
	} {
		add("Beacon check-in", "2026-10-30", l[0], l[1], l[2])
	}
	b.Passages = 8
	return b
}

// TestTheRealWriter runs the real writing model, from the environment
// tools/setup-models makes, on made-up notes and made-up words, when
// ONAIR_LOCAL_MODEL_PYTHON names its Python, and times it. It is skipped
// otherwise, as in CI.
func TestTheRealWriter(t *testing.T) {
	python := os.Getenv("ONAIR_LOCAL_MODEL_PYTHON")
	if python == "" {
		t.Skip("ONAIR_LOCAL_MODEL_PYTHON isn't set")
	}
	w := localmodel.NewWriter(python)
	defer w.Close()
	write := func(p live.Prompt, timeout time.Duration) (string, time.Duration) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		began := time.Now()
		text, err := w.Write(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		return text, time.Since(began)
	}
	b := fictional()
	began := time.Now()
	w.Warm()
	p, _ := live.AnswerFor("how much were they looking to raise for Halcyon", b)
	write(live.Prompt{System: p.System, User: "Notes:\n[1] made up\n\nQuestion: is it loaded?", Most: 4}, 2*time.Minute)
	t.Logf("loaded in %v", time.Since(began))
	slowest := time.Duration(0)
	for _, tc := range []struct{ said, has, cites, result string }{
		{"how much were they looking to raise for Halcyon", "$750k", "Halcyon hopes to close a $750k pre-seed round", "answered"},
		{"what day are we shipping it", "9", "Doors open to the public on the 9th of December", "answered"},
		{"what features is Halcyon planning for the next version", "offline sync", "Priya listed the features she needs next", "answered"},
		{"what did the lawyers say about the office lease", "", "", "not in the archive"},
	} {
		p, read := live.AnswerFor(tc.said, b)
		text, took := write(p, 30*time.Second)
		answer, cited, result := read(text)
		t.Logf("%q in %v, %d prompt characters: %s %q", tc.said, took, len(p.System+p.User), result, answer)
		slowest = max(slowest, took)
		if result != tc.result || !strings.Contains(answer, tc.has) || tc.cites != "" && (len(cited) == 0 || !strings.HasPrefix(cited[0].Text, tc.cites)) {
			t.Errorf("%q: %s %q citing %+v, from %q", tc.said, result, answer, cited, text)
		}
	}
	for _, tc := range []struct {
		said  []string
		shown string
	}{
		{[]string{"Honestly the weather has been great this week.", "So Halcyon is closing a two million dollar seed round next month."}, "the record says “Halcyon hopes to close a $750k pre-seed round"},
		{[]string{"Halcyon is still aiming for the $750k pre-seed round.", "And the waitlist is past two thousand now."}, ""},
	} {
		p, read := live.CheckFor(tc.said, b)
		text, took := write(p, 30*time.Second)
		shown, result := read(text)
		t.Logf("a check in %v, %d prompt characters: %s %q", took, len(p.System+p.User), result, shown)
		slowest = max(slowest, took)
		if !strings.Contains(shown, tc.shown) || tc.shown == "" && shown != "" {
			t.Errorf("%q: %s %q, from %q", tc.said, result, shown, text)
		}
	}
	// A question whose answer sits under a heading on the roadmap, in
	// words other than the question's: reworded, and answered from about
	// 2,200 tokens of lines.
	road := live.Roadmap()
	for _, said := range []string{"what features did Rafael want in the new version", "how much did Kestrel raise", "when does the new Kestrel version go out"} {
		p, read := live.RewordFor(said)
		text, took := write(p, 30*time.Second)
		added, topic := read(text)
		t.Logf("%q reworded in %v: %q, so On the Record is asked %q", said, took, added, topic)
		slowest = max(slowest, took)
		if took > time.Second {
			t.Errorf("%q took %v to reword", said, took)
		}
	}
	p, _ = live.RewordFor("what features did Rafael want in the new version")
	text, _ := write(p, 30*time.Second)
	ap, read := live.AnswerFor("what features did Rafael want in the new version", road, text)
	answer, took := write(ap, 30*time.Second)
	shown, cited, result := read(answer)
	t.Logf("the features question in %v, %d prompt characters: %s %q citing %d lines", took, len(ap.System+ap.User), result, shown, len(cited))
	if result != "answered" || len(cited) == 0 || !strings.Contains(cited[0].Under, "Capabilities") {
		t.Errorf("the features question: %s %q citing %+v, from %q", result, shown, cited, answer)
	}
	slowest = max(slowest, took)
	if slowest > 5*time.Second {
		t.Errorf("the slowest took %v, once loaded", slowest)
	}
}

// timedScreen is the Mac as the live side sees it, keeping when each panel
// was shown, and the diagnostic log.
type timedScreen struct {
	mu     sync.Mutex
	shown  []time.Time
	panels []live.Panel
	lines  []string
}

func (s *timedScreen) Ask()                       {}
func (s *timedScreen) Listen(bool, time.Duration) {}
func (s *timedScreen) Log(line string)            { s.mu.Lock(); s.lines = append(s.lines, line); s.mu.Unlock() }
func (s *timedScreen) Show(p live.Panel) {
	s.mu.Lock()
	s.shown, s.panels = append(s.shown, time.Now()), append(s.panels, p)
	s.mu.Unlock()
}
func (s *timedScreen) first(ok func(live.Answer) bool) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.panels {
		if len(p.Answers) > 0 && ok(p.Answers[0]) {
			return s.shown[i]
		}
	}
	return time.Time{}
}

type slowArchive struct {
	took time.Duration
	mu   sync.Mutex
	at   []time.Time
}

func (a *slowArchive) Connections(ctx context.Context, text string) (live.Found, error) {
	return live.Found{}, nil
}
func (a *slowArchive) Answer(ctx context.Context, question string, transcript bool) (live.Brief, error) {
	a.mu.Lock()
	a.at = append(a.at, time.Now())
	a.mu.Unlock()
	select {
	case <-time.After(a.took):
		return live.Roadmap(), nil
	case <-ctx.Done():
		return live.Brief{}, ctx.Err()
	}
}

// noReword is a writer that writes answers but rewords nothing, as before
// questions were reworded.
type noReword struct{ live.Writer }

func (w noReword) Write(ctx context.Context, p live.Prompt) (string, error) {
	if p.Quick {
		return "", errors.New("not reworded")
	}
	return w.Writer.Write(ctx, p)
}

// TestHowLongAnAnswerTakes runs the live side on this Mac with the real
// writing model, when ONAIR_LOCAL_MODEL_PYTHON names its Python, and a
// made-up On the Record that takes as long as the owner's did in a call,
// 1.8 to 5.0 s, and times a made-up question from its last word to its
// answer on the panel. After: partial results come as recognition would
// send them, each word three tenths of a second after it ends, and how far
// the sound has gone four times a second. Before: the final result comes at
// once after the question, as it did only when the question ended its
// request's fifteen seconds, which on average was 7.5 s later; and the
// question isn't reworded, and the writer reads 1,500 tokens.
func TestHowLongAnAnswerTakes(t *testing.T) {
	python := os.Getenv("ONAIR_LOCAL_MODEL_PYTHON")
	if python == "" {
		t.Skip("ONAIR_LOCAL_MODEL_PYTHON isn't set")
	}
	w := localmodel.NewWriter(python)
	defer w.Close()
	w.Warm()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	if _, err := w.Write(ctx, live.Prompt{System: "Reply with one word.", User: "Ready?", Most: 2}); err != nil {
		t.Fatal(err)
	}
	cancel()
	question := "So what features did Rafael want in the new version?"
	for _, before := range []bool{true, false} {
		for _, took := range []time.Duration{1800 * time.Millisecond, 3300 * time.Millisecond, 5 * time.Second} {
			screen := &timedScreen{}
			archive := &slowArchive{took: took}
			cfg := live.Defaults
			var writer live.Writer = w
			if before {
				cfg.AnswerTokens, writer = 1500, noReword{w}
			}
			c := live.Start(cfg, archive, screen, time.Now, nil, map[string]live.Writer{"local": writer})
			c.Judging("local")
			c.Enabled(true)
			c.Capturing("", true)
			c.Recording(true)
			time.Sleep(300 * time.Millisecond)
			began := time.Now()
			words := strings.Fields(question)
			segments := func(n int) []live.Segment {
				var out []live.Segment
				for i, w := range words[:n] {
					out = append(out, live.Segment{Text: w, At: time.Duration(i) * 350 * time.Millisecond, Length: 250 * time.Millisecond})
				}
				return out
			}
			end := began.Add(time.Duration(len(words)-1)*350*time.Millisecond + 250*time.Millisecond)
			said := func(partial bool, n int) {
				steps := int(time.Since(began)/(100*time.Millisecond)) - 1
				c.Said(live.Said{Began: began, Request: 1, Partial: partial, Segments: segments(n), Step: 100 * time.Millisecond, Mic: make([]float64, max(steps, 0)), Call: make([]float64, max(steps, 0))})
			}
			if before {
				time.Sleep(time.Until(end.Add(100 * time.Millisecond)))
				said(false, len(words))
			} else {
				heard, sent := 0, time.Time{}
				for time.Since(end) < 3*time.Second && screen.first(func(live.Answer) bool { return true }).IsZero() {
					n := 0
					for i := range words {
						if time.Since(began) >= time.Duration(i)*350*time.Millisecond+550*time.Millisecond {
							n = i + 1
						}
					}
					if n != heard || time.Since(sent) >= 250*time.Millisecond {
						said(true, n)
						heard, sent = n, time.Now()
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			deadline := time.Now().Add(20 * time.Second)
			var answered time.Time
			for answered.IsZero() && time.Now().Before(deadline) {
				answered = screen.first(func(a live.Answer) bool { return a.Text != "" || a.Note != "" && !strings.HasSuffix(a.Note, "…") })
				time.Sleep(20 * time.Millisecond)
			}
			// Stopping the recording leaves the writer warm for the next run,
			// as a call goes on.
			c.Recording(false)
			noticed := screen.first(func(live.Answer) bool { return true })
			var steps string
			screen.mu.Lock()
			for _, l := range screen.lines {
				if strings.HasPrefix(l, "card ") {
					steps = l
				}
				if strings.HasPrefix(l, "write ") || strings.HasPrefix(l, "reword ") || strings.HasPrefix(l, "answer ") {
					t.Logf("  %s", l)
				}
			}
			final := screen.panels[len(screen.panels)-1]
			screen.mu.Unlock()
			mode := "after"
			if before {
				mode = "before (plus the wait for the request's end, 7.5 s on average)"
			}
			t.Logf("%s, On the Record taking %v: card shown %.1f s after the question's last word, answer %.1f s after it; %s", mode, took, noticed.Sub(end).Seconds(), answered.Sub(end).Seconds(), steps)
			if len(final.Answers) > 0 {
				t.Logf("  answer: %q %q", final.Answers[0].Text, final.Answers[0].Note)
			}
			if answered.IsZero() {
				t.Errorf("%s: no answer", mode)
			}
		}
	}
}
