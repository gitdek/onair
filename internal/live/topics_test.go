package live

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// madeUpLibrary is a made-up archive's lists: three meetings, newest
// first, four open items, two of them on its owners' lane, and a notebook
// entry.
func madeUpLibrary() Library {
	m := func(n string) string { return "https://archive.example/m/" + n }
	return Library{
		Meetings: []Meeting{
			{ID: "m-3", Title: "Quillstone pottery review", Date: "2026-03-10", URL: m("3"), People: []string{"Ana Lima", "Ben Ortiz", "Priya Shah"},
				Summary: "Glaze colours, the Harbor Labs kiln trial and the open studio date."},
			{ID: "m-2", Title: "Ana / Ben / Priya planning", Date: "2026-03-03", URL: m("2"), People: []string{"Ana Lima", "Ben Ortiz", "Priya Shah"},
				Summary: "Getting ready for the Quillstone call."},
			{ID: "m-1", Title: "Website Redesign Discussion", Date: "2026-02-20", URL: m("1"), People: []string{"Ana Lima", "Ben Ortiz", "Tomas Weber"},
				Summary: "Early scope, a website redesign."},
		},
		Items: []Item{
			{ID: "t-1", Owner: "Ana/Ben", Ours: true, Status: "open", Text: "Send Priya the Quillstone glaze chart. No date was set.", Meeting: "Quillstone pottery review", MeetingDate: "2026-03-10", URL: m("3?t=100")},
			{ID: "t-2", Owner: "Ben, helped by Ana", Ours: true, Status: "open", Text: "Book the Harbor Labs kiln visit", Meeting: "Ana / Ben / Priya planning", MeetingDate: "2026-03-03", URL: m("2?t=50")},
			{ID: "t-3", Owner: "Priya", Status: "open", Text: "Share the cost of the kiln trial", Meeting: "Quillstone pottery review", MeetingDate: "2026-03-10", URL: m("3?t=200")},
			{ID: "t-4", Owner: "Group", Status: "open", Text: "Plan the next review", URL: "https://elsewhere.example/private"},
		},
		Notes: []Note{{ID: "n-1", Title: "Quillstone: kiln notes (Mar 10)", Tags: []string{"quillstone"}, Updated: "2026-03-11T10:00:00.000Z", URL: "https://archive.example/notebook/n-1"}},
	}
}

func rosterTitles(r roster, person bool) []string {
	var out []string
	for _, s := range r.all {
		if s.person == person {
			out = append(out, s.title)
		}
	}
	return out
}

func TestTheArchivesNamesAreLearnedFromItsLists(t *testing.T) {
	r := learn(madeUpLibrary())
	if got := rosterTitles(r, false); !slices.Equal(got, []string{"Quillstone", "Harbor Labs"}) {
		t.Errorf("projects and companies: %q", got)
	}
	if got := rosterTitles(r, true); !slices.Equal(got, []string{"Ana Lima", "Ben Ortiz", "Priya Shah", "Tomas Weber"}) {
		t.Errorf("people: %q", got)
	}
	var ours []string
	for _, s := range r.all {
		if s.ours {
			ours = append(ours, s.title)
		}
	}
	if !slices.Equal(ours, []string{"Ana Lima", "Ben Ortiz"}) {
		t.Errorf("owners: %q", ours)
	}
	if r.home != "archive.example" {
		t.Errorf("home %q", r.home)
	}
	q := r.all[slices.IndexFunc(r.all, func(s subject) bool { return s.title == "Quillstone" })]
	if !slices.Equal(q.meetings, []int{0, 1}) || !slices.Equal(q.items, []int{0}) || !slices.Equal(q.notes, []int{0}) {
		t.Errorf("where the lists hold Quillstone: meetings %v, items %v, notes %v", q.meetings, q.items, q.notes)
	}
	p := r.all[slices.IndexFunc(r.all, func(s subject) bool { return s.title == "Priya Shah" })]
	if !slices.Equal(p.items, []int{0, 2}) {
		t.Errorf("Priya's items, and those naming her: %v", p.items)
	}
}

