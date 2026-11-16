package live

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// follow is the live side's state, which one goroutine owns: whether it is
// on, the last minute of what was said, when it last asked On the Record,
// and what the panel shows. Every decision is a method on it, given the
// time, so it is tested on a clock the test sets.
type follow struct {
	cfg Config

	on        bool // Live Context is turned on
	recording bool
	display   string
	leftOut   bool      // the recording leaves On Air's own windows out of its picture
	stopped   time.Time // when the last recording stopped, for the linger

	heard []Heard // the window, oldest first
	fresh bool    // something new was heard since the last question

	asked  time.Time // when the last question was asked
	asking int       // the question waiting for an answer, or 0
	asks   int       // questions asked so far, which number them

	// How the last question that ended went, and when it was asked.
	result   string
	resultAt time.Time

	shown map[string]time.Time // when each link was last put on the panel
	links []Link               // on the panel, newest first

	judging bool      // a judge is chosen
	judged  time.Time // when the last round was asked
	inRound int       // the round waiting for a verdict, or 0
	rounds  int       // rounds asked so far, which number them
	news    bool      // something new was said since the last round
	calls   []call    // what the judge said, newest first, one of each kind
	verdict string    // what went wrong asking the judge, if anything
	loading bool      // and it was only that the model is still loading

	// The local model's writing, one answer or check at a time, and never
	// while a round waits for the judge: they share this Mac's GPU.
	writer     bool        // a writer is chosen
	writing    int         // the card whose answer is being written, or 0
	writeAsked time.Time   // when it was asked
	wrote      string      // what went wrong asking the writer, if anything
	statements []statement // what was said lately that states a fact, oldest first
	newFact    bool        // something that states a fact was said since the last check
	records    []record    // lines of briefs fetched lately that state a fact
	checking   int         // the check waiting for the writer, or 0
	checked    time.Time   // when the last check was asked
	checks     int         // checks asked so far, which number them
	check      checkAsked  // what the check waiting was asked about
	differs    *differs    // what was said that differs from the record, while shown
	differed   map[string]time.Time
	// Counts for the diagnostic log: answers written, of them answered,
	// not in the archive and rejected, and checks that came back, of them
	// rejected and differences found.
	written, answeredN, notIn, rejectedN, checksBack, checkRejected, found int

	stream      stream    // speech recognition's results, which hand on sentences once finished
	cards       []*card   // answer cards, newest first, and one asked early, not shown, before them
	cardsMade   int       // cards made so far, which number them
	answering   int       // the card whose question waits for an answer, or 0
	answerAsked time.Time // when it was asked
	lately      []lately  // questions asked lately, which aren't asked again
	// The writer rewords one question at a time for On the Record, first
	// of what it does.
	rewording   int       // the card whose question it rewords, or 0
	rewordAsked time.Time // when it was asked
	slow        bool      // it said lately that its model is loading, so questions go as heard
	noticed     int       // questions noticed so far, for the diagnostic log
	answers     int       // and asked
	lines       int       // and lines of answers shown
	// Questions asked early, before their sentence was finished: so far,
	// kept for the question once it was, and dropped.
	askedEarly, keptEarly, droppedEarly int

	allowed   Problem // SpeechAllowed, SpeechRefused or SpeechUnsupported, once the Mac has said
	speech    string  // what is wrong with listening, if anything
	transient bool    // and it goes once words are heard again
	archive   string  // what went wrong asking On the Record, if anything
	answered  string  // a word on the last answer
	dropped   string  // that sound was left out, if it was
}

func newFollow(cfg Config) *follow {
	return &follow{cfg: cfg, shown: map[string]time.Time{}, differed: map[string]time.Time{}}
}

// checkAsked is what a check asked the writer about.
type checkAsked struct {
	said    []statement
	records []record
}

// differs is a difference from the record on the panel, and since when.
type differs struct {
	line AnswerLine
	at   time.Time
}

// listening says whether speech should be recognized now.
func (f *follow) listening() bool { return f.on && f.recording }

// enable turns Live Context on or off, and says whether to ask for the
// permission to recognize speech: each time it is turned on, so the
// question comes before the first recording that uses it.
func (f *follow) enable(on bool) (ask bool) {
	ask = on && !f.on
	f.on = on
	if !on {
		f.heard, f.fresh, f.news, f.calls, f.stream = nil, false, false, nil, stream{}
		f.statements, f.newFact, f.differs = nil, false, nil
	}
	return ask
}

// judge says whether a judge is chosen. Without one, what it said goes.
func (f *follow) judge(on bool) {
	if !on {
		f.calls, f.verdict, f.loading, f.inRound = nil, "", false, 0
	}
	f.judging = on
}

// writes says whether a writer is chosen. Without one, what is waiting to
// be written shows the lines that share the question's words, and what it
// found goes.
func (f *follow) writes(on bool) {
	if !on && f.writer {
		for _, c := range f.cards {
			if c.write || c.n == f.writing {
				c.write = false
				f.fallback(c)
			}
		}
		f.writing, f.checking, f.check, f.wrote, f.differs, f.rewording = 0, 0, checkAsked{}, "", nil, 0
	}
	if on && !f.writer {
		f.slow = false
	}
	f.writer = on
}

// capturing notes what capture was last asked to record: the display the
// panel goes beside, and whether that recording leaves the panel out of
// its picture.
func (f *follow) capturing(display string, leftOut bool) {
	f.display, f.leftOut = display, leftOut
}

