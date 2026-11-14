package live

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

// What waits for the controller is bounded, so a controller that has
// stopped, or fallen behind, never holds more than this.
const (
	waitingHeard   = 64 // finished pieces of text; the oldest go first
	waitingReports = 16 // reports from the Mac; likewise
	inFlight       = 2  // questions to On the Record not yet returned
	panicsAllowed  = 3  // panics the controller recovers from before it stops
)

// Controller runs the live side on goroutines of its own. Its inputs,
// Enabled, Capturing, Recording, Heard and Report, return at once from any
// goroutine: they set a field or add to a bounded queue, and wake it.
// It asks On the Record through an Archive, and has the Mac listen and
// show the panel through a Screen.
type Controller struct {
	cfg     Config
	archive Archive
	screen  Screen
	now     func() time.Time

	mu        sync.Mutex // guards what the inputs set, and is held only to set or take it
	on        bool
	recording bool
	judge     string // the judge chosen, by name, or none
	display   string
	leftOut   bool
	heard     []Heard
	reports   []report
	lost      int // inputs dropped because their queue was full

	wake    chan struct{}
	answers chan answer
	out     atomic.Int32 // questions not yet returned
	stopped atomic.Bool  // the controller gave up after too many panics

	judges   map[string]Judge
	verdicts chan verdict
	judging  atomic.Int32 // rounds not yet returned
}

type verdict struct {
	round   int
	records []Link
	v       Verdict
	err     error
}

type report struct {
	problem Problem
	detail  string
}

type answer struct {
	ask   int
	found Found
	err   error
}

// Start starts a controller with the numbers in cfg. A nil archive answers
// every question with an error. Judges are the judges that can be chosen,
// by name.
func Start(cfg Config, archive Archive, screen Screen, now func() time.Time, judges map[string]Judge) *Controller {
	c := &Controller{cfg: cfg, archive: archive, screen: screen, now: now, judges: judges,
		wake: make(chan struct{}, 1), answers: make(chan answer, inFlight+1), verdicts: make(chan verdict, inFlight+1)}
	go c.run()
	return c
}

// Judging says which judge is chosen, by name, or none when empty.
func (c *Controller) Judging(name string) {
	c.set(func() { c.judge = name })
}

// Enabled says whether Live Context is turned on.
func (c *Controller) Enabled(on bool) {
	c.set(func() { c.on = on })
}

// Capturing says what capture was asked to record: the display, empty for
// the main one, and whether the panel is left out of that recording's
// picture.
func (c *Controller) Capturing(display string, leftOut bool) {
	c.set(func() { c.display, c.leftOut = display, leftOut })
}

// Recording says whether On Air is recording.
func (c *Controller) Recording(on bool) {
	c.set(func() { c.recording = on })
}

// Heard takes finished words from speech recognition.
func (c *Controller) Heard(h Heard) {
	if c == nil {
		return
	}
	if len(h.Text) > c.cfg.Chars {
		h.Text = h.Text[len(h.Text)-c.cfg.Chars:]
	}
	c.set(func() {
		if len(c.heard) >= waitingHeard {
			c.heard, c.lost = c.heard[1:], c.lost+1
		}
		c.heard = append(c.heard, h)
	})
}

// Report takes what the Mac says about listening.
func (c *Controller) Report(p Problem, detail string) {
	if c == nil {
		return
	}
	if len(detail) > 300 {
		detail = detail[:300]
	}
	c.set(func() {
		if len(c.reports) >= waitingReports {
			c.reports, c.lost = c.reports[1:], c.lost+1
		}
		c.reports = append(c.reports, report{p, detail})
	})
}

