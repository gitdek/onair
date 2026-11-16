package live

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)

// at is t0 plus some seconds.
func at(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }

// listening is a follow that is on and recording, as from t0.
func listening(cfg Config) *follow {
	f := newFollow(cfg)
	f.enable(true)
	f.capturing("", true)
	f.record(true, t0)
	return f
}

func words(n int, word string) string {
	return strings.TrimSpace(strings.Repeat(word+" ", n))
}

func TestNothingIsHeardUnlessOnAndRecording(t *testing.T) {
	f := newFollow(Defaults)
	f.hear(Heard{Speaker: Person, Text: "not on", To: t0}, t0)
	f.enable(true)
	f.hear(Heard{Speaker: Person, Text: "not recording", To: t0}, t0)
	f.record(true, t0)
	f.hear(Heard{Speaker: Person, Text: "heard", To: t0}, t0)
	f.record(false, at(1))
	f.hear(Heard{Speaker: Person, Text: "stopped", To: at(1)}, at(1))
	if got := f.text(); got != "heard" {
		t.Fatalf("the window holds %q", got)
	}
	f.record(true, at(2))
	if got := f.text(); got != "" {
		t.Fatalf("a new recording starts with %q", got)
	}
	f.enable(false)
	f.hear(Heard{Speaker: Person, Text: "turned off", To: at(2)}, at(2))
	if got := f.text(); got != "" {
		t.Fatalf("with Live Context off the window holds %q", got)
	}
}

func TestTheWindowKeepsTheLastMinute(t *testing.T) {
	f := listening(Defaults)
	for s := 0; s <= 90; s += 15 {
		f.hear(Heard{Speaker: Others, Text: fmt.Sprintf("said at %d", s), From: at(float64(s) - 14), To: at(float64(s))}, at(float64(s)))
	}
	if got, want := f.text(), "said at 30\nsaid at 45\nsaid at 60\nsaid at 75\nsaid at 90"; got != want {
		t.Fatalf("the window holds %q, not %q", got, want)
	}
}

func TestTheWindowKeepsTheNewestWordsWhenItIsFull(t *testing.T) {
	cfg := Defaults
	cfg.Chars = 40
	f := listening(cfg)
	f.hear(Heard{Speaker: Others, Text: "one two three four five six seven", To: at(1)}, at(1))
	f.hear(Heard{Speaker: Person, Text: "eight nine ten eleven twelve", To: at(2)}, at(2))
	got := f.text()
	if len(got) > 40 || !strings.HasSuffix(got, "eight nine ten eleven twelve") || strings.Contains(got, "one") {
		t.Fatalf("a full window holds %q", got)
	}
	f.hear(Heard{Speaker: Person, Text: strings.Repeat("é", 100), To: at(3)}, at(3))
	if got := f.text(); len(got) > 40 || !strings.HasPrefix(got, "é") {
		t.Fatalf("one long piece leaves %q", got)
	}
}

func TestTimesThatCantBeRightAreTakenAsNow(t *testing.T) {
	f := listening(Defaults)
	f.hear(Heard{Speaker: Person, Text: "no time"}, at(5))
	f.hear(Heard{Speaker: Person, Text: "in 1970", From: time.Unix(1, 0), To: time.Unix(2, 0)}, at(6))
	f.hear(Heard{Speaker: Person, Text: "next week", To: at(7 * 24 * 3600)}, at(7))
	f.hear(Heard{Speaker: 9, Text: "nobody", To: at(8)}, at(8))
	if got := f.text(); got != "no time\nin 1970\nnext week\nnobody" {
		t.Fatalf("the window holds %q", got)
	}
	if f.heard[3].Speaker != Others {
		t.Fatalf("an unknown speaker is taken as %v", f.heard[3].Speaker)
	}
	// Still a minute later, all of them are gone.
	f.hear(Heard{Speaker: Person, Text: "later", To: at(70)}, at(70))
	if got := f.text(); got != "later" {
		t.Fatalf("a minute on, the window holds %q", got)
	}
}

func TestTheTextHoldsBothSpeakersInTheOrderTheySpoke(t *testing.T) {
	f := listening(Defaults)
	// Each sound's words come when its own request ends, so later words
	// can come first.
	f.hear(Heard{Speaker: Person, Text: "  shall we  move\nthe launch ", From: at(20), To: at(24)}, at(25))
	f.hear(Heard{Speaker: Others, Text: "the pricing is not settled", From: at(12), To: at(26)}, at(27))
	if got := f.text(); got != "the pricing is not settled\nshall we move the launch" {
		t.Fatalf("the text is %q", got)
	}
	if all, person := f.words(); all != 10 || person != 5 {
		t.Fatalf("counted %d words, %d of them the person's", all, person)
	}
}