func TestANameIsHeardAsSaidSplitOrMisheard(t *testing.T) {
	r := learn(madeUpLibrary())
	heard := func(text string) []string {
		var out []string
		for _, k := range r.mentions(text, Defaults) {
			out = append(out, r.all[k].title)
		}
		return out
	}
	for text, want := range map[string][]string{
		"so the Quillstone numbers came in":        {"Quillstone"},
		"we should ask quill stone about it":       {"Quillstone"},
		"the quilstone deal":                       {"Quillstone"},
		"the quillsome deal":                       nil,
		"what about quillen stone then":            {"Quillstone"},
		"the quill and the stone":                  nil,
		"I'd say Quilstone":                        {"Quillstone"},
		"Millstone is heavy":                       nil,
		"a millstone round the neck":               nil,
		"Harbour labs wants a kiln":                {"Harbor Labs"},
		"did priya send it":                        {"Priya Shah"},
		"I talked to Pria yesterday":               {"Priya Shah"},
		"Ben thinks so":                            {"Ben Ortiz"},
		"we asked Benn":                            {"Ben Ortiz"},
		"we asked benn":                            nil,
		"it's been a while":                        nil,
		"the harbor is nice this time of year":     nil,
		"the price is right":                       nil,
		"been there, done that":                    nil,
		"tomas weber and Priya and the quillstone": {"Priya Shah", "Tomas Weber", "Quillstone"},
	} {
		got := heard(text)
		slices.Sort(got)
		w := slices.Clone(want)
		slices.Sort(w)
		if !slices.Equal(got, w) {
			t.Errorf("%q: heard %q, not %q", text, got, w)
		}
	}
}

// Every name the archive knows is listened for in each sentence, several
// times a second while one is being said, so how a word sounds is worked
// out without building its table of letters that sound alike each time.
func TestHowAWordSoundsBuildsNoTableEachTime(t *testing.T) {
	if n := testing.AllocsPerRun(200, func() { sound("quillstone") }); n > 10 {
		t.Errorf("how a word sounds takes %.0f allocations", n)
	}
	if sound("benn") != sound("ben") || sound("quilstone") != sound("quillstone") || sound("pria") != sound("priya") {
		t.Error("words that sound alike don't")
	}
}

// topical is a follow listening with a writer, the made-up library in.
func topical(t *testing.T) *follow {
	t.Helper()
	f := listening(Defaults)
	f.writes(true)
	f.listenedAt = t0
	f.library(madeUpLibrary(), nil, t0)
	return f
}

func TestTheStandingCardShowsTheOwnersOpenItemsAndThoseOfPeopleNamedFirst(t *testing.T) {
	f := topical(t)
	p := f.panel(t0)
	if len(p.Cards) != 1 || p.Cards[0].Title != "Open items" {
		t.Fatalf("cards: %+v", p.Cards)
	}
	if got := p.Cards[0].Lines; len(got) != 2 || got[0].Text != "Ana/Ben: Send Priya the Quillstone glaze chart." || got[0].Sources[0] != (Source{Label: "Mar 10", URL: "https://archive.example/m/3?t=100"}) ||
		got[1].Text != "Ben: Book the Harbor Labs kiln visit" {
		t.Fatalf("lines: %+v", got)
	}
	// Priya, named in the first minute, adds hers.
	f.heardNames("hi Priya, good to see you", true, t0.Add(20*time.Second))
	got := f.panel(t0.Add(20 * time.Second)).Cards
	i := slices.IndexFunc(got, func(c Card) bool { return c.Title == "Open items" })
	if i < 0 || len(got[i].Lines) != 3 || !slices.ContainsFunc(got[i].Lines, func(l CardLine) bool { return l.Text == "Priya: Share the cost of the kiln trial" }) {
		t.Fatalf("after Priya was named: %+v", got)
	}
	// Named after the first minute, a person adds nothing to it.
	f.heardNames("and Tomas Weber too", true, t0.Add(2*time.Minute))
	if c := f.standing; len(c.lines) != 3 {
		t.Errorf("the standing card has %d lines", len(c.lines))
	}
}

