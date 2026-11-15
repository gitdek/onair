package live

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fast is Defaults made quick enough to watch.
var fast = Config{Window: time.Minute, Every: 20 * time.Millisecond, Least: 2, Chars: 4000, Most: 3, Quiet: time.Minute,
	Timeout: 50 * time.Millisecond, Chunk: 15 * time.Second, Linger: time.Minute, Tick: 5 * time.Millisecond}

// fakeScreen is the Mac, as the live side sees it.
type fakeScreen struct {
	mu      sync.Mutex
	asks    int
	listens []bool
	panels  []Panel
	// hang, when set, holds Show and Listen until it is closed; broken
	// has them panic.
	hang   chan struct{}
	broken bool
}

func (s *fakeScreen) Ask() { s.mu.Lock(); s.asks++; s.mu.Unlock() }
func (s *fakeScreen) Listen(on bool, chunk time.Duration) {
	s.trouble()
	s.mu.Lock()
	s.listens = append(s.listens, on)
	s.mu.Unlock()
}
func (s *fakeScreen) Show(p Panel) {
	s.trouble()
	s.mu.Lock()
	s.panels = append(s.panels, p)
	s.mu.Unlock()
}
func (s *fakeScreen) trouble() {
	if s.hang != nil {
		<-s.hang
	}
	if s.broken {
		panic("the panel broke")
	}
}
func (s *fakeScreen) last() Panel {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.panels) == 0 {
		return Panel{}
	}
	return s.panels[len(s.panels)-1]
}
func (s *fakeScreen) said() (asks int, listens []bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asks, append([]bool(nil), s.listens...)
}

// fakeArchive is On the Record, answering as answer says.
type fakeArchive struct {
	mu     sync.Mutex
	asked  []string
	answer func(ctx context.Context, text string) (Found, error)
}

func (a *fakeArchive) Connections(ctx context.Context, text string) (Found, error) {
	a.mu.Lock()
	a.asked = append(a.asked, text)
	answer := a.answer
	a.mu.Unlock()
	return answer(ctx, text)
}
func (a *fakeArchive) questions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.asked...)
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("waited 5s for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// say keeps something new being said, until stop is closed.
func say(c *Controller, stop chan struct{}) {
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(3 * time.Millisecond):
			}
			c.Heard(Heard{Speaker: Speaker(1 + i%2), Text: "the launch date and the price", From: time.Now(), To: time.Now()})
		}
	}()
}

func TestTheControllerAsksAndShowsWhatTheCallConnectsTo(t *testing.T) {
	screen := &fakeScreen{}
	archive := &fakeArchive{answer: func(context.Context, string) (Found, error) { return strong("m1"), nil }}
	c := Start(fast, archive, screen, time.Now, nil)
	c.Enabled(true)
	waitFor(t, "the question about speech recognition", func() bool { asks, _ := screen.said(); return asks == 1 })
	if _, listens := screen.said(); len(listens) != 0 {
		t.Fatalf("listening before a recording: %v", listens)
	}
	c.Capturing("display-2", true)
	c.Recording(true)
	waitFor(t, "listening", func() bool { _, l := screen.said(); return len(l) == 1 && l[0] })
	if p := screen.last(); !p.Visible || p.Display != "display-2" || p.Status != "Listening…" {
		t.Fatalf("a recording with Live Context on shows %+v", p)
	}
	c.Heard(Heard{Speaker: Person, Text: "the launch date", From: time.Now(), To: time.Now()})
	c.Heard(Heard{Speaker: Others, Text: "and the price", From: time.Now(), To: time.Now()})
	waitFor(t, "a link", func() bool { return len(screen.last().Links) == 1 })
	if p := screen.last(); p.Links[0].Title != "Meeting m1" || p.Status != "Heard 6 words in the last minute, 3 of them yours." {
		t.Fatalf("after an answer the panel shows %+v", p)
	}
	if q := archive.questions(); len(q) != 1 || q[0] != "the launch date\nand the price" {
		t.Fatalf("asked %q", q)
	}
	// Nothing new said, nothing asked.
	time.Sleep(10 * fast.Every)
	if q := archive.questions(); len(q) != 1 {
		t.Fatalf("asked again with nothing new said: %q", q)
	}
	c.Recording(false)
	waitFor(t, "listening to stop", func() bool { _, l := screen.said(); return len(l) == 2 && !l[1] })
	if p := screen.last(); !p.Visible || len(p.Links) != 1 || !strings.HasPrefix(p.Status, "The recording stopped.") {
		t.Fatalf("after the recording the panel shows %+v", p)
	}
	c.Enabled(false)
	waitFor(t, "the panel to hide", func() bool { return !screen.last().Visible })
}

