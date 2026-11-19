package live

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// What waits for the controller is bounded, so a controller that has
// stopped, or fallen behind, never holds more than this.
const (
	waitingHeard   = 64 // finished pieces of text, and sentences; the oldest go first
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
	results   []Said // of speech recognition, a partial one replacing the one before of its request
	reports   []report
	lost      int // inputs dropped because their queue was full

	wake    chan struct{}
	answers chan answer
	out     atomic.Int32 // questions not yet returned
	stopped atomic.Bool  // the controller gave up after too many panics

	judges   map[string]Judge
	verdicts chan verdict
	judging  atomic.Int32 // rounds not yet returned

	replies   chan reply
	answering atomic.Int32 // questions of answer cards not yet returned

	writers  map[string]Writer
	writings chan writing
	writes   atomic.Int32 // answers, checks and rewordings the writer hasn't returned

	libraries chan library
	fetches   atomic.Int32 // questions about a card's name not yet returned

	// cancels give up on the questions to On the Record still out, by
	// card, for one asked early that is no longer wanted. Only run uses
	// it.
	cancels map[int]context.CancelFunc
}

// writing is what the writer wrote: an answer for a card, a check, a
// question's other words, or a card about a name.
type writing struct {
	card, check, reword, topic int
	text                       string
	err                        error
	took                       time.Duration
}

// reply is On the Record's answer about a card's question, or, for topic,
// about a card's name.
type reply struct {
	card  int
	topic bool
	brief Brief
	err   error
	took  time.Duration
}

// library is the archive's lists, as they came back.
type library struct {
	lib  Library
	err  error
	took time.Duration
}

// Librarian is what an Archive may also do: give what its lists hold.
type Librarian interface {
	Library(ctx context.Context) (Library, error)
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
	took  time.Duration
}