// record notes whether On Air is recording. A new recording starts with
// an empty window and panel, and a question still out from the last one is
// dropped when it comes back. What was shown stays shown for f.cfg.Quiet,
// across recordings.
func (f *follow) record(on bool, now time.Time) {
	switch {
	case on && !f.recording:
		f.heard, f.fresh, f.links = nil, false, nil
		f.asking, f.asked, f.result, f.resultAt = 0, time.Time{}, "", time.Time{}
		f.news, f.calls, f.inRound, f.judged, f.verdict, f.loading = false, nil, 0, time.Time{}, "", false
		f.speech, f.transient, f.archive, f.answered, f.dropped = "", false, "", "", ""
		f.stream, f.cards, f.answering, f.lately, f.rewording = stream{}, nil, 0, nil, 0
		f.writing, f.wrote, f.statements, f.newFact, f.records = 0, "", nil, false, nil
		f.checking, f.checked, f.check, f.differs = 0, time.Time{}, checkAsked{}, nil
	case !on && f.recording:
		f.stopped = now
	}
	f.recording = on
}

// report takes what the Mac says about listening.
func (f *follow) report(p Problem, detail string, now time.Time) {
	if p == SpeechAllowed || p == SpeechRefused || p == SpeechUnsupported {
		f.allowed = p
	}
	switch p {
	case SpeechAllowed:
		// A refusal, or a language the Mac couldn't recognize, no longer
		// holds. A failure goes once words are heard.
		if !f.transient {
			f.speech = ""
		}
	case SpeechRefused:
		how := ""
		if detail != "" {
			how = " (" + detail + ")"
		}
		f.speech, f.transient = "Live Context can't listen: speech recognition isn't allowed"+how+". Allow On Air in System Settings, Privacy & Security, Speech Recognition, then start a new recording.", false
	case SpeechUnsupported:
		language := "this language"
		if detail != "" {
			language = detail
		}
		f.speech, f.transient = fmt.Sprintf("Live Context can't listen: this Mac can't recognize %s on its own, and On Air doesn't send the call's words to Apple.", language), false
	case SpeechFailed:
		if f.speech == "" || f.transient {
			f.speech, f.transient = "Speech recognition had a problem, and keeps trying: "+sentence(detail), true
		}
	case SoundDropped:
		if f.listening() {
			f.dropped = fmt.Sprintf("Live Context fell behind at %s and skipped some of the sound.", now.Format("15:04"))
		}
	}
}

// hear takes finished words, while listening, and keeps the window to the
// last f.cfg.Window and f.cfg.Chars.
func (f *follow) hear(h Heard, now time.Time) {
	if !f.listening() {
		return
	}
	h.Text = strings.Join(strings.Fields(h.Text), " ")
	if h.Text == "" {
		return
	}
	// Times that can't be right are taken as now: a minute ahead, or
	// before the window began.
	if h.To.IsZero() || h.To.After(now.Add(time.Minute)) || h.To.Before(now.Add(-f.cfg.Window)) {
		h.From, h.To = now, now
	}
	if h.From.IsZero() || h.From.After(h.To) {
		h.From = h.To
	}
	if h.Speaker != Person {
		h.Speaker = Others
	}
	f.heard = append(f.heard, h)
	f.fresh, f.news = true, true
	if f.transient {
		f.speech, f.transient = "", false
	}
	f.trim(now)
}

// trim drops what was said before the window, then the oldest words until
// the window holds f.cfg.Chars at most.
func (f *follow) trim(now time.Time) {
	since := now.Add(-f.cfg.Window)
	var kept []Heard
	total := 0
	for _, h := range f.heard {
		if !h.To.Before(since) {
			kept = append(kept, h)
			total += len(h.Text) + 1
		}
	}
	for total > f.cfg.Chars && len(kept) > 0 {
		over := total - f.cfg.Chars
		if over >= len(kept[0].Text)+1 {
			total -= len(kept[0].Text) + 1
			kept = kept[1:]
			continue
		}
		// The newest words of the oldest piece stay.
		cut := over
		for cut < len(kept[0].Text) && !utf8.RuneStart(kept[0].Text[cut]) {
			cut++
		}
		kept[0].Text = strings.TrimSpace(kept[0].Text[cut:])
		break
	}
	f.heard = kept
}

// words counts the words in the window: all of them, and the person's.
func (f *follow) words() (all, person int) {
	for _, h := range f.heard {
		n := len(strings.Fields(h.Text))
		all += n
		if h.Speaker == Person {
			person += n
		}
	}
	return all, person
}

// text is the window as one piece of text, in the order it was said.
func (f *follow) text() string {
	lines := make([]string, 0, len(f.heard))
	ordered := append([]Heard(nil), f.heard...)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j].From.Before(ordered[j-1].From); j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	for _, h := range ordered {
		lines = append(lines, h.Text)
	}
	return strings.Join(lines, "\n")
}

// due says whether to ask On the Record now, and what: while listening,
// once something new was said since the last question, f.cfg.Every after
// it, when the window holds f.cfg.Least words, and never while a question
// waits for its answer. A question it returns is under way: answer or
// abandon ends it.
func (f *follow) due(now time.Time) (text string, ask int, ok bool) {
	if !f.listening() || f.asking != 0 || !f.fresh {
		return "", 0, false
	}
	if !f.asked.IsZero() && now.Sub(f.asked) < f.cfg.Every {
		return "", 0, false
	}
	if all, _ := f.words(); all < f.cfg.Least {
		return "", 0, false
	}
	f.asks++
	f.asking, f.asked, f.fresh = f.asks, now, false
	return f.text(), f.asking, true
}