func TestACardAboutANameComesAtOnceWithWhatTheListsHold(t *testing.T) {
	f := topical(t)
	at := t0.Add(2 * time.Minute)
	hs := f.heardNames("so the quill stone numbers", false, at)
	if len(hs) != 1 || !hs[0].started || hs[0].person {
		t.Fatalf("heard %+v", hs)
	}
	p := f.panel(at)
	if len(p.Cards) != 2 || p.Cards[0].Title != "Quillstone" {
		t.Fatalf("cards: %+v", p.Cards)
	}
	// Its open item is on the standing card already, so it isn't shown
	// twice.
	var texts []string
	for _, l := range p.Cards[0].Lines {
		texts = append(texts, l.Text)
	}
	want := []string{"Last met Mar 10: Glaze colours, the Harbor Labs kiln trial and the open studio date.", "Notebook: Quillstone: kiln notes (Mar 10)"}
	if !slices.Equal(texts, want) {
		t.Fatalf("lines %q", texts)
	}
	if src := p.Cards[0].Lines[1].Sources[0]; src != (Source{Label: "Mar 11", URL: "https://archive.example/notebook/n-1"}) {
		t.Errorf("the notebook line's source: %+v", src)
	}
	// Said again while its card shows, it brings no other.
	if hs := f.heardNames("Quillstone again", true, at.Add(10*time.Second)); len(hs) != 0 {
		t.Errorf("heard again: %+v", hs)
	}
	// The archive's owners get no card of their own.
	if hs := f.heardNames("Ben said so", true, at.Add(20*time.Second)); len(hs) != 0 {
		t.Errorf("an owner: %+v", hs)
	}
}

// quillBrief is what a made-up archive gathers about Quillstone.
func quillBrief() Brief {
	u := "https://archive.example/m/3?t="
	return Brief{Points: []Point{
		{Text: "Quillstone wants a $40 glaze kit before the April open studio", Under: "Glazes", Meeting: "Quillstone pottery review", Date: "2026-03-10", At: "4:00", URL: u + "240"},
		{Text: "Send the glaze chart", Under: "Next steps", Meeting: "Quillstone pottery review", Date: "2026-03-10", URL: u + "300", Standing: "done"},
		{Text: "The open studio moved to April 14", Under: "Dates", Meeting: "Quillstone pottery review", Date: "2026-03-10", At: "9:00", URL: u + "540"},
		{Text: "A private line", Meeting: "Quillstone pottery review", Date: "2026-03-10", URL: "https://elsewhere.example/x"},
		{Text: "Quillstone research: they fire 2,000 mugs a week", Meeting: "Quillstone: kiln notes", Date: "2026-03-11", URL: "https://archive.example/notebook/n-1", Notebook: true, Under: "notebook"},
	}}
}

func TestACardIsWrittenFromWhatTheArchiveGathersAndOnlyLinesThatCheckOutShow(t *testing.T) {
	f := topical(t)
	at := t0.Add(2 * time.Minute)
	f.heardNames("the quillstone deal", false, at)
	ask, n, ok := f.topicFetchDue(at)
	if !ok || ask != "Quillstone" {
		t.Fatalf("asked %q, %v", ask, ok)
	}
	if notes, taken := f.topicFetched(n, quillBrief(), nil, at.Add(2*time.Second)); !taken || notes == 0 {
		t.Fatalf("fetched: %d notes, %v", notes, taken)
	}
	c := f.tcard(n)
	for _, p := range c.from {
		if strings.Contains(p.URL, "elsewhere") {
			t.Errorf("a line the archive's own links don't open was given: %+v", p)
		}
		if p.Standing != "" {
			t.Errorf("an action item was given, which the card shows from the lists: %+v", p)
		}
	}
	p, wn, ok := f.topicWriteDue(at.Add(2 * time.Second))
	if !ok || wn != n || !strings.Contains(p.User, "Topic: Quillstone") || strings.Contains(p.User, "Send the glaze chart") {
		t.Fatalf("prompt %q", p.User)
	}
	k := func(prefix string) string {
		i := slices.IndexFunc(c.from, func(p Point) bool { return strings.HasPrefix(p.Text, prefix) })
		return fmt.Sprintf("[%d]", i+1)
	}
	text := "- Quillstone wants a $40 glaze kit " + k("Quillstone wants") + "\n" +
		"2. The open studio moved to April 14 " + k("The open studio") + "\n" +
		"They are happy [99]\n" +
		"No source here\n" +
		"Fires two thousand mugs " + k("Quillstone research") + " [see notes]\n"
	kept, left, why, taken := f.topicWritten(n, text, nil, at.Add(4*time.Second))
	if !taken || kept != 2 || left != 3 || why != "a line cited a note it wasn't given" {
		t.Fatalf("kept %d, left %d, because %q", kept, left, why)
	}
	card := f.panel(at.Add(4 * time.Second)).Cards[0]
	if card.Title != "Quillstone" || len(card.Lines) != 2 || card.Lines[0].Text != "Quillstone wants a $40 glaze kit" ||
		card.Lines[1].Text != "The open studio moved to April 14" || card.Lines[0].Sources[0] != (Source{Label: "Mar 10", URL: "https://archive.example/m/3?t=240"}) {
		t.Fatalf("card %+v", card)
	}
}