// Start starts a controller with the numbers in cfg. A nil archive answers
// every question with an error. Judges are the judges that can be chosen,
// by name, and writers the writers that go with them: choosing a name
// chooses both.
func Start(cfg Config, archive Archive, screen Screen, now func() time.Time, judges map[string]Judge, writers map[string]Writer) *Controller {
	c := &Controller{cfg: cfg, archive: archive, screen: screen, now: now, judges: judges, writers: writers,
		wake: make(chan struct{}, 1), answers: make(chan answer, inFlight+1), verdicts: make(chan verdict, inFlight+1), replies: make(chan reply, inFlight+1),
		writings: make(chan writing, inFlight+1), cancels: map[int]context.CancelFunc{}, libraries: make(chan library, 1)}
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

// Said takes a result of speech recognition. Of a final one, it decides
// whose each word was, and hears each speaker's turn in it. Each goes on to
// be split into sentences, which a question is noticed in as soon as it is
// finished: a partial one replaces the one before it of the same request,
// which it holds all of. It returns at once, and a panic in it goes no
// further.
func (c *Controller) Said(s Said) {
	if c == nil {
		return
	}
	defer func() { _ = recover() }()
	if !s.Partial {
		turns := s.heard()
		for _, h := range turns {
			c.Heard(h)
		}
		c.log("%s", s.counts(turns))
	}
	c.set(func() {
		if n := len(c.results); s.Partial && n > 0 && c.results[n-1].Partial && c.results[n-1].Request == s.Request {
			c.results[n-1] = s
			return
		}
		if len(c.results) >= waitingHeard {
			c.results, c.lost = c.results[1:], c.lost+1
		}
		c.results = append(c.results, s)
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
		var replied *reply
		var wrote *writing
		var listed *library
		select {
		case <-c.wake:
		case <-t.C:
		case a := <-c.answers:
			got = &a
		case v := <-c.verdicts:
			judged = &v
		case r := <-c.replies:
			replied = &r
		case w := <-c.writings:
			wrote = &w
		case l := <-c.libraries:
			listed = &l
		}
		err := c.step(f, got, judged, replied, wrote, listed, s)
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
		if s.writer != nil {
			c.safely(s.writer.Close)
		}
		c.safely(func() { c.screen.Listen(false, c.cfg.Chunk) })
		c.safely(func() {
			c.screen.Show(Panel{Visible: f.on && f.recording, Display: f.display, Status: "Live Context stopped after an error, and recording carries on: " + sentence(err.Error())})
		})
		return
	}
}

// sent is what the Mac was last told, and the judge and writer in use.
type sent struct {
	listening bool
	panel     Panel
	shown     bool
	judge     Judge
	writer    Writer
	warmed    bool   // they were told to load their models, for this time listening
	shows     string // what the diagnostic log last said the panel shows
}

func (c *Controller) step(f *follow, got *answer, judged *verdict, replied *reply, wrote *writing, listed *library, s *sent) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	now := c.now()
	c.mu.Lock()
	on, recording, display, leftOut, name := c.on, c.recording, c.display, c.leftOut, c.judge
	heard, reports, results := c.heard, c.reports, c.results
	c.heard, c.reports, c.results = nil, nil, nil
	c.mu.Unlock()

	if f.enable(on) {
		c.screen.Ask()
	}
	f.capturing(display, leftOut)
	f.record(recording, now)
	for _, r := range reports {
		c.log("speech %s: %s", map[Problem]string{SpeechAllowed: "allowed", SpeechRefused: "refused", SpeechUnsupported: "unsupported", SpeechFailed: "failed", SoundDropped: "dropped"}[r.problem], line(r.detail, 160))
		f.report(r.problem, r.detail, now)
	}
	if len(heard) > 0 {
		person, others := 0, 0
		for _, h := range heard {
			if n := len(strings.Fields(h.Text)); h.Speaker == Person {
				person += n
			} else {
				others += n
			}
			f.hear(h, now)
		}
		all, mine := f.words()
		c.log("heard %d pieces, %d words yours and %d others, listening %t; the window holds %d words, %d yours", len(heard), person, others, f.listening(), all, mine)
	}
	if listed != nil {
		f.library(listed.lib, listed.err, now)
		switch {
		case listed.err != nil:
			c.log("the archive's lists failed after %s: %s", listed.took.Round(time.Millisecond), brief(listed.err))
		case f.roster != nil:
			c.log("the archive's lists came in %s: %s", listed.took.Round(time.Millisecond), f.roster.counts())
		}
	}
	if f.listening() && f.listenedAt.IsZero() {
		f.listenedAt = now
		f.standingCard(now)
	}
	var finished []utterance
	for _, s := range results {
		finished = append(finished, f.recognized(s)...)
	}
	for _, r := range f.stream.read() {
		c.log("%s", r)
	}
	for _, said := range append(finished, f.flush(now)...) {
		c.heardNames(f, said.text, true, now)
		q, ok := notice(said)
		if !ok {
			f.state(said)
			continue
		}
		if !f.asksArchive(q, said.text) {
			// Left to the people in the call, it still asks something,
			// and is no statement to check against the record.
			c.log("a question of %d words names nothing the archive knows, so it is left to the people in the call", len(strings.Fields(q.topic)))
			continue
		}
		who := map[Speaker]string{Person: "yours", Others: "someone else's"}[q.who]
		how := map[ending]string{byMark: "a mark", byPause: "a pause", open: "the end of the sound", byTime: "time"}[q.how]
		if n, again, kept := f.ask(q, now); again {
			c.log("a question of %d words, %s, was asked within %s, so not again", len(strings.Fields(q.topic)), who, f.cfg.Again)
		} else if n != 0 {
			early := "not asked early"
			if kept {
				early = "asked early, and what was found for it kept"
			}
			c.log("question %d noticed: %d words, %d key words, %s, its sentence ended by %s, %s after its last word, %s; so far %d noticed, %d asked early, %d of them kept",
				n, len(strings.Fields(q.topic)), len(q.keys), who, how, after(now, q.said), early, f.noticed, f.askedEarly, f.keptEarly)
			if cd := f.card(n); kept && cd != nil && cd.done {
				c.log("%s", f.timing(cd, now))
			}
		}
	}
	// A question still being asked is asked early, and a name in the
	// sentence being said gets its card at once.
	g, asks := f.stream.growing()
	if asks {
		c.heardNames(f, g.text, false, now)
	}
	var q question
	if asks {
		q, asks = notice(g)
		asks = asks && f.asksArchive(q, g.text)
	}
	if started, dropped := f.early(q, asks, now); started != 0 || dropped != 0 {
		if dropped != 0 {
			c.log("question %d, asked early, dropped: the sentence went on to ask something else, or nothing", dropped)
		}
		if started != 0 {
			c.log("question %d asked early: %d words so far, %d key words, %s", started, len(strings.Fields(q.topic)), len(q.keys), map[Speaker]string{Person: "yours", Others: "someone else's"}[q.who])
		}
	}
	for n, cancel := range c.cancels {
		if !f.waits(n) {
			cancel()
			delete(c.cancels, n)
		}
	}
	if replied != nil && replied.topic {
		notes, taken := f.topicFetched(replied.card, replied.brief, replied.err, now)
		switch {
		case !taken:
			c.log("topic card %d: On the Record's answer came after %s, too late to be taken", replied.card, replied.took.Round(time.Millisecond))
		case replied.err != nil:
			c.log("topic card %d: asking On the Record failed after %s: %s", replied.card, replied.took.Round(time.Millisecond), brief(replied.err))
		default:
			c.log("topic card %d: On the Record answered in %s: %d lines, %d of them notes for the local model", replied.card, replied.took.Round(time.Millisecond), len(replied.brief.Points), notes)
		}
		replied = nil
	}
	if replied != nil {
		early := f.card(replied.card) != nil && f.card(replied.card).early
		shown, taken, write := f.reply(replied.card, replied.brief, replied.err, now)
		meetings := map[string]bool{}
		for _, p := range replied.brief.Points {
			meetings[p.Meeting+"\x00"+p.Date] = true
		}
		switch {
		case !taken:
			c.log("answer %d came back after %s, too late to be taken", replied.card, replied.took.Round(time.Millisecond))
		case replied.err != nil:
			c.log("answer %d failed after %s: %s", replied.card, replied.took.Round(time.Millisecond), brief(replied.err))
		case early:
			c.log("answer %d came back in %s: %d lines from %d meetings, %d passages, about %d tokens; kept for when its sentence is finished",
				replied.card, replied.took.Round(time.Millisecond), len(replied.brief.Points), len(meetings), replied.brief.Passages, briefTokens(replied.brief))
		case write:
			c.log("answer %d came back in %s: %d lines from %d meetings, %d passages, about %d tokens; for the local model to write",
				replied.card, replied.took.Round(time.Millisecond), len(replied.brief.Points), len(meetings), replied.brief.Passages, briefTokens(replied.brief))
		default:
			c.log("answer %d came back in %s: %d lines from %d meetings, %d passages; %d shown; so far %d asked and %d lines shown",
				replied.card, replied.took.Round(time.Millisecond), len(replied.brief.Points), len(meetings), replied.brief.Passages, shown, f.answers, f.lines)
		}
		delete(c.cancels, replied.card)
		if cd := f.card(replied.card); taken && cd != nil && cd.done {
			c.log("%s", f.timing(cd, now))
		}
	}
	if n := f.abandonAnswer(now); n != 0 {
		c.log("answer %d given up on after %s", n, now.Sub(f.answerAsked).Round(time.Millisecond))
	}
	if got != nil {
		taken := got.ask != 0 && got.ask == f.asking
		f.answer(got.ask, got.found, got.err, now)
		switch {
		case !taken:
			c.log("ask %d came back after %s, too late to be taken", got.ask, got.took.Round(time.Millisecond))
		case got.err != nil:
			c.log("ask %d failed after %s: %s", got.ask, got.took.Round(time.Millisecond), brief(got.err))
		default:
			c.log("ask %d answered in %s: %d meetings, %d notes, %d items, %d strong, %d picked", got.ask, got.took.Round(time.Millisecond),
				len(got.found.Meetings), len(got.found.Notes), len(got.found.Items), len(got.found.StrongMeetings)+len(got.found.StrongItems), len(pick(got.found, c.cfg.Most)))
		}
	}
	if waiting := f.asking; waiting != 0 {
		if f.abandon(now); f.asking == 0 {
			c.log("ask %d given up on after %s", waiting, now.Sub(f.asked).Round(time.Millisecond))
		}
	}
	// A judge and its writer run only while chosen with Live Context on,
	// and one that is no longer wanted is stopped. They start loading
	// their models as listening starts, so the first round and the first
	// answer don't find them loading.
	judge, writer := c.judges[name], c.writers[name]
	if !on {
		judge, writer = nil, nil
	}
	if judge != s.judge {
		if s.judge != nil {
			old := s.judge
			go c.safely(old.Close)
		}
		s.judge, s.warmed = judge, false
	}
	if writer != s.writer {
		if s.writer != nil {
			old := s.writer
			go c.safely(old.Close)
		}
		s.writer, s.warmed = writer, false
	}
	f.judge(judge != nil)
	f.writes(writer != nil)
	if !f.listening() {
		s.warmed = false
	} else if !s.warmed {
		s.warmed = true
		for _, m := range []any{judge, writer} {
			if w, ok := m.(Warmer); ok {
				go c.safely(w.Warm)
			}
		}
	}
	if judged != nil {
		taken := judged.round != 0 && judged.round == f.inRound
		f.judgement(judged.round, judged.records, judged.v, judged.err, now)
		switch {
		case !taken:
			c.log("round %d came back too late to be taken", judged.round)
		case errors.Is(judged.err, ErrLoading):
			c.log("round %d waits: the local model is loading", judged.round)
		case judged.err != nil:
			c.log("round %d failed: %s", judged.round, brief(judged.err))
		case judged.v.Topic >= Thresholds.Topic:
			c.log("round %d judged: decision %.2f, topic changed %.2f, so the window was emptied", judged.round, judged.v.Decision, judged.v.Topic)
		default:
			c.log("round %d judged: decision %.2f, topic changed %.2f", judged.round, judged.v.Decision, judged.v.Topic)
		}
	}
	if waiting := f.inRound; waiting != 0 {
		if f.abandonRound(now); f.inRound == 0 {
			c.log("round %d given up on after %s", waiting, now.Sub(f.judged).Round(time.Millisecond))
		}
	}
	if wrote != nil {
		c.wrote(f, wrote, now)
	}
	if n := f.abandonReword(now); n != 0 {
		c.log("reword %d given up on after %s, so its question goes as heard", n, now.Sub(f.rewordAsked).Round(time.Millisecond))
	}
	if n := f.abandonWrite(now); n != 0 {
		c.log("write %d given up on after %s", n, now.Sub(f.writeAsked).Round(time.Millisecond))
	}
	if n := f.abandonCheck(now); n != 0 {
		c.log("check %d given up on after %s", n, now.Sub(f.checked).Round(time.Millisecond))
	}
	if n := f.abandonTopicFetch(now); n != 0 {
		c.log("topic card %d: On the Record given up on after %s", n, now.Sub(f.fetchAsked).Round(time.Millisecond))
	}
	if n := f.abandonTopicWrite(now); n != 0 {
		c.log("topic card %d: the local model given up on after %s", n, now.Sub(f.topicAsked).Round(time.Millisecond))
	}
	if f.libraryDue(now) {
		if l, ok := c.archive.(Librarian); ok {
			c.log("the archive's lists asked for")
			c.library(l)
		} else {
			f.libAsking = false
		}
	}
	if listening := f.listening(); listening != s.listening {
		c.log("listen %t", listening)
		c.screen.Listen(listening, c.cfg.Chunk)
		s.listening = listening
	}
	if c.out.Load() < inFlight {
		if text, ask, ok := f.due(now); ok {
			c.log("ask %d sent, %d words", ask, len(strings.Fields(text)))
			c.ask(text, ask)
		}
	}
	if writer != nil && c.writes.Load() < inFlight {
		if p, n, ok := f.rewordDue(now); ok {
			c.log("reword %d asked: about %d tokens", n, tokens(p.System+p.User))
			c.write(writer, p, writing{reword: n})
		}
	}
	if c.fetches.Load() < 1 {
		if ask, n, ok := f.topicFetchDue(now); ok {
			c.log("topic card %d: On the Record asked, %d words", n, len(strings.Fields(ask)))
			c.fetchTopic(ask, n)
		}
	}
	if c.answering.Load() < inFlight {
		if topic, n, ok := f.answerDue(now); ok {
			cd := f.card(n)
			c.log("answer %d asked, %d words, %d of them the local model's, %s", n, len(strings.Fields(topic)), len(cd.q.extra), map[bool]string{true: "early", false: "for a card shown"}[cd.early])
			c.answer(topic, n)
		}
	}
	if writer != nil && c.writes.Load() < inFlight {
		if p, n, ok := f.writeDue(now); ok {
			c.log("write %d asked: %d passages, about %d tokens", n, strings.Count(p.User, "\n["), tokens(p.System+p.User))
			c.write(writer, p, writing{card: n})
		} else if p, n, ok := f.topicWriteDue(now); ok {
			c.log("topic card %d: the local model asked, %d notes, about %d tokens", n, strings.Count(p.User, "\n["), tokens(p.System+p.User))
			c.write(writer, p, writing{topic: n})
		} else if p, n, said, records, ok := f.checkDue(now); ok {
			c.log("check %d asked: %d statements, %d records, about %d tokens", n, said, records, tokens(p.System+p.User))
			c.write(writer, p, writing{check: n})
		}
	}
	if judge != nil && c.judging.Load() < inFlight {
		if r, round, ok := f.roundDue(now); ok {
			c.log("round %d sent, %d words", round, len(strings.Fields(r.Text)))
			c.round(judge, r, round)
		}
	}
	if p := f.panel(now); !s.shown || !reflect.DeepEqual(p, s.panel) {
		c.screen.Show(p)
		s.panel, s.shown = p, true
		if shows := f.shows(p, now); shows != s.shows {
			c.log("panel %s", shows)
			s.shows = shows
		}
	}
	return nil
}

// log adds a line to the diagnostic log, when the Screen keeps one.
func (c *Controller) log(format string, args ...any) {
	if l, ok := c.screen.(Logger); ok {
		l.Log(fmt.Sprintf(format, args...))
	}
}

// ask asks On the Record on a goroutine of its own, for at most
// c.cfg.Timeout, and hands the answer back to run. It never blocks: run
// gives up on an answer that is late, and an answer with no room left is
// dropped.
func (c *Controller) ask(text string, ask int) {
	c.out.Add(1)
	go func() {
		a := answer{ask: ask}
		began := c.now()
		defer func() {
			if r := recover(); r != nil {
				a.found, a.err = Found{}, fmt.Errorf("asking On the Record failed: %v", r)
			}
			a.took = c.now().Sub(began)
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

// answer asks On the Record about a card's question on a goroutine of its
// own, for at most c.cfg.Timeout, and hands the reply back to run, as ask
// does.
func (c *Controller) answer(topic string, card int) {
	c.answering.Add(1)
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Timeout)
	c.cancels[card] = cancel
	go func() {
		defer cancel()
		r := reply{card: card}
		began := c.now()
		defer func() {
			if p := recover(); p != nil {
				r.brief, r.err = Brief{}, fmt.Errorf("asking On the Record failed: %v", p)
			}
			r.took = c.now().Sub(began)
			c.answering.Add(-1)
			select {
			case c.replies <- r:
			default:
			}
		}()
		if c.archive == nil {
			r.err = errors.New("On Air has no address for On the Record")
			return
		}
		r.brief, r.err = c.archive.Answer(ctx, topic, c.cfg.Transcript)
	}()
}

// heardNames takes the names the archive knows in what was said, and logs
// what came of each, in counts only.
func (c *Controller) heardNames(f *follow, text string, finished bool, now time.Time) {
	for _, h := range f.heardNames(text, finished, now) {
		kind := map[bool]string{true: "a person", false: "a project or company"}[h.person]
		how := map[bool]string{true: "a finished sentence", false: "a sentence still being said"}[finished]
		if h.again {
			c.log("topic card %d started: %s came back in %s with something else said about it", h.card, kind, how)
			continue
		}
		c.log("topic card %d started: %s heard in %s, %s from the archive's lists shown at once", h.card, kind, how, plural(h.lines, "line"))
	}
}

// library asks the archive for its lists on a goroutine of its own, for
// at most c.cfg.Timeout, and hands them back to run, as ask does.
func (c *Controller) library(l Librarian) {
	go func() {
		out := library{}
		began := c.now()
		defer func() {
			if r := recover(); r != nil {
				out.lib, out.err = Library{}, fmt.Errorf("asking On the Record failed: %v", r)
			}
			out.took = c.now().Sub(began)
			select {
			case c.libraries <- out:
			default:
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Timeout)
		defer cancel()
		out.lib, out.err = l.Library(ctx)
	}()
}

// fetchTopic asks On the Record about a card's name on a goroutine of its
// own, for at most c.cfg.Timeout, and hands the reply back to run, as ask
// does.
func (c *Controller) fetchTopic(ask string, card int) {
	c.fetches.Add(1)
	go func() {
		r := reply{card: card, topic: true}
		began := c.now()
		defer func() {
			if p := recover(); p != nil {
				r.brief, r.err = Brief{}, fmt.Errorf("asking On the Record failed: %v", p)
			}
			r.took = c.now().Sub(began)
			c.fetches.Add(-1)
			select {
			case c.replies <- r:
			default:
			}
		}()
		if c.archive == nil {
			r.err = errors.New("On Air has no address for On the Record")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Timeout)
		defer cancel()
		r.brief, r.err = c.archive.Answer(ctx, ask, false)
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

// wrote takes what the writer wrote, and logs what came of it, in counts
// and On Air's own words: never what was asked or written.
func (c *Controller) wrote(f *follow, w *writing, now time.Time) {
	took := w.took.Round(time.Millisecond)
	if w.topic != 0 {
		kept, left, why, taken := f.topicWritten(w.topic, w.text, w.err, now)
		switch {
		case !taken:
			c.log("topic card %d: the local model came back after %s, too late to be taken", w.topic, took)
		case w.err != nil:
			c.log("topic card %d: the local model failed after %s: %s", w.topic, took, why)
		default:
			came := fmt.Sprintf("%s kept, %d left out", plural(kept, "line"), left)
			if why != "" {
				came += ", the first because " + why
			}
			c.log("topic card %d: the local model wrote in %s: %s; so far %d cards about names started, %d written", w.topic, took, came, f.topicsStarted, f.topicsWritten)
			if tc := f.tcard(w.topic); tc != nil {
				c.log("%s", f.topicLog(tc, now))
			}
		}
		return
	}
	if w.reword != 0 {
		added, failed, taken := f.reworded(w.reword, w.text, w.err, now)
		switch {
		case !taken:
			c.log("reword %d came back after %s, too late to be taken", w.reword, took)
		case failed != "":
			c.log("reword %d failed after %s, so its question goes as heard: %s", w.reword, took, failed)
		default:
			c.log("reword %d came back in %s: %s added", w.reword, took, plural(added, "word"))
		}
		return
	}
	if w.card != 0 {
		o, why, taken := f.answerWritten(w.card, w.text, w.err)
		var came string
		switch {
		case !taken:
			c.log("write %d came back after %s, too late to be taken", w.card, took)
			return
		case w.err != nil:
			came = "failed: " + brief(w.err)
		case o == answered:
			came = fmt.Sprintf("answered, citing %s", plural(len(f.card(w.card).sources), "source"))
		case o == notInArchive:
			came = "not in the archive"
		default:
			came = "rejected: " + why
		}
		c.log("write %d came back in %s: %s; so far %d written, %d answered, %d not in the archive, %d rejected",
			w.card, took, came, f.written, f.answeredN, f.notIn, f.rejectedN)
		if cd := f.card(w.card); cd != nil {
			c.log("%s", f.timing(cd, now))
		}
		return
	}
	result, taken := f.checkedBy(w.check, w.text, w.err, now)
	if !taken {
		c.log("check %d came back after %s, too late to be taken", w.check, took)
		return
	}
	c.log("check %d came back in %s: %s; so far %d checked, %d differ, %d rejected", w.check, took, result, f.checksBack, f.found, f.checkRejected)
}

// write asks the writer on a goroutine of its own, for at most
// c.cfg.WriteTimeout, and hands what it wrote back to run, as ask does.
func (c *Controller) write(writer Writer, p Prompt, w writing) {
	c.writes.Add(1)
	go func() {
		began := c.now()
		defer func() {
			if r := recover(); r != nil {
				w.text, w.err = "", fmt.Errorf("the local model failed: %v", r)
			}
			w.took = c.now().Sub(began)
			c.writes.Add(-1)
			select {
			case c.writings <- w:
			default:
			}
		}()
		timeout := c.cfg.WriteTimeout
		if w.reword != 0 {
			timeout = c.cfg.RewordTimeout
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		w.text, w.err = writer.Write(ctx, p)
	}()
}

// safely runs a call to the Mac, and keeps a panic in it from going on.
func (c *Controller) safely(call func()) {
	defer func() { _ = recover() }()
	call()
}