// set changes what the inputs hold and wakes the controller, which reads
// it on its own goroutine.
func (c *Controller) set(change func()) {
	if c == nil {
		return
	}
	c.mu.Lock()
	change()
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// run is the controller's goroutine. Each step takes what the inputs set,
// decides, and tells the Mac. A panic in a step is recovered, and after
// panicsAllowed of them the controller stops: listening ends, the panel
// says why, and what comes in after waits in its bounded queues.
func (c *Controller) run() {
	f := newFollow(c.cfg)
	t := time.NewTicker(c.cfg.Tick)
	defer t.Stop()
	s := &sent{}
	panics := 0
	for {
		var got *answer
		var judged *verdict
		select {
		case <-c.wake:
		case <-t.C:
		case a := <-c.answers:
			got = &a
		case v := <-c.verdicts:
			judged = &v
		}
		err := c.step(f, got, judged, s)
		if err == nil {
			continue
		}
		panics++
		if panics < panicsAllowed {
			continue
		}
		c.stopped.Store(true)
		if s.judge != nil {
			c.safely(s.judge.Close)
		}
		c.safely(func() { c.screen.Listen(false, c.cfg.Chunk) })
		c.safely(func() {
			c.screen.Show(Panel{Visible: f.on && f.recording, Display: f.display, Status: "Live Context stopped after an error, and recording carries on: " + sentence(err.Error())})
		})
		return
	}
}

// sent is what the Mac was last told, and the judge in use.
type sent struct {
	listening bool
	panel     Panel
	shown     bool
	judge     Judge
}

func (c *Controller) step(f *follow, got *answer, judged *verdict, s *sent) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	now := c.now()
	c.mu.Lock()
	on, recording, display, leftOut, name := c.on, c.recording, c.display, c.leftOut, c.judge
	heard, reports := c.heard, c.reports
	c.heard, c.reports = nil, nil
	c.mu.Unlock()

	if f.enable(on) {
		c.screen.Ask()
	}
	f.capturing(display, leftOut)
	f.record(recording, now)
	for _, r := range reports {
		f.report(r.problem, r.detail, now)
	}
	for _, h := range heard {
		f.hear(h, now)
	}
	if got != nil {
		f.answer(got.ask, got.found, got.err, now)
	}
	f.abandon(now)
	// A judge runs only while chosen with Live Context on, and one that
	// is no longer wanted is stopped.
	judge := c.judges[name]
	if !on {
		judge = nil
	}
	if judge != s.judge {
		if s.judge != nil {
			old := s.judge
			go c.safely(old.Close)
		}
		s.judge = judge
	}
	f.judge(judge != nil)
	if judged != nil {
		f.judgement(judged.round, judged.records, judged.v, judged.err, now)
	}
	f.abandonRound(now)
	if listening := f.listening(); listening != s.listening {
		c.screen.Listen(listening, c.cfg.Chunk)
		s.listening = listening
	}
	if c.out.Load() < inFlight {
		if text, ask, ok := f.due(now); ok {
			c.ask(text, ask)
		}
	}
	if judge != nil && c.judging.Load() < inFlight {
		if r, round, ok := f.roundDue(now); ok {
			c.round(judge, r, round)
		}
	}
	if p := f.panel(now); !s.shown || !reflect.DeepEqual(p, s.panel) {
		c.screen.Show(p)
		s.panel, s.shown = p, true
	}
	return nil
}

// ask asks On the Record on a goroutine of its own, for at most
// c.cfg.Timeout, and hands the answer back to run. It never blocks: run
// gives up on an answer that is late, and an answer with no room left is
// dropped.
func (c *Controller) ask(text string, ask int) {
	c.out.Add(1)
	go func() {
		a := answer{ask: ask}
		defer func() {
			if r := recover(); r != nil {
				a.found, a.err = Found{}, fmt.Errorf("asking On the Record failed: %v", r)
			}
			c.out.Add(-1)
			select {
			case c.answers <- a:
			default:
			}
		}()
		if c.archive == nil {
			a.err = errors.New("On Air has no address for On the Record")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Timeout)
		defer cancel()
		a.found, a.err = c.archive.Connections(ctx, text)
	}()
}

// round asks a judge on a goroutine of its own, for at most
// c.cfg.JudgeTimeout, and hands the verdict back to run, as ask does.
func (c *Controller) round(judge Judge, r Round, round int) {
	c.judging.Add(1)
	go func() {
		v := verdict{round: round, records: r.Records}
		defer func() {
			if p := recover(); p != nil {
				v.v, v.err = Verdict{}, fmt.Errorf("the model failed: %v", p)
			}
			c.judging.Add(-1)
			select {
			case c.verdicts <- v:
			default:
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), c.cfg.JudgeTimeout)
		defer cancel()
		v.v, v.err = judge.Judge(ctx, r)
	}()
}

// safely runs a call to the Mac, and keeps a panic in it from going on.
func (c *Controller) safely(call func()) {
	defer func() { _ = recover() }()
	call()
}