// roundDue says whether to ask the judge now, and what: while listening
// with a judge chosen, once something new was said since the last round,
// f.cfg.JudgeEvery after it, when the window holds f.cfg.Least words, and
// never while a round waits for its verdict.
func (f *follow) roundDue(now time.Time) (Round, int, bool) {
	if !f.listening() || !f.judging || f.inRound != 0 || !f.news || f.writing != 0 || f.checking != 0 || f.pending() {
		return Round{}, 0, false
	}
	if !f.judged.IsZero() && now.Sub(f.judged) < f.cfg.JudgeEvery {
		return Round{}, 0, false
	}
	if all, _ := f.words(); all < f.cfg.Least {
		return Round{}, 0, false
	}
	f.rounds++
	f.inRound, f.judged, f.news = f.rounds, now, false
	return Round{Text: f.text(), Records: append([]Link(nil), f.links...)}, f.inRound, true
}

// judgement takes a judge's verdict on a round. A topic that changed
// clears the panel and starts a fresh minute. Otherwise what clears the
// thresholds shows, the newest of each kind, for f.cfg.Hold.
func (f *follow) judgement(round int, records []Link, v Verdict, err error, now time.Time) {
	if round == 0 || round != f.inRound {
		return
	}
	f.inRound = 0
	if errors.Is(err, ErrLoading) {
		// Loading is no failure: the next round asks again, once
		// something new is said, as this one would have.
		f.verdict, f.loading, f.news = "The local model is loading.", true, true
		return
	}
	f.loading = false
	if err != nil {
		f.verdict = "Couldn't ask the model: " + sentence(err.Error())
		return
	}
	f.verdict = ""
	if v.Topic >= Thresholds.Topic {
		f.links, f.calls, f.heard, f.fresh, f.news, f.answered = nil, nil, nil, false, false, ""
		return
	}
	calls := verdictCalls(v, now)
	kept := append([]call(nil), calls...)
	for _, old := range f.calls {
		replaced := false
		for _, c := range calls {
			replaced = replaced || c.kind == old.kind
		}
		if !replaced {
			kept = append(kept, old)
		}
	}
	f.calls = kept
}

// abandonRound gives up on a round whose verdict is late by a second.
func (f *follow) abandonRound(now time.Time) {
	if f.inRound == 0 || now.Sub(f.judged) <= f.cfg.JudgeTimeout+time.Second {
		return
	}
	f.inRound, f.loading = 0, false
	f.verdict = fmt.Sprintf("Couldn't ask the model: it didn't answer within %s.", f.cfg.JudgeTimeout)
}

// answer takes On the Record's answer to a question. One that nothing
// waits for any more is dropped.
func (f *follow) answer(ask int, found Found, err error, now time.Time) {
	if ask == 0 || ask != f.asking {
		return
	}
	f.asking = 0
	if err != nil {
		f.archive, f.answered = "Couldn't ask On the Record: "+sentence(err.Error()), ""
		f.result, f.resultAt = brief(err), f.asked
		return
	}
	f.archive = ""
	picked := pick(found, f.cfg.Most)
	f.result, f.resultAt = plural(len(picked), "link"), f.asked
	if f.merge(picked, now) == 0 {
		f.answered = fmt.Sprintf("Nothing new from On the Record at %s.", now.Format("15:04"))
	} else {
		f.answered = ""
	}
}

// abandon gives up on a question whose answer is late by a second, so a
// question that never returns doesn't stop the next.
func (f *follow) abandon(now time.Time) {
	if f.asking == 0 || now.Sub(f.asked) <= f.cfg.Timeout+time.Second {
		return
	}
	f.asking = 0
	f.archive, f.answered = fmt.Sprintf("Couldn't ask On the Record: it didn't answer within %s.", f.cfg.Timeout), ""
	f.result, f.resultAt = fmt.Sprintf("no answer within %s", f.cfg.Timeout), f.asked
}

// merge puts the links not shown in the last f.cfg.Quiet on top of the
// panel, in their order, keeps f.cfg.Most, and says how many it put there.
func (f *follow) merge(links []Link, now time.Time) int {
	for key, at := range f.shown {
		if now.Sub(at) >= f.cfg.Quiet {
			delete(f.shown, key)
		}
	}
	var added []Link
	for _, l := range links {
		if _, recent := f.shown[l.key()]; recent {
			continue
		}
		f.shown[l.key()] = now
		added = append(added, l)
	}
	if len(added) == 0 {
		return 0
	}
	kept := added
	for _, l := range f.links {
		dup := false
		for _, a := range added {
			dup = dup || a.key() == l.key()
		}
		if !dup {
			kept = append(kept, l)
		}
	}
	if len(kept) > f.cfg.Most {
		kept = kept[:f.cfg.Most]
	}
	f.links = kept
	return len(added)
}