// A written line may word what its notes say, but not change a sum, a
// count or a date and cite them for it: a figure it gives in digits must
// be one that a note it cites gives, as an answer's must.
func TestACardsWrittenLineGivesOnlyFiguresTheNotesItCitesGive(t *testing.T) {
	f := topical(t)
	at := t0.Add(2 * time.Minute)
	f.heardNames("the quillstone deal", false, at)
	_, n, _ := f.topicFetchDue(at)
	f.topicFetched(n, quillBrief(), nil, at.Add(2*time.Second))
	f.topicWriteDue(at.Add(2 * time.Second))
	c := f.tcard(n)
	k := func(prefix string) int {
		return slices.IndexFunc(c.from, func(p Point) bool { return strings.HasPrefix(p.Text, prefix) }) + 1
	}
	text := fmt.Sprintf("Quillstone wants a $400 glaze kit [%d]\nThe open studio moved to April 15 [%d]\nThe open studio moved to April 14 [%d]\n"+
		"They fire two thousand mugs a week [%d]\nThe glaze kit costs $40 [%d]\n",
		k("Quillstone wants"), k("The open studio"), k("The open studio"), k("Quillstone research"), k("The open studio"))
	kept, left, why, taken := f.topicWritten(n, text, nil, at.Add(4*time.Second))
	if !taken || kept != 2 || left != 3 || why != "a line gave a figure its notes don't" {
		t.Fatalf("kept %d, left %d, because %q", kept, left, why)
	}
	var texts []string
	for _, l := range f.panel(at.Add(4 * time.Second)).Cards[0].Lines {
		texts = append(texts, l.Text)
	}
	if !slices.Equal(texts, []string{"The open studio moved to April 14", "They fire two thousand mugs a week"}) {
		t.Errorf("lines %q", texts)
	}
}

func TestWhatTheWriterWritesForACardIsReadStrictly(t *testing.T) {
	notes := []Point{{Text: "A kit is $40 a box"}, {Text: "The open studio is on April 14"}}
	for text, want := range map[string][]string{
		"Not in the archive":  nil,
		"Not in the archive.": nil,
		"The kit is $40 a box [1]\nThe open studio is April 14 [2].": {"The kit is $40 a box", "The open studio is April 14."},
		"• Owner: Ben, booking the kiln visit [1, 2]":                {"Owner: Ben, booking the kiln visit"},
		"Too short [1]":                          nil,
		strings.Repeat("long ", 30) + "[1]":      nil,
		"A line with [1] and [x] brackets":       nil,
		"It isn't not in the archive at all [1]": nil,
	} {
		lines, _, _ := readTopic(text, notes)
		var got []string
		for _, l := range lines {
			got = append(got, l.text)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%q: read %q, not %q", text, got, want)
		}
	}
	lines, _, _ := readTopic("Both notes say so [1, 2] [2]", notes)
	if len(lines) != 1 || !slices.Equal(lines[0].cited, []int{1, 2}) {
		t.Errorf("cited %+v", lines)
	}
}

func TestAFactShownLatelyIsNotShownAgain(t *testing.T) {
	f := topical(t)
	at := t0.Add(2 * time.Minute)
	f.heardNames("Harbor Labs", false, at)
	h := f.tcards[0]
	if h.title != "Harbor Labs" {
		t.Fatalf("card %q", h.title)
	}
	// Its item and meeting are on screen; a card about Priya made now
	// shows neither again.
	f.heardNames("and Priya", false, at.Add(time.Second))
	p := f.tcards[0]
	for _, l := range p.lines {
		if strings.Contains(l.Text, "Glaze colours") {
			t.Errorf("a fact shown on another card was shown again: %q", l.Text)
		}
	}
	// While its card is on the panel, said again after the quiet it
	// brings no other; once its card is gone, it may be shown again.
	if hs := f.heardNames("Priya Shah", false, at.Add(Defaults.TopicQuiet+time.Minute)); len(hs) != 0 {
		t.Errorf("a name whose card is on the panel: %+v", hs)
	}
	p.gone = true
	f.heardNames("Priya Shah", false, at.Add(Defaults.TopicQuiet+time.Minute))
	if p2 := f.tcards[0]; p2 == p || !slices.ContainsFunc(p2.lines, func(l CardLine) bool { return strings.Contains(l.Text, "Glaze colours") }) {
		t.Errorf("after a while: %+v", p2.lines)
	}
}