func TestAQuestionWaitsForWordsAndForItsTime(t *testing.T) {
	f := listening(Defaults)
	if _, _, ok := f.due(at(1)); ok {
		t.Fatal("asked with nothing said")
	}
	f.hear(Heard{Speaker: Others, Text: words(Defaults.Least-1, "word"), To: at(15)}, at(15))
	if _, _, ok := f.due(at(15)); ok {
		t.Fatal("asked with too few words")
	}
	f.hear(Heard{Speaker: Person, Text: "enough", To: at(16)}, at(16))
	text, ask, ok := f.due(at(16))
	if !ok || ask == 0 || !strings.HasSuffix(text, "enough") {
		t.Fatalf("no question once enough was said: %q %d %v", text, ask, ok)
	}
	f.hear(Heard{Speaker: Person, Text: "more", To: at(17)}, at(17))
	if _, _, ok := f.due(at(40)); ok {
		t.Fatal("asked again while the first question waited")
	}
	f.answer(ask, Found{}, nil, at(18))
	if _, _, ok := f.due(at(20)); ok {
		t.Fatal("asked again before its time")
	}
	if _, next, ok := f.due(at(31)); !ok || next == ask {
		t.Fatalf("no second question once something new was said and its time came: %d %v", next, ok)
	}
	f.answer(ask+1, Found{}, nil, at(32))
	if _, _, ok := f.due(at(60)); ok {
		t.Fatal("asked again with nothing new said")
	}
	f.hear(Heard{Speaker: Others, Text: "new words", To: at(61)}, at(61))
	if _, _, ok := f.due(at(61)); !ok {
		t.Fatal("no question once something new was said")
	}
}

// strong is an answer whose meetings the archive suggests linking.
func strong(ids ...string) Found {
	var f Found
	for _, id := range ids {
		f.Meetings = append(f.Meetings, Meeting{ID: id, Title: "Meeting " + id, Date: "2026-11-0" + id[len(id)-1:], URL: "http://127.0.0.1:8797/m/" + id,
			Why: []string{"close in meaning (0.70)"}})
		f.StrongMeetings = append(f.StrongMeetings, id)
	}
	return f
}

// asked has f ask, at a time, with something new said first.
func asked(t *testing.T, f *follow, now time.Time) int {
	t.Helper()
	f.hear(Heard{Speaker: Others, Text: words(Defaults.Least, "word"), To: now}, now)
	_, ask, ok := f.due(now)
	if !ok {
		t.Fatalf("no question at %v", now)
	}
	return ask
}

func titles(links []Link) string {
	var ts []string
	for _, l := range links {
		ts = append(ts, l.Title)
	}
	return strings.Join(ts, ", ")
}

func TestNewLinksGoOnTopAndNoneComesBackForAWhile(t *testing.T) {
	f := listening(Defaults)
	f.answer(asked(t, f, at(15)), strong("m1", "m2"), nil, at(15))
	if got := titles(f.panel(at(15)).Links); got != "Meeting m1, Meeting m2" {
		t.Fatalf("the panel shows %s", got)
	}
	f.answer(asked(t, f, at(30)), strong("m3", "m1", "m4"), nil, at(30))
	if got := titles(f.panel(at(30)).Links); got != "Meeting m3, Meeting m4, Meeting m1" {
		t.Fatalf("the panel shows %s", got)
	}
	// Nothing new: the panel stays, and says so.
	f.answer(asked(t, f, at(45)), strong("m1", "m3"), nil, at(45))
	p := f.panel(at(45))
	if got := titles(p.Links); got != "Meeting m3, Meeting m4, Meeting m1" || !strings.Contains(p.Status, "Nothing new from On the Record at 15:00.") {
		t.Fatalf("an answer with nothing new shows %s and %q", got, p.Status)
	}
	// Five minutes after it was shown, m1 may be shown again.
	f.answer(asked(t, f, at(15+300)), strong("m1"), nil, at(15+300))
	if got := titles(f.panel(at(315)).Links); got != "Meeting m1, Meeting m3, Meeting m4" {
		t.Fatalf("five minutes on, the panel shows %s", got)
	}
}