// panel is what the panel shows now.
func (f *follow) panel(now time.Time) Panel {
	lingering := !f.recording && (len(f.links) > 0 || len(f.cards) > 0) && !f.stopped.IsZero() && now.Sub(f.stopped) < f.cfg.Linger
	p := Panel{Visible: f.on && (f.recording || lingering), Display: f.display, Links: append([]Link(nil), f.links...)}
	if !p.Visible {
		p.Links = nil
		return p
	}
	p.Headline = f.headline(now)
	for _, c := range f.cards {
		if c.early {
			continue
		}
		a := Answer{Question: c.q.shown, Text: c.text, Sources: append([]AnswerLine(nil), c.sources...), Lines: append([]AnswerLine(nil), c.lines...), Note: c.note}
		switch {
		case c.write || c.n == f.writing:
			a.Lines, a.Note = nil, "Reading what On the Record found…"
		case !c.done:
			a.Note = "Looking…"
		}
		p.Answers = append(p.Answers, a)
	}
	if f.differs != nil && now.Sub(f.differs.at) < f.cfg.Hold {
		p.Differs = f.differs.line
	}
	for _, c := range f.calls {
		if now.Sub(c.at) < f.cfg.Hold {
			p.Calls = append(p.Calls, c.text)
		}
	}
	if !f.recording {
		p.Status = fmt.Sprintf("The recording stopped. The panel goes at %s.", f.stopped.Add(f.cfg.Linger).Format("15:04"))
		return p
	}
	var lines []string
	lines = append(lines, f.speech)
	if !f.leftOut {
		lines = append(lines, "This recording started before Live Context was on, so the panel may be in it.")
	}
	lines = append(lines, f.archive, f.verdict, f.wrote, f.dropped)
	if f.speech == "" || f.transient {
		switch all, person := f.words(); {
		case all == 0:
			lines = append(lines, "Listening…")
		case person == 1:
			lines = append(lines, fmt.Sprintf("Heard %d words in the last minute, 1 of them yours.", all))
		default:
			lines = append(lines, fmt.Sprintf("Heard %d words in the last minute, %d of them yours.", all, person))
		}
	}
	lines = append(lines, f.answered)
	var said []string
	for _, l := range lines {
		if l != "" {
			said = append(said, l)
		}
	}
	p.Status = strings.Join(said, " ")
	return p
}

// recent counts the words said in the last f.cfg.Window, the person's and
// everyone else's, by when they were said: the window itself is only
// trimmed when words come.
func (f *follow) recent(now time.Time) (person, others int) {
	since := now.Add(-f.cfg.Window)
	for _, h := range f.heard {
		if h.To.Before(since) {
			continue
		}
		if n := len(strings.Fields(h.Text)); h.Speaker == Person {
			person += n
		} else {
			others += n
		}
	}
	return person, others
}

// headline is the line under the panel's title: whether speech may be
// recognized, the words heard in the last minute, the person's and
// everyone else's, and how the last question to On the Record went. It
// changes no more often than every five seconds unless something happens,
// so the panel isn't redrawn under a click.
func (f *follow) headline(now time.Time) string {
	speech := "Speech: no answer yet"
	switch f.allowed {
	case SpeechAllowed:
		speech = "Speech allowed"
	case SpeechRefused:
		speech = "Speech not allowed"
	case SpeechUnsupported:
		speech = "Speech can't run on this Mac"
	}
	person, others := f.recent(now)
	ask := "not asked yet"
	switch {
	case f.asking != 0:
		ask = "asking…"
	case !f.resultAt.IsZero():
		ask = "asked " + ago(now.Sub(f.resultAt)) + ": " + f.result
	}
	return fmt.Sprintf("%s · last minute: %d words yours, %d others · %s", speech, person, others, ask)
}

// ago says how long ago, in steps of five seconds, then of minutes.
func ago(d time.Duration) string {
	switch {
	case d < 5*time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%d s ago", int(d/(5*time.Second))*5)
	}
	return fmt.Sprintf("%d min ago", int(d/time.Minute))
}

func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

// brief is an error in On Air's own words: what it says before a colon.
// What follows one may come from another program, such as On the Record,
// and is left out.
func brief(err error) string {
	said, _, _ := strings.Cut(err.Error(), ": ")
	return line(strings.TrimSuffix(strings.TrimSpace(said), "."), 80)
}

// shows is what the panel shows, for the diagnostic log: the kinds of line
// in its status, and counts. Never what was said, what went wrong in
// another program's words, or what On the Record answered.
func (f *follow) shows(p Panel, now time.Time) string {
	if !p.Visible {
		return "hidden"
	}
	var kinds []string
	add := func(on bool, kind string) {
		if on {
			kinds = append(kinds, kind)
		}
	}
	add(!f.recording, "recording stopped")
	add(f.speech != "" && f.transient, "speech failed")
	add(f.speech != "" && !f.transient && f.allowed == SpeechRefused, "speech refused")
	add(f.speech != "" && !f.transient && f.allowed == SpeechUnsupported, "speech unsupported")
	add(!f.leftOut, "panel may be in the recording")
	add(f.archive != "", "question failed")
	add(f.verdict != "" && !f.loading, "model failed")
	add(f.loading, "model loading")
	add(f.wrote != "", "writing failed")
	add(p.Differs.Text != "", "differs from the record")
	add(f.dropped != "", "sound dropped")
	add(f.answered != "", "nothing new")
	if len(kinds) == 0 {
		kinds = append(kinds, "listening")
	}
	person, others := f.recent(now)
	lines, written := 0, 0
	for _, a := range p.Answers {
		lines += len(a.Lines)
		if a.Text != "" {
			written++
		}
	}
	return fmt.Sprintf("shown, %d words yours and %d others in the last minute, %s, %s, %s with %s, %d written; status: %s",
		person, others, plural(len(p.Links), "link"), plural(len(p.Calls), "call"), plural(len(p.Answers), "answer card"), plural(lines, "line"), written, strings.Join(kinds, ", "))
}