func TestANameThatComesBackWithSomethingElseSaidGetsACardAboutThat(t *testing.T) {
	f := topical(t)
	at := t0.Add(2 * time.Minute)
	f.heardNames("Harbor Labs", true, at)
	first := f.tcards[0]
	// Three other names push it off the panel.
	f.heardNames("Quillstone", true, at.Add(10*time.Second))
	f.heardNames("Tomas Weber", true, at.Add(20*time.Second))
	f.standing.shown = at.Add(30 * time.Second)
	later := at.Add(Defaults.TopicAgain + time.Second)
	if hs := f.heardNames("Harbor Labs, yes", true, later); len(hs) != 0 {
		t.Errorf("a name said again with nothing else: %+v", hs)
	}
	hs := f.heardNames("Harbor Labs wants the budget numbers by Friday", true, later)
	if len(hs) != 1 || !hs[0].again {
		t.Fatalf("heard %+v", hs)
	}
	again := f.tcards[0]
	if again == first || len(again.lines) != 0 || len(again.aspect) == 0 {
		t.Fatalf("the card about something new: %+v", again)
	}
	ask, _, ok := f.topicFetchDue(later)
	if !ok || !strings.HasPrefix(ask, "Harbor Labs ") || !strings.Contains(ask, "budget") {
		t.Errorf("asked %q", ask)
	}
}

func TestThePanelShowsThreeCardsNewestFirstAndAnAnswerCardAsTheAnswerPathHasIt(t *testing.T) {
	f := topical(t)
	at := t0.Add(2 * time.Minute)
	f.heardNames("Quillstone", false, at)
	f.heardNames("Tomas Weber", false, at.Add(time.Second))
	f.heardNames("Priya", false, at.Add(2*time.Second))
	// Harbor Labs's open item and meeting are on other cards already, so
	// its card waits for something new to show.
	f.heardNames("Harbor Labs", false, at.Add(3*time.Second))
	got := f.panel(at.Add(3 * time.Second)).Cards
	var names []string
	for _, c := range got {
		names = append(names, c.Title)
	}
	if !slices.Equal(names, []string{"Priya Shah", "Tomas Weber", "Quillstone"}) {
		t.Fatalf("cards %q", names)
	}
	// A question being asked shows as the answer path has it, its word
	// under it.
	q := question{shown: "How much is the glaze kit?", topic: "how much is the glaze kit", keys: []key{{stem: "glaze", weight: 1}, {stem: "kit", weight: 1}}}
	n, _, _ := f.ask(q, at.Add(4*time.Second))
	if c := f.panel(at.Add(4 * time.Second)).Cards[0]; c.Title != "How much is the glaze kit?" || c.Note != "Looking…" || len(c.Lines) != 0 {
		t.Fatalf("a question being asked shows: %+v", c)
	}
	cd := f.card(n)
	cd.done, cd.text, cd.chips = true, "The glaze kit is $40 [1]. The open studio is April 14 [2].", []Source{{Label: "Mar 10", URL: "https://archive.example/m/3?t=240"}, {Label: "Mar 10", URL: "https://archive.example/m/3?t=540"}}
	c := f.panel(at.Add(5 * time.Second)).Cards[0]
	if c.Title != "How much is the glaze kit?" || c.Note != "" || !c.Written || len(c.Lines) != 2 || c.Lines[0].Text != "The glaze kit is $40." || c.Lines[1].Sources[0].URL != "https://archive.example/m/3?t=540" {
		t.Fatalf("the answered question's card: %+v", c)
	}
}

// libraryArchive is the made-up archive with its lists, answering every
// question with the made-up brief, and noting what it was asked.
type libraryArchive struct{ fakeArchive }

func (a *libraryArchive) Library(ctx context.Context) (Library, error) { return madeUpLibrary(), nil }