func TestAFailedQuestionIsSaidAndTheLinksStay(t *testing.T) {
	f := listening(Defaults)
	f.answer(asked(t, f, at(15)), strong("m1"), nil, at(15))
	f.answer(asked(t, f, at(30)), Found{}, errors.New("On the Record isn't answering on this Mac. Is its server running?"), at(30))
	p := f.panel(at(30))
	if titles(p.Links) != "Meeting m1" || !strings.Contains(p.Status, "Couldn't ask On the Record: On the Record isn't answering on this Mac. Is its server running?") {
		t.Fatalf("a failed question shows %+v", p)
	}
	f.answer(asked(t, f, at(45)), strong("m2"), nil, at(45))
	if p := f.panel(at(45)); strings.Contains(p.Status, "Couldn't ask") {
		t.Fatalf("an answer after a failure still shows %q", p.Status)
	}
}

func TestAnAnswerNothingWaitsForIsDropped(t *testing.T) {
	f := listening(Defaults)
	first := asked(t, f, at(15))
	// No answer within the time a question may take, and a second more:
	// it is given up on, and the next question can be asked.
	f.abandon(at(15).Add(Defaults.Timeout))
	if f.asking == 0 {
		t.Fatal("a question was given up on before its time")
	}
	f.abandon(at(15).Add(Defaults.Timeout + 1001*time.Millisecond))
	if p := f.panel(at(20)); !strings.Contains(p.Status, "didn't answer within 10s") {
		t.Fatalf("a question given up on shows %q", p.Status)
	}
	second := asked(t, f, at(31))
	f.answer(first, strong("late"), nil, at(32))
	if len(f.links) != 0 || f.asking != second {
		t.Fatalf("a late answer was taken: %v", f.links)
	}
	// A question from the last recording is dropped too.
	f.record(false, at(33))
	f.record(true, at(34))
	f.answer(second, strong("old"), nil, at(35))
	if len(f.links) != 0 {
		t.Fatalf("an answer from the last recording was taken: %v", f.links)
	}
}

func TestThePanelShowsWhileOnAndRecording(t *testing.T) {
	f := newFollow(Defaults)
	if p := f.panel(t0); p.Visible {
		t.Fatal("the panel shows before Live Context is on")
	}
	f.capturing("display-2", true)
	f.record(true, t0)
	if p := f.panel(t0); p.Visible {
		t.Fatal("the panel shows while recording with Live Context off")
	}
	if !f.enable(true) {
		t.Fatal("turning Live Context on doesn't ask for speech recognition")
	}
	if f.enable(true) {
		t.Fatal("Live Context asks again while it stays on")
	}
	p := f.panel(t0)
	if !p.Visible || p.Display != "display-2" || p.Status != "Listening…" {
		t.Fatalf("while on and recording the panel shows %+v", p)
	}
	f.hear(Heard{Speaker: Person, Text: "one two", To: at(1)}, at(1))
	f.hear(Heard{Speaker: Others, Text: "three", To: at(2)}, at(2))
	if p := f.panel(at(2)); p.Status != "Heard 3 words in the last minute, 2 of them yours." {
		t.Fatalf("after words the panel says %q", p.Status)
	}
	f.enable(false)
	if p := f.panel(at(3)); p.Visible || len(p.Links) != 0 {
		t.Fatalf("turned off, the panel shows %+v", p)
	}
}

func TestThePanelStaysAWhileAfterTheRecordingWithItsLinks(t *testing.T) {
	f := listening(Defaults)
	f.record(false, at(10))
	if p := f.panel(at(10)); p.Visible {
		t.Fatal("a panel with no links stays after the recording")
	}
	f.record(true, at(20))
	f.answer(asked(t, f, at(35)), strong("m1"), nil, at(35))
	f.record(false, at(40))
	p := f.panel(at(41))
	if !p.Visible || titles(p.Links) != "Meeting m1" || p.Status != "The recording stopped. The panel goes at 15:02." {
		t.Fatalf("after the recording the panel shows %+v", p)
	}
	if p := f.panel(at(40 + 120)); p.Visible {
		t.Fatal("the panel stays longer than two minutes after the recording")
	}
	f.record(true, at(200))
	if p := f.panel(at(200)); len(p.Links) != 0 {
		t.Fatalf("a new recording starts with %v", p.Links)
	}
}