// sentence ends what another part said with a full stop, if it has none.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no reason was given."
	}
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, "?") || strings.HasSuffix(s, "!") {
		return s
	}
	return s + "."
}

// card is an answer card: a question noticed in the call, and what On the
// Record answered about it. One asked early, while its sentence was still
// being said, isn't shown until the finished sentence asks the same.
type card struct {
	n     int
	q     question
	early bool
	// failed says On the Record couldn't answer it while it was asked
	// early: it is asked again once, if its sentence asks the same.
	failed bool
	// search is what On the Record is asked: the question as heard, and
	// the writer's other words for what it asks about, once it has
	// reworded it, or given up on that.
	search   string
	reworded bool
	asked    time.Time // when On the Record was asked, or zero while it waits
	brief    *Brief    // what it answered, once it did
	done     bool      // its answer came, or it was given up on
	lines    []AnswerLine
	note     string
	// For the writer: the lines of the brief it answers from, numbered
	// from 1 in this order, whether it waits to be written, and the
	// answer and its sources once written and checked.
	from    []Point
	write   bool
	text    string
	sources []AnswerLine
	// When each step began and ended, for the diagnostic log.
	made, noticed, rewordAsked, rewordCame, fetched, writeAsked time.Time
}

// recognized takes a result of speech recognition, while listening, and
// returns the sentences it finished.
func (f *follow) recognized(s Said) []utterance {
	if !f.listening() {
		return nil
	}
	return f.stream.take(s)
}

// flush hands on a sentence held at the end of a request that the next
// doesn't carry on, or that no result followed within a request's length
// and five seconds.
func (f *follow) flush(now time.Time) []utterance {
	if !f.listening() {
		f.stream = stream{}
		return nil
	}
	return f.stream.flush(now, f.cfg.Chunk+5*time.Second)
}

// ask takes a question noticed in the call. A card for it goes on top, to
// be asked, and the panel keeps f.cfg.Cards, unless the same question was
// asked within f.cfg.Again. A question asked early that asks the same
// becomes its card, with what was found for it so far.
func (f *follow) ask(q question, now time.Time) (n int, again, kept bool) {
	if f.cfg.Cards <= 0 {
		return 0, false, false
	}
	f.noticed++
	if f.askedLately(q, now) {
		f.dropEarly()
		return 0, true, false
	}
	f.lately = append(f.lately, lately{keys: q.keys, at: now})
	c := f.earlyCard()
	if c != nil && same(c.q.keys, q.keys) {
		extra := c.q.extra
		c.early, c.q, c.noticed = false, q, now
		c.q.extra = extra
		f.keptEarly++
		kept = true
		switch {
		case c.failed:
			c.failed, c.asked = false, time.Time{}
		case c.brief != nil:
			f.settle(c)
		}
	} else {
		f.dropEarly()
		f.cardsMade++
		c = &card{n: f.cardsMade, q: q, made: now, noticed: now}
		f.cards = append([]*card{c}, f.cards...)
	}
	shown := 0
	f.cards = slices.DeleteFunc(f.cards, func(c *card) bool {
		if c.early {
			return false
		}
		shown++
		return shown > f.cfg.Cards
	})
	return c.n, false, kept
}

// askedLately says whether the same question was asked within
// f.cfg.Again, and forgets those asked before.
func (f *follow) askedLately(q question, now time.Time) bool {
	kept := f.lately[:0]
	for _, l := range f.lately {
		if now.Sub(l.at) < f.cfg.Again {
			kept = append(kept, l)
		}
	}
	f.lately = kept
	return slices.ContainsFunc(f.lately, func(l lately) bool { return same(l.keys, q.keys) })
}

// early takes the sentence being said, as notice reads it, when it asks
// something worth asking: On the Record is asked about it before it is
// finished, on a card of its own that isn't shown, one at a time. One
// asked early that the sentence no longer asks the same as is dropped,
// and what is still out for it given up on. It says which card it started
// and which it dropped, if any.
func (f *follow) early(q question, asks bool, now time.Time) (started, dropped int) {
	c := f.earlyCard()
	if c != nil && asks && same(c.q.keys, q.keys) {
		return 0, 0
	}
	if c != nil {
		dropped = f.dropEarly()
	}
	if !asks || !f.listening() || f.cfg.Cards <= 0 || f.askedLately(q, now) {
		return 0, dropped
	}
	f.cardsMade++
	f.askedEarly++
	f.cards = append([]*card{{n: f.cardsMade, q: q, early: true, made: now}}, f.cards...)
	return f.cardsMade, dropped
}

// earlyCard is the card asked early, if there is one.
func (f *follow) earlyCard() *card {
	for _, c := range f.cards {
		if c.early {
			return c
		}
	}
	return nil
}

// dropEarly drops the card asked early, and returns its number, or 0.
func (f *follow) dropEarly() int {
	c := f.earlyCard()
	if c == nil {
		return 0
	}
	f.cards = slices.DeleteFunc(f.cards, func(d *card) bool { return d == c })
	if f.answering == c.n {
		f.answering = 0
	}
	if f.rewording == c.n {
		f.rewording = 0
	}
	f.droppedEarly++
	return c.n
}

// waits says whether a card's question is still being asked: On the
// Record's answer to it waits, and else whether the writer's.
func (f *follow) waits(n int) bool { return n != 0 && f.answering == n }

