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

	shown map[string]time.Time // when each link was last put on the panel
	links []Link               // on the panel, newest first

	judging bool      // a judge is chosen
	judged  time.Time // when the last round was asked
	inRound int       // the round waiting for a verdict, or 0
	rounds  int       // rounds asked so far, which number them
	news    bool      // something new was said since the last round
	calls   []call    // what the judge said, newest first, one of each kind
	verdict string    // what went wrong asking the judge, if anything

	speech    string // what is wrong with listening, if anything
	transient bool   // and it goes once words are heard again
	archive   string // what went wrong asking On the Record, if anything
	answered  string // a word on the last answer
	dropped   string // that sound was left out, if it was
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
		f.heard, f.fresh, f.news, f.calls = nil, false, false, nil
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
		f.asking, f.asked = 0, time.Time{}
		f.news, f.calls, f.inRound, f.judged, f.verdict = false, nil, 0, time.Time{}, ""
		f.speech, f.transient, f.archive, f.answered, f.dropped = "", false, "", "", ""
	case !on && f.recording:
		f.stopped = now
	}
	f.recording = on
}

// report takes what the Mac says about listening.
func (f *follow) report(p Problem, detail string, now time.Time) {
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
		return
	}
	f.archive = ""
	if f.merge(pick(found, f.cfg.Most), now) == 0 {
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
	lingering := !f.recording && len(f.links) > 0 && !f.stopped.IsZero() && now.Sub(f.stopped) < f.cfg.Linger
	p := Panel{Visible: f.on && (f.recording || lingering), Display: f.display, Links: append([]Link(nil), f.links...)}
	if !p.Visible {
		p.Links = nil
		return p
	}
	for _, c := range f.calls {
		if now.Sub(c.at) < f.cfg.Hold {
			p.Calls = append(p.Calls, c.text)
		}
	}
	if !f.recording {
		p.Status = fmt.Sprintf("The recording stopped. These links go at %s.", f.stopped.Add(f.cfg.Linger).Format("15:04"))
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