func TestWhatGoesWrongIsSaidInThePanel(t *testing.T) {
	f := listening(Defaults)
	f.report(SpeechRefused, "it was refused", t0)
	p := f.panel(t0)
	if !p.Visible || p.Status != "Live Context can't listen: speech recognition isn't allowed (it was refused). Allow On Air in System Settings, Privacy & Security, Speech Recognition, then start a new recording." {
		t.Fatalf("a refusal shows %q", p.Status)
	}
	// A refusal stays until recognition is found allowed.
	f.report(SpeechFailed, "something", t0)
	if p := f.panel(t0); !strings.HasPrefix(p.Status, "Live Context can't listen: speech recognition isn't allowed") {
		t.Fatalf("a refusal was replaced: %q", p.Status)
	}
	f.report(SpeechAllowed, "", t0)
	if p := f.panel(t0); p.Status != "Listening…" {
		t.Fatalf("once allowed the panel shows %q", p.Status)
	}
	f.record(false, at(1))
	f.record(true, at(2))
	f.report(SpeechUnsupported, "English (United States)", at(2))
	if p := f.panel(at(2)); p.Status != "Live Context can't listen: this Mac can't recognize English (United States) on its own, and On Air doesn't send the call's words to Apple." {
		t.Fatalf("a language the Mac can't recognize shows %q", p.Status)
	}
	f.record(false, at(3))
	f.record(true, at(4))
	f.report(SpeechFailed, "the service went away (kAFAssistantErrorDomain 1101)", at(4))
	if p := f.panel(at(4)); p.Status != "Speech recognition had a problem, and keeps trying: the service went away (kAFAssistantErrorDomain 1101). Listening…" {
		t.Fatalf("a recognition error shows %q", p.Status)
	}
	f.hear(Heard{Speaker: Person, Text: "it works again", To: at(5)}, at(5))
	if p := f.panel(at(5)); strings.Contains(p.Status, "problem") {
		t.Fatalf("words heard after a recognition error still show %q", p.Status)
	}
	f.report(SoundDropped, "12", at(6))
	if p := f.panel(at(6)); !strings.HasPrefix(p.Status, "Live Context fell behind at 15:00 and skipped some of the sound.") {
		t.Fatalf("dropped sound shows %q", p.Status)
	}
	// A recording whose capture started before Live Context was on says
	// that the panel may be in it.
	f.record(false, at(7))
	f.capturing("", false)
	f.record(true, at(8))
	if p := f.panel(at(8)); p.Status != "This recording started before Live Context was on, so the panel may be in it. Listening…" {
		t.Fatalf("a recording that doesn't leave the panel out shows %q", p.Status)
	}
}

func TestTheHeadlineSaysWhatTheLiveSideDid(t *testing.T) {
	f := listening(Defaults)
	if got := f.panel(t0).Headline; got != "Speech: no answer yet · last minute: 0 words yours, 0 others · not asked yet" {
		t.Fatalf("at first the headline says %q", got)
	}
	f.report(SpeechAllowed, "", t0)
	f.hear(Heard{Speaker: Person, Text: "one two", To: at(1)}, at(1))
	first := asked(t, f, at(2))
	if got := f.panel(at(3)).Headline; got != "Speech allowed · last minute: 2 words yours, 8 others · asking…" {
		t.Fatalf("while asking the headline says %q", got)
	}
	f.answer(first, strong("m1"), nil, at(4))
	if got := f.panel(at(14)).Headline; got != "Speech allowed · last minute: 2 words yours, 8 others · asked 10 s ago: 1 link" {
		t.Fatalf("after an answer the headline says %q", got)
	}
	// Words leave the last minute as time passes, though nothing new is
	// heard.
	if got := f.panel(at(61.5)).Headline; got != "Speech allowed · last minute: 0 words yours, 8 others · asked 55 s ago: 1 link" {
		t.Fatalf("a minute on the headline says %q", got)
	}
	// What went wrong is said in On Air's own words, without what another
	// program added.
	f.answer(asked(t, f, at(70)), Found{}, errors.New("On the Record refused the question: what it said"), at(71))
	if got := f.panel(at(71)).Headline; !strings.HasSuffix(got, " · asked just now: On the Record refused the question") {
		t.Fatalf("after a failed question the headline says %q", got)
	}
	asked(t, f, at(90))
	f.abandon(at(90).Add(Defaults.Timeout + 2*time.Second))
	if got := f.panel(at(102)).Headline; !strings.HasSuffix(got, " · asked 10 s ago: no answer within 10s") {
		t.Fatalf("after a question given up on the headline says %q", got)
	}
	f.report(SpeechRefused, "it was refused", at(103))
	if got := f.panel(at(103)).Headline; !strings.HasPrefix(got, "Speech not allowed · ") {
		t.Fatalf("after a refusal the headline says %q", got)
	}
}