// order is the cards in the order their steps are taken: those shown,
// oldest first, then the one asked early.
func (f *follow) order() []*card {
	var out []*card
	var early *card
	for i := len(f.cards) - 1; i >= 0; i-- {
		if c := f.cards[i]; c.early {
			early = c
		} else {
			out = append(out, c)
		}
	}
	if early != nil {
		out = append(out, early)
	}
	return out
}

// rewordDue says which card's question to have the writer reword for On
// the Record now: the first in order that hasn't been, nor asked, while
// listening with a writer chosen that isn't loading, and never while it
// writes or checks, or a round is out. One it returns is under way:
// reworded or abandonReword ends it.
func (f *follow) rewordDue(now time.Time) (Prompt, int, bool) {
	if !f.listening() || !f.writer || f.slow || f.rewording != 0 || f.writing != 0 || f.checking != 0 || f.inRound != 0 {
		return Prompt{}, 0, false
	}
	for _, c := range f.order() {
		if c.asked.IsZero() && !c.reworded {
			c.reworded, c.rewordAsked = true, now
			f.rewording, f.rewordAsked = c.n, now
			return rewordPrompt(c.q), c.n, true
		}
	}
	return Prompt{}, 0, false
}

// reworded takes what the writer wrote for a card's question: its other
// words for what the question asks about, which On the Record is asked
// with the question as heard, and the lines of its brief are ranked by.
// When it failed, the question goes as heard. It says how many words it
// added, and what went wrong in On Air's own words.
func (f *follow) reworded(n int, text string, err error, now time.Time) (added int, failed string, taken bool) {
	if n == 0 || n != f.rewording {
		return 0, "", false
	}
	f.rewording = 0
	c := f.card(n)
	if c == nil {
		return 0, "", true
	}
	c.rewordCame = now
	if err != nil {
		f.slow = errors.Is(err, ErrLoading)
		return 0, brief(err), true
	}
	f.slow = false
	c.q.extra = readReword(text, c.q)
	var more []string
	for _, k := range c.q.extra {
		more = append(more, k.word)
	}
	c.search = strings.TrimSpace(c.q.topic + " " + strings.Join(more, " "))
	return len(more), "", true
}

// abandonReword gives up on a rewording the writer is late with, so the
// question goes as heard.
func (f *follow) abandonReword(now time.Time) (n int) {
	if f.rewording == 0 || now.Sub(f.rewordAsked) <= f.cfg.RewordTimeout+250*time.Millisecond {
		return 0
	}
	n, f.rewording = f.rewording, 0
	return n
}

// ready says whether a card's question can go to On the Record: once the
// writer has reworded it or failed to, or at once without a writer, or
// one that is loading, or once RewordWait has gone by with the writer
// busy.
func (f *follow) ready(c *card, now time.Time) bool {
	switch {
	case !f.writer || f.slow:
		return true
	case c.reworded:
		return f.rewording != c.n
	}
	return now.Sub(c.made) >= RewordWait
}

// answerDue says which card's question to ask On the Record now: the
// first in order not yet asked, once it is ready, while listening, and
// never while another waits for its answer. A question it returns is
// under way: reply or abandonAnswer ends it.
func (f *follow) answerDue(now time.Time) (topic string, n int, ok bool) {
	if !f.listening() || f.answering != 0 {
		return "", 0, false
	}
	for _, c := range f.order() {
		if !c.asked.IsZero() {
			continue
		}
		if !f.ready(c, now) {
			return "", 0, false
		}
		c.asked, f.answering, f.answerAsked = now, c.n, now
		f.answers++
		if c.search == "" {
			c.search = c.q.topic
		}
		return c.search, c.n, true
	}
	return "", 0, false
}

// reply takes On the Record's answer to a card's question: the lines that
// answer it, or that the archive has none, or what went wrong. One that
// nothing waits for any more is dropped. A card asked early keeps its
// answer until its sentence is finished.
func (f *follow) reply(n int, b Brief, err error, now time.Time) (shown int, taken, write bool) {
	if n == 0 || n != f.answering {
		return 0, false, false
	}
	f.answering = 0
	if err == nil {
		f.remember(b, now)
	}
	c := f.card(n)
	if c == nil {
		return 0, true, false
	}
	c.fetched = now
	if err != nil {
		if c.early {
			// Asked again, once, when its sentence is finished.
			c.failed = true
			return 0, true, false
		}
		c.done, c.note = true, sentence(brief(err))
		return 0, true, false
	}
	c.brief = &b
	if c.early {
		return 0, true, false
	}
	return f.settle(c)
}

// settle takes the brief of a shown card: the lines that share the
// question's words are kept, and shown if the writer can't answer, and the
// lines the writer answers from are chosen.
func (f *follow) settle(c *card) (shown int, taken, write bool) {
	c.lines = answerLines(c.q, *c.brief, 3)
	if f.writer {
		if from := passages(c.q, *c.brief, f.cfg.AnswerTokens); len(from) > 0 {
			c.from, c.write = from, true
			return 0, true, true
		}
	}
	f.fallback(c)
	return len(c.lines), true, false
}

// fallback ends a card with the lines that share its question's words,
// or a word that the archive has none.
func (f *follow) fallback(c *card) {
	c.done, c.from = true, nil
	if len(c.lines) == 0 {
		c.note = "No answer in the archive."
	}
	f.lines += len(c.lines)
}