func TestWhatGoesWrongAskingIsSaidInThePanelOnly(t *testing.T) {
	never := make(chan struct{})
	defer close(never)
	for name, tc := range map[string]struct {
		answer     func(ctx context.Context, text string) (Found, error)
		nilArchive bool
		says       string
	}{
		"down": {answer: func(context.Context, string) (Found, error) {
			return Found{}, errors.New("On the Record isn't answering on this Mac. Is its server running?")
		}, says: "Couldn't ask On the Record: On the Record isn't answering on this Mac. Is its server running?"},
		"slow": {answer: func(ctx context.Context, _ string) (Found, error) { <-ctx.Done(); return Found{}, ctx.Err() },
			says: "Couldn't ask On the Record: context deadline exceeded."},
		"never answers": {answer: func(context.Context, string) (Found, error) { <-never; return Found{}, nil },
			says: "Couldn't ask On the Record: it didn't answer within 50ms."},
		"panics": {answer: func(context.Context, string) (Found, error) { panic("the archive broke") },
			says: "Couldn't ask On the Record: asking On the Record failed: the archive broke."},
		"no address": {nilArchive: true, says: "Couldn't ask On the Record: On Air has no address for On the Record."},
	} {
		t.Run(name, func(t *testing.T) {
			screen := &fakeScreen{}
			var c *Controller
			archive := &fakeArchive{answer: tc.answer}
			if tc.nilArchive {
				c = Start(fast, nil, screen, time.Now, nil)
			} else {
				c = Start(fast, archive, screen, time.Now, nil)
			}
			c.Enabled(true)
			c.Capturing("", true)
			c.Recording(true)
			stop := make(chan struct{})
			defer close(stop)
			say(c, stop)
			waitFor(t, "the panel to say what went wrong", func() bool { return strings.Contains(screen.last().Status, tc.says) })
			// Questions that never come back are never more than two.
			if n := c.out.Load(); n > inFlight {
				t.Fatalf("%d questions out at once", n)
			}
			if p := screen.last(); !p.Visible {
				t.Fatalf("the panel hid: %+v", p)
			}
		})
	}
}

func TestABrokenScreenStopsTheControllerAndNothingElse(t *testing.T) {
	screen := &fakeScreen{broken: true}
	c := Start(fast, &fakeArchive{answer: func(context.Context, string) (Found, error) { return strong("m1"), nil }}, screen, time.Now, nil)
	c.Enabled(true)
	c.Recording(true)
	waitFor(t, "the controller to stop", c.stopped.Load)
	start := time.Now()
	for i := 0; i < 10000; i++ {
		c.Heard(Heard{Speaker: Person, Text: "still talking"})
		c.Recording(i%2 == 0)
		c.Report(SpeechFailed, "again")
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("inputs to a stopped controller took %v", took)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.heard) > waitingHeard || len(c.reports) > waitingReports || c.lost == 0 {
		t.Fatalf("a stopped controller holds %d pieces and %d reports, and dropped %d", len(c.heard), len(c.reports), c.lost)
	}
}

func TestInputsNeverWaitForAControllerThatHangs(t *testing.T) {
	screen := &fakeScreen{hang: make(chan struct{})}
	defer close(screen.hang)
	c := Start(fast, &fakeArchive{answer: func(context.Context, string) (Found, error) { return Found{}, nil }}, screen, time.Now, nil)
	c.Enabled(true)
	c.Recording(true)
	time.Sleep(20 * time.Millisecond)
	start := time.Now()
	long := strings.Repeat("word ", 100000)
	for i := 0; i < 100000; i++ {
		c.Heard(Heard{Speaker: Others, Text: long})
		c.Report(SoundDropped, long)
		c.Capturing("display", i%2 == 0)
		c.Enabled(true)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("inputs to a hung controller took %v", took)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	held := 0
	for _, h := range c.heard {
		held += len(h.Text)
	}
	if len(c.heard) > waitingHeard || len(c.reports) > waitingReports || held > waitingHeard*fast.Chars {
		t.Fatalf("a hung controller holds %d pieces of %d bytes and %d reports", len(c.heard), held, len(c.reports))
	}
}

func TestANilControllerTakesInputs(t *testing.T) {
	var c *Controller
	c.Enabled(true)
	c.Capturing("display", true)
	c.Recording(true)
	c.Heard(Heard{Text: "words"})
	c.Report(SpeechRefused, "")
}