func TestTheControllerShowsACardAboutANameAndLogsCountsOnly(t *testing.T) {
	screen := &logScreen{}
	archive := &libraryArchive{fakeArchive{brief: func(context.Context, string) (Brief, error) { return quillBrief(), nil },
		answer: func(context.Context, string) (Found, error) { return Found{}, nil }}}
	// The writer cites the note that says it: the open item before it
	// is on the standing card already.
	writer := &fakeWriter{write: func(_ context.Context, p Prompt) (string, error) {
		for _, l := range strings.Split(p.User, "\n") {
			if strings.Contains(p.User, "Topic: Quillstone") && strings.Contains(l, "Quillstone wants a $40") {
				return "Quillstone wants a $40 glaze kit before the open studio " + l[:strings.Index(l, "]")+1], nil
			}
		}
		return "Not in the archive", nil
	}}
	cfg := fast
	cfg.Topics, cfg.TopicQuiet, cfg.TopicAgain, cfg.TopicLines, cfg.TopicTokens = true, Defaults.TopicQuiet, Defaults.TopicAgain, Defaults.TopicLines, Defaults.TopicTokens
	cfg.FirstMinute, cfg.HearShort, cfg.HearLong, cfg.WriteTimeout = Defaults.FirstMinute, Defaults.HearShort, Defaults.HearLong, time.Second
	c := Start(cfg, archive, screen, time.Now, nil, map[string]Writer{"local": writer})
	c.Judging("local")
	c.Enabled(true)
	c.Capturing("", true)
	c.Recording(true)
	waitFor(t, "the standing card", func() bool { p := screen.last(); return len(p.Cards) > 0 && p.Cards[0].Title == "Open items" })
	c.Said(Said{Began: time.Now(), Request: 1, Partial: true, Segments: []Segment{{Text: "so"}, {Text: "the"}, {Text: "Quillstone"}, {Text: "numbers"}}})
	waitFor(t, "the card about the name", func() bool { p := screen.last(); return len(p.Cards) > 0 && p.Cards[0].Title == "Quillstone" })
	waitFor(t, "its written line", func() bool {
		p := screen.last()
		return len(p.Cards) > 0 && p.Cards[0].Title == "Quillstone" && len(p.Cards[0].Lines) > 0 && p.Cards[0].Lines[0].Text == "Quillstone wants a $40 glaze kit before the open studio"
	})
	screen.mu.Lock()
	lines := append([]string(nil), screen.lines...)
	screen.mu.Unlock()
	for _, l := range lines {
		for _, private := range []string{"Quillstone", "Harbor", "Priya", "Ana", "Ben", "glaze", "kiln"} {
			if strings.Contains(l, private) {
				t.Errorf("the log says %q: %s", private, l)
			}
		}
	}
	if !slices.ContainsFunc(lines, func(l string) bool {
		return strings.HasPrefix(l, "topic card ") && strings.Contains(l, "started: a project or company heard in a sentence still being said")
	}) {
		t.Errorf("the log doesn't say a card was started: %q", lines)
	}
}

func TestAQuestionIsAskedOfTheArchiveOnlyWhenItNamesWhatTheArchiveKnows(t *testing.T) {
	f := topical(t)
	for text, want := range map[string]bool{
		"What did Priya ask for in the kiln trial?": true,
		"How did the Quillstone call go?":           true,
		"Did the plumber fix the kitchen sink?":     false,
		"Did Ben like the movie last night?":        false,
		"Remember when we fired the kiln at forty?": true,
	} {
		q, ok := notice(utterance{text: text})
		if !ok {
			t.Fatalf("%q isn't noticed as a question", text)
		}
		if got := f.asksArchive(q, text); got != want {
			t.Errorf("%q: asked of the archive %v", text, got)
		}
	}
	// Knowing no names, it asks them all, as before.
	g := listening(Defaults)
	q, _ := notice(utterance{text: "Did the plumber fix the kitchen sink?"})
	if !g.asksArchive(q, "Did the plumber fix the kitchen sink?") {
		t.Error("a follow that knows no names left a question alone")
	}
}