// writeDue says which card's answer to ask the writer for now: the oldest
// waiting, while listening, never while another answer, a check or a
// round is out. One it returns is under way: answerWritten or abandonWrite ends
// it.
func (f *follow) writeDue(now time.Time) (Prompt, int, bool) {
	if !f.listening() || !f.writer || f.writing != 0 || f.checking != 0 || f.inRound != 0 || f.rewording != 0 {
		return Prompt{}, 0, false
	}
	for i := len(f.cards) - 1; i >= 0; i-- {
		if c := f.cards[i]; c.write {
			c.write, c.writeAsked = false, now
			f.writing, f.writeAsked = c.n, now
			return answerPrompt(c.q, c.from), c.n, true
		}
	}
	return Prompt{}, 0, false
}

// answerWritten takes what the writer wrote for a card, and shows it only if it
// checks out: its answer and sources, or that the archive has no answer,
// or that what it wrote didn't check out. When the writer failed, the
// card shows the lines that share the question's words. One that nothing
// waits for any more is dropped.
func (f *follow) answerWritten(n int, text string, err error) (o outcome, why string, taken bool) {
	if n == 0 || n != f.writing {
		return 0, "", false
	}
	f.writing = 0
	c := f.card(n)
	if c == nil {
		return 0, "", true
	}
	if err != nil {
		f.wrote = "Couldn't ask the local model for an answer: " + sentence(brief(err))
		if errors.Is(err, ErrLoading) {
			f.wrote = "The local model is still loading, so the answer shows lines that share the question's words."
		}
		f.fallback(c)
		return 0, "", true
	}
	f.wrote, f.slow = "", false
	f.written++
	shown, cited, o, why := readAnswer(text, len(c.from))
	closest := c.lines
	c.done, c.lines = true, nil
	switch o {
	case answered:
		f.answeredN++
		c.text = shown
		for i, k := range cited {
			p := c.from[k-1]
			c.sources = append(c.sources, AnswerLine{Text: fmt.Sprintf("[%d] %s", i+1, whereSaid(p)), URL: p.URL})
		}
	case notInArchive:
		f.notIn++
		// The model can be too strict when the notes use other words. If the
		// cheap ranking found lines about the question, show them rather
		// than nothing, and say the model found no direct answer.
		if len(closest) > 0 {
			c.lines = closest
			f.lines += len(closest)
			c.note = "No direct answer; the closest notes:"
			break
		}
		c.note = "Not in the archive."
	default:
		f.rejectedN++
		c.note = "The local model's answer didn't check out, so it isn't shown."
	}
	c.from = nil
	return o, why, true
}

// abandonWrite gives up on an answer the writer is late with by a second,
// and shows the lines that share the question's words.
func (f *follow) abandonWrite(now time.Time) (n int) {
	if f.writing == 0 || now.Sub(f.writeAsked) <= f.cfg.WriteTimeout+time.Second {
		return 0
	}
	n, f.writing = f.writing, 0
	f.wrote = fmt.Sprintf("Couldn't ask the local model for an answer: it didn't answer within %s.", f.cfg.WriteTimeout)
	if c := f.card(n); c != nil {
		f.fallback(c)
	}
	return n
}

// state takes a sentence said in the call that asks nothing. One that
// states a fact is kept for a Window, to be checked against the record.
func (f *follow) state(u utterance) {
	fs := facts(u.text)
	if len(fs) == 0 || !f.listening() {
		return
	}
	f.statements = append(f.statements, statement{text: u.text, at: u.to, facts: fs})
	if len(f.statements) > 4*checkSaid {
		f.statements = f.statements[len(f.statements)-4*checkSaid:]
	}
	f.newFact = true
}

// remember keeps the lines of a brief that state a fact, for f.cfg.Recall,
// to check what is said against.
func (f *follow) remember(b Brief, now time.Time) {
	for _, p := range b.Points {
		fs := facts(p.Text)
		if len(fs) == 0 || !openable(p.URL) {
			continue
		}
		f.records = slices.DeleteFunc(f.records, func(r record) bool { return r.p.URL == p.URL && r.p.Text == p.Text })
		f.records = append(f.records, record{p: p, at: now, facts: fs})
	}
	if len(f.records) > 200 {
		f.records = f.records[len(f.records)-200:]
	}
	// What was said before the brief came is checked against it too.
	for _, s := range f.statements {
		if now.Sub(s.at) < f.cfg.Window {
			f.newFact = true
		}
	}
}

// checkDue says whether to ask the writer now whether what was said in
// the last Window differs from the records of the last f.cfg.Recall, and
// what: while listening with a writer chosen, once something that states a
// fact was said since the last check, f.cfg.CheckEvery after it, never
// while an answer is written or waits to be, nor while a round is out, and
// only when a record shares a key word with what was said. A check it
// returns is under way: checkedBy or abandonCheck ends it.
func (f *follow) checkDue(now time.Time) (Prompt, int, int, int, bool) {
	if !f.listening() || !f.writer || !f.newFact || f.writing != 0 || f.checking != 0 || f.inRound != 0 || f.rewording != 0 {
		return Prompt{}, 0, 0, 0, false
	}
	if !f.checked.IsZero() && now.Sub(f.checked) < f.cfg.CheckEvery {
		return Prompt{}, 0, 0, 0, false
	}
	if f.pending() {
		return Prompt{}, 0, 0, 0, false
	}
	f.newFact = false
	f.records = slices.DeleteFunc(f.records, func(r record) bool { return now.Sub(r.at) >= f.cfg.Recall })
	var said []statement
	for _, s := range f.statements {
		if now.Sub(s.at) < f.cfg.Window {
			said = append(said, s)
		}
	}
	if len(said) > checkSaid {
		said = said[len(said)-checkSaid:]
	}
	type scored struct {
		r     record
		score int
	}
	for key, at := range f.differed {
		if now.Sub(at) >= f.cfg.Quiet {
			delete(f.differed, key)
		}
	}
	var near []scored
	for _, r := range f.records {
		if _, lately := f.differed[r.p.URL+"\x00"+r.p.Text]; lately {
			// Shown to differ lately: not asked about again yet.
			continue
		}
		score := 0
		for _, s := range said {
			score += related(s.text, r.p.Text)
		}
		if score > 0 {
			near = append(near, scored{r, score})
		}
	}
	slices.SortStableFunc(near, func(a, b scored) int { return b.score - a.score })
	var records []record
	used := 0
	for _, n := range near {
		cost := tokens(numbered(len(records)+1, n.r.p))
		if len(records) == checkRecords || used+cost > checkMost {
			break
		}
		used += cost
		records = append(records, n.r)
	}
	if len(said) == 0 || len(records) == 0 {
		return Prompt{}, 0, 0, 0, false
	}
	f.checks++
	f.checking, f.checked = f.checks, now
	f.check = checkAsked{said: said, records: records}
	return checkPrompt(said, records), f.checking, len(said), len(records), true
}

