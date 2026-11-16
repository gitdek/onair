package live

import (
	"fmt"
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

	open        *utterance // the last utterance of a result, which the next may carry on
	cards       []*card    // answer cards, newest first
	cardsMade   int        // cards made so far, which number them
	answering   int        // the card whose question waits for an answer, or 0
	answerAsked time.Time  // when it was asked
	lately      []lately   // questions asked lately, which aren't asked again
	noticed     int        // questions noticed so far, for the diagnostic log
	answers     int        // and asked
	lines       int        // and lines of answers shown

	allowed   Problem // SpeechAllowed, SpeechRefused or SpeechUnsupported, once the Mac has said
	speech    string  // what is wrong with listening, if anything
	transient bool    // and it goes once words are heard again
	archive   string  // what went wrong asking On the Record, if anything
	answered  string  // a word on the last answer
	dropped   string  // that sound was left out, if it was
}

func newFollow(cfg Config) *follow {
	return &follow{cfg: cfg, shown: map[string]time.Time{}}
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
		f.heard, f.fresh, f.news, f.calls, f.open = nil, false, false, nil, nil
	}
	return ask
}

// judge says whether a judge is chosen. Without one, what it said goes.
func (f *follow) judge(on bool) {
	if !on {
		f.calls, f.verdict, f.inRound = nil, "", 0
	}
	f.judging = on
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
		f.news, f.calls, f.inRound, f.judged, f.verdict = false, nil, 0, time.Time{}, ""
		f.speech, f.transient, f.archive, f.answered, f.dropped = "", false, "", "", ""
		f.open, f.cards, f.answering, f.lately = nil, nil, 0, nil
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
	if !f.listening() || !f.judging || f.inRound != 0 || !f.news {
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
	if err != nil {
		f.verdict = "Couldn't ask the model: " + sentence(err.Error())
		return
	}
	f.verdict = ""
	if v.Topic >= Thresholds.Topic {
		f.links, f.calls, f.heard, f.fresh, f.news, f.answered = nil, nil, nil, false, false, ""
		return
	}
	calls := verdictCalls(v, records, now)
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
	f.inRound = 0
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
		a := Answer{Question: c.q.shown, Lines: append([]AnswerLine(nil), c.lines...), Note: c.note}
		if !c.done {
			a.Note = "Looking in On the Record…"
		}
		p.Answers = append(p.Answers, a)
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
	lines = append(lines, f.archive, f.verdict, f.dropped)
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
	add(f.verdict != "", "model failed")
	add(f.dropped != "", "sound dropped")
	add(f.answered != "", "nothing new")
	if len(kinds) == 0 {
		kinds = append(kinds, "listening")
	}
	person, others := f.recent(now)
	lines := 0
	for _, a := range p.Answers {
		lines += len(a.Lines)
	}
	return fmt.Sprintf("shown, %d words yours and %d others in the last minute, %s, %s, %s with %s; status: %s",
		person, others, plural(len(p.Links), "link"), plural(len(p.Calls), "call"), plural(len(p.Answers), "answer card"), plural(lines, "line"), strings.Join(kinds, ", "))
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
// Record answered about it.
type card struct {
	n     int
	q     question
	asked time.Time // when it was asked, or zero while it waits
	done  bool      // its answer came, or it was given up on
	lines []AnswerLine
	note  string
}

// said takes the sentences of a result, while listening, and returns
// those that ended. The last, when it runs to the end of its result's
// sound, is held: the next result carries it on when it starts as that
// result's sound does, within two Pauses of it, and else it ended too.
func (f *follow) said(ss []utterance) []utterance {
	if !f.listening() {
		return nil
	}
	var ended []utterance
	if held := f.open; held != nil {
		f.open = nil
		if len(ss) > 0 && ss[0].first && ss[0].from.Sub(held.to) <= 2*Pause+250*time.Millisecond && len(strings.Fields(held.text)) < 60 {
			joined := *held
			joined.text, joined.to, joined.ended = held.text+" "+ss[0].text, ss[0].to, ss[0].ended
			ss = append([]utterance{joined}, ss[1:]...)
		} else {
			ended = append(ended, *held)
		}
	}
	for i, s := range ss {
		if i == len(ss)-1 && s.ended == open {
			f.open = &s
			break
		}
		ended = append(ended, s)
	}
	return ended
}

// flush ends a held sentence that no result carried on within a result's
// length and five seconds.
func (f *follow) flush(now time.Time) []utterance {
	if !f.listening() {
		f.open = nil
	}
	if f.open == nil || now.Sub(f.open.to) < f.cfg.Chunk+5*time.Second {
		return nil
	}
	s := *f.open
	f.open, s.ended = nil, byTime
	return []utterance{s}
}

// ask takes a question noticed in the call. A card for it goes on top, to
// be asked, and the panel keeps f.cfg.Cards, unless the same question was
// asked within f.cfg.Again.
func (f *follow) ask(q question, now time.Time) (n int, again bool) {
	if f.cfg.Cards <= 0 {
		return 0, false
	}
	f.noticed++
	kept := f.lately[:0]
	for _, l := range f.lately {
		if now.Sub(l.at) < f.cfg.Again {
			kept = append(kept, l)
		}
	}
	f.lately = kept
	for _, l := range f.lately {
		if same(l.keys, q.keys) {
			return 0, true
		}
	}
	f.lately = append(f.lately, lately{keys: q.keys, at: now})
	f.cardsMade++
	f.cards = append([]*card{{n: f.cardsMade, q: q}}, f.cards...)
	if len(f.cards) > f.cfg.Cards {
		f.cards = f.cards[:f.cfg.Cards]
	}
	return f.cardsMade, false
}

// answerDue says which card's question to ask On the Record now: the
// oldest not yet asked, while listening, and never while another waits for
// its answer. A question it returns is under way: reply or abandonAnswer
// ends it.
func (f *follow) answerDue(now time.Time) (topic string, n int, ok bool) {
	if !f.listening() || f.answering != 0 {
		return "", 0, false
	}
	for i := len(f.cards) - 1; i >= 0; i-- {
		if c := f.cards[i]; c.asked.IsZero() {
			c.asked, f.answering, f.answerAsked = now, c.n, now
			f.answers++
			return c.q.topic, c.n, true
		}
	}
	return "", 0, false
}

// reply takes On the Record's answer to a card's question: the lines that
// answer it, or that the archive has none, or what went wrong. One that
// nothing waits for any more is dropped.
func (f *follow) reply(n int, b Brief, err error) (shown int, taken bool) {
	if n == 0 || n != f.answering {
		return 0, false
	}
	f.answering = 0
	c := f.card(n)
	if c == nil {
		return 0, true
	}
	c.done = true
	if err != nil {
		c.note = sentence(brief(err))
		return 0, true
	}
	c.lines = answerLines(c.q, b, 3)
	if len(c.lines) == 0 {
		c.note = "No answer in the archive."
	}
	f.lines += len(c.lines)
	return len(c.lines), true
}

// abandonAnswer gives up on a card's question whose answer is late by a
// second, so one that never returns doesn't stop the next.
func (f *follow) abandonAnswer(now time.Time) (n int) {
	if f.answering == 0 || now.Sub(f.answerAsked) <= f.cfg.Timeout+time.Second {
		return 0
	}
	n, f.answering = f.answering, 0
	if c := f.card(n); c != nil {
		c.done, c.note = true, fmt.Sprintf("On the Record didn't answer within %s.", f.cfg.Timeout)
	}
	return n
}

func (f *follow) card(n int) *card {
	for _, c := range f.cards {
		if c.n == n {
			return c
		}
	}
	return nil
}