// A question left to the people in the call asks something: it isn't a
// statement of fact, to check against the record, even when it holds a
// figure.
func TestAQuestionLeftToThePeopleIsntCheckedAsAStatement(t *testing.T) {
	screen := &logScreen{}
	archive := &libraryArchive{fakeArchive{brief: func(context.Context, string) (Brief, error) { return quillBrief(), nil },
		answer: func(context.Context, string) (Found, error) { return Found{}, nil }}}
	writer := &fakeWriter{write: func(_ context.Context, p Prompt) (string, error) {
		if p.System == checkSystem {
			return `{"differs": false}`, nil
		}
		return "Not in the archive", nil
	}}
	cfg := writingConfig(fast)
	cfg.Topics, cfg.TopicQuiet, cfg.TopicAgain, cfg.TopicLines, cfg.TopicTokens = true, Defaults.TopicQuiet, Defaults.TopicAgain, Defaults.TopicLines, Defaults.TopicTokens
	cfg.FirstMinute, cfg.HearShort, cfg.HearLong, cfg.WriteTimeout, cfg.CheckEvery = Defaults.FirstMinute, Defaults.HearShort, Defaults.HearLong, time.Second, 10*time.Millisecond
	c := Start(cfg, archive, screen, time.Now, nil, map[string]Writer{"local": writer})
	defer c.Enabled(false)
	c.Judging("local")
	c.Enabled(true)
	c.Capturing("", true)
	c.Recording(true)
	waitFor(t, "the standing card", func() bool { p := screen.last(); return len(p.Cards) > 0 && p.Cards[0].Title == "Open items" })
	say := func(words ...string) {
		s := Said{Began: time.Now(), Step: 100 * time.Millisecond, Mic: make([]float64, 40), Call: make([]float64, 40)}
		for i, w := range words {
			s.Segments = append(s.Segments, Segment{Text: w, At: time.Duration(i) * 300 * time.Millisecond, Length: 250 * time.Millisecond})
		}
		c.Said(s)
	}
	checks := func() []string {
		writer.mu.Lock()
		defer writer.mu.Unlock()
		var out []string
		for _, p := range writer.prompts {
			if p.System == checkSystem {
				out = append(out, p.User)
			}
		}
		return out
	}
	// The archive's lines about a name said are kept to check what is
	// said against.
	say("so", "Quillstone", "then.")
	waitFor(t, "the card about the name", func() bool { p := screen.last(); return len(p.Cards) > 0 && p.Cards[0].Title == "Quillstone" })
	say("did", "they", "order", "the", "$400", "glaze", "kit?")
	say("they", "ordered", "the", "$400", "glaze", "kit.")
	waitFor(t, "a check of the statement", func() bool {
		return slices.ContainsFunc(checks(), func(u string) bool { return strings.Contains(u, "they ordered the $400 glaze kit.") })
	})
	for _, u := range checks() {
		if strings.Contains(u, "did they order") {
			t.Fatalf("a question left to the people in the call was checked as a statement:\n%s", u)
		}
	}
}

func TestACardPushedOffThePanelDoesntComeBack(t *testing.T) {
	f := topical(t)
	f.panel(t0)
	at := t0.Add(2 * time.Minute)
	f.heardNames("Quillstone", false, at)
	f.heardNames("Tomas Weber", false, at.Add(time.Second))
	f.heardNames("Priya", false, at.Add(2*time.Second))
	f.panel(at.Add(3 * time.Second))
	q := question{shown: "How much is the glaze kit?", topic: "how much is the glaze kit", keys: []key{{stem: "glaze", weight: 1}, {stem: "kit", weight: 1}}}
	n, _, _ := f.ask(q, at.Add(4*time.Second))
	cd := f.card(n)
	cd.done, cd.text, cd.chips = true, "The glaze kit is $40 [1].", []Source{{Label: "Mar 10", URL: "https://archive.example/m/3?t=240"}}
	var names []string
	for _, c := range f.panel(at.Add(5 * time.Second)).Cards {
		names = append(names, c.Title)
	}
	if !slices.Equal(names, []string{"How much is the glaze kit?", "Priya Shah", "Tomas Weber"}) {
		t.Fatalf("cards %q", names)
	}
	// The question's card goes, as when the panel keeps three questions
	// and a fourth comes: Quillstone, pushed off, doesn't come back.
	cd.gone = true
	names = nil
	for _, c := range f.panel(at.Add(6 * time.Second)).Cards {
		names = append(names, c.Title)
	}
	if !slices.Equal(names, []string{"Priya Shah", "Tomas Weber"}) {
		t.Fatalf("cards after one went: %q", names)
	}
}

func TestADifferenceIsShownOnlyWhenBothAreAboutTheSameName(t *testing.T) {
	f := topical(t)
	quill := Point{Text: "A shared kiln was discussed, nothing agreed", Meeting: "Quillstone pottery review", Under: "Kilns"}
	if !f.sameNames("Quillstone offered us a shared kiln", quill) {
		t.Error("a statement about Quillstone against Quillstone's record")
	}
	if f.sameNames("Harbor Labs offered us a shared kiln", quill) {
		t.Error("a statement about Harbor Labs against Quillstone's record")
	}
	if !f.sameNames("Priya offered us a shared kiln", quill) {
		t.Error("a statement about Priya against a meeting she was in")
	}
	if f.sameNames("Tomas offered us a shared kiln", quill) {
		t.Error("a statement about Tomas against a meeting he wasn't in")
	}
	if !f.sameNames("they offered us a shared kiln", quill) {
		t.Error("a statement that names no one")
	}
}