// checkedBy takes what the writer wrote about a check, and shows the
// difference it found only if it checks out and the same record wasn't
// shown to differ within f.cfg.Quiet. One difference is shown at a time,
// the newest, for f.cfg.Hold. It says what came of the check, in On Air's
// own words.
func (f *follow) checkedBy(n int, text string, err error, now time.Time) (result string, taken bool) {
	if n == 0 || n != f.checking {
		return "", false
	}
	f.checking = 0
	asked := f.check
	f.check = checkAsked{}
	if err != nil {
		return "failed: " + brief(err), true
	}
	f.checksBack++
	s, r, quoted, recorded, why := readCheck(text, asked.said, asked.records)
	switch {
	case why != "":
		f.checkRejected++
		return "rejected: " + why, true
	case s == nil:
		return "nothing differs", true
	}
	for key, at := range f.differed {
		if now.Sub(at) >= f.cfg.Quiet {
			delete(f.differed, key)
		}
	}
	key := r.p.URL + "\x00" + r.p.Text
	if _, lately := f.differed[key]; lately {
		return "differs, as shown lately", true
	}
	f.differed[key] = now
	f.found++
	f.differs = &differs{line: differsLine(*s, *r, quoted, recorded), at: now}
	return "differs, shown", true
}

// abandonCheck gives up on a check the writer is late with by a second.
func (f *follow) abandonCheck(now time.Time) (n int) {
	if f.checking == 0 || now.Sub(f.checked) <= f.cfg.WriteTimeout+time.Second {
		return 0
	}
	n, f.checking, f.check = f.checking, 0, checkAsked{}
	return n
}

// abandonAnswer gives up on a card's question whose answer is late by a
// second, so one that never returns doesn't stop the next.
func (f *follow) abandonAnswer(now time.Time) (n int) {
	if f.answering == 0 || now.Sub(f.answerAsked) <= f.cfg.Timeout+time.Second {
		return 0
	}
	n, f.answering = f.answering, 0
	if c := f.card(n); c != nil && c.early {
		f.dropEarly()
	} else if c != nil {
		c.done, c.note = true, fmt.Sprintf("On the Record didn't answer within %s.", f.cfg.Timeout)
	}
	return n
}

// pending says whether a card, shown or asked early, waits for its
// answer: the writer and the decision model leave the Mac's GPU to it
// meanwhile.
func (f *follow) pending() bool {
	return slices.ContainsFunc(f.cards, func(c *card) bool { return !c.done })
}

func (f *follow) card(n int) *card {
	for _, c := range f.cards {
		if c.n == n {
			return c
		}
	}
	return nil
}

// timing is how long the steps of a card's answer took, for the
// diagnostic log: from its question's last word to now, as it shows.
func (f *follow) timing(c *card, now time.Time) string {
	parts := []string{"noticed " + after(c.noticed, c.q.said) + " after it"}
	switch {
	case c.rewordAsked.IsZero():
		parts = append(parts, "not reworded")
	case c.rewordCame.IsZero():
		parts = append(parts, "not reworded in time")
	default:
		parts = append(parts, "reworded in "+after(c.rewordCame, c.rewordAsked))
	}
	if !c.asked.IsZero() && !c.fetched.IsZero() {
		fetch := "On the Record answered in " + after(c.fetched, c.asked)
		if c.asked.Before(c.noticed) {
			fetch += ", asked " + after(c.noticed, c.asked) + " before the question was finished"
		}
		parts = append(parts, fetch)
	}
	if !c.writeAsked.IsZero() {
		parts = append(parts, "written in "+after(now, c.writeAsked))
	}
	return fmt.Sprintf("card %d shown %s after its question's last word: %s", c.n, after(now, c.q.said), strings.Join(parts, ", "))
}

// after says how long after since now is, in seconds, or that it isn't
// known.
func after(now, since time.Time) string {
	if since.IsZero() || now.IsZero() {
		return "an unknown time"
	}
	return fmt.Sprintf("%.1f s", now.Sub(since).Seconds())
}

// briefTokens is about how many tokens all of a brief's lines take, as the
// writer is given them.
func briefTokens(b Brief) int {
	n := 0
	for i, p := range b.Points {
		n += tokens(numbered(i+1, p))
	}
	return n
}