// A meeting a person was in is the one of that title on that date: of a
// meeting held every week under one title, only the weeks they were in.
func TestAMeetingAPersonWasInIsKnownByItsDateAsWellAsItsTitle(t *testing.T) {
	lib := madeUpLibrary()
	again := Meeting{ID: "m-4", Title: "Quillstone pottery review", Date: "2026-03-24", URL: "https://archive.example/m/4", People: []string{"Ana Lima", "Ben Ortiz", "Tomas Weber"}}
	lib.Meetings = append([]Meeting{again}, lib.Meetings...)
	f := listening(Defaults)
	f.writes(true)
	f.library(lib, nil, t0)
	line := func(date string) Point {
		return Point{Text: "A shared kiln was discussed, nothing agreed", Meeting: "Quillstone pottery review", Date: date, Under: "Kilns"}
	}
	for _, c := range []struct {
		said, date string
		same       bool
	}{
		{"Priya offered us a shared kiln", "2026-03-10", true},
		{"Priya offered us a shared kiln", "2026-03-24", false},
		{"Tomas offered us a shared kiln", "2026-03-24", true},
		{"Tomas offered us a shared kiln", "2026-03-10", false},
	} {
		if got := f.sameNames(c.said, line(c.date)); got != c.same {
			t.Errorf("%q against the meeting of %s: %v", c.said, c.date, got)
		}
	}
}

func TestOnlySoManyCardsAboutNamesAreKeptButTheStandingCardIs(t *testing.T) {
	f := topical(t)
	for i := range 2 * keptTopics {
		f.startTopic(i%len(f.roster.all), nil, t0.Add(time.Duration(i)*time.Second))
	}
	if len(f.tcards) != keptTopics+1 || !slices.Contains(f.tcards, f.standing) {
		t.Errorf("kept %d cards, the standing card among them: %v", len(f.tcards), slices.Contains(f.tcards, f.standing))
	}
}

func TestANameHasOneCardOnThePanelItsNewest(t *testing.T) {
	f := topical(t)
	at := t0.Add(2 * time.Minute)
	f.heardNames("Quillstone", true, at)
	first := f.tcards[0]
	f.panel(at)
	later := at.Add(Defaults.TopicAgain + time.Second)
	f.heardNames("Quillstone wants the budget numbers", true, later)
	again := f.tcards[0]
	again.lines, again.shown = []CardLine{{Text: "The budget is due Friday", Sources: []Source{{Label: "Mar 10", URL: "https://archive.example/m/3?t=9"}}}}, later
	var titles []string
	for _, c := range f.panel(later).Cards {
		titles = append(titles, c.Title)
	}
	if slices.Index(titles, "Quillstone") != 0 || slices.Index(titles[1:], "Quillstone") >= 0 || !first.gone {
		t.Errorf("cards %q; the first card gone: %v", titles, first.gone)
	}
}

func TestAWrittenCardKeepsItsOpenItemsAsTheListsHoldThem(t *testing.T) {
	f := topical(t)
	at := t0.Add(2 * time.Minute)
	f.heardNames("and Priya", false, at)
	_, n, _ := f.topicFetchDue(at)
	f.topicFetched(n, quillBrief(), nil, at.Add(time.Second))
	f.topicWriteDue(at.Add(time.Second))
	c := f.tcard(n)
	i := slices.IndexFunc(c.from, func(p Point) bool { return strings.HasPrefix(p.Text, "The open studio") })
	f.topicWritten(n, fmt.Sprintf("Priya says the open studio moved to April 14 [%d]", i+1), nil, at.Add(3*time.Second))
	var texts []string
	for _, l := range f.panel(at.Add(3 * time.Second)).Cards[0].Lines {
		texts = append(texts, l.Text)
	}
	if !slices.Equal(texts, []string{"Open: Share the cost of the kiln trial (Priya)", "Priya says the open studio moved to April 14"}) {
		t.Errorf("lines %q", texts)
	}
}
