package live

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Cards about what the archive knows: when a project, a person or a
// company it knows by name comes up in the call, a card about it shows at
// once with what the archive's lists hold, and then with a few short lines
// the local model writes from what On the Record gathers about it, each
// checked as an answer is. A standing card shows the open items of the
// archive's owners, who are on the call, and of the people named in its
// first minute.

// tcard is a card about a name the archive knows, or the standing card of
// open items.
type tcard struct {
	n       int
	subject int // its name's place in the roster, or -1 for the open items
	title   string
	// aspect is what else was said with its name when it came back, which
	// a card with something new is about.
	aspect []key
	heard  time.Time // when its name was heard
	shown  time.Time // when it went on top of the panel, or zero while it has nothing to show
	lines  []CardLine
	facts  [][]string // the facts each line shows, by what they come from
	// written says its lines are the local model's, not the lists'.
	written bool
	// gone says newer cards pushed it off the panel: it doesn't come back.
	gone bool
	// Asking On the Record, and then the writer, about it.
	fetch      bool // waits to be asked about
	asked      time.Time
	fetched    time.Time
	from       []Point  // the notes the writer is given, numbered from 1
	fromFacts  []string // and what each comes from
	write      bool     // waits for the writer
	writeAsked time.Time
	done       bool
}

// seenFact is when a fact was last shown, and on which card.
type seenFact struct {
	at   time.Time
	card int
}

// hearing is what came of a name heard, for the diagnostic log.
type hearing struct {
	card    int
	person  bool
	started bool // a card was made for it
	again   bool // it came back with something new
	lines   int  // the lists' lines it showed at once
}

// library takes the archive's lists, and learns the names in them. The
// standing card shows the open items of the archive's owners.
func (f *follow) library(lib Library, err error, now time.Time) {
	f.libAsking = false
	if err != nil {
		return
	}
	r := learn(lib)
	f.roster = &r
	f.standingCard(now)
}

// libraryDue says whether to ask the archive for its lists now: once while
// listening with topics on, and again a minute after a failure.
func (f *follow) libraryDue(now time.Time) bool {
	if !f.listening() || !f.cfg.Topics || f.roster != nil || f.libAsking {
		return false
	}
	if !f.libAsked.IsZero() && now.Sub(f.libAsked) < time.Minute {
		return false
	}
	f.libAsking, f.libAsked = true, now
	return true
}

// standingCard puts the open items of the archive's owners on the panel,
// and of the people named since listening began, newest first.
func (f *follow) standingCard(now time.Time) {
	if f.roster == nil {
		return
	}
	r := f.roster
	var picked []int
	for i, it := range r.lib.Items {
		if it.Ours {
			picked = append(picked, i)
		}
	}
	for _, k := range f.firstNamed {
		for _, i := range r.all[k].items {
			if !slices.Contains(picked, i) {
				picked = append(picked, i)
			}
		}
	}
	slices.SortStableFunc(picked, func(a, b int) int { return cmp.Compare(r.lib.Items[b].MeetingDate, r.lib.Items[a].MeetingDate) })
	var lines []CardLine
	var facts [][]string
	for _, i := range picked {
		it := r.lib.Items[i]
		if !r.ours(it.URL) || len(lines) == f.cfg.TopicLines {
			continue
		}
		text := line(firstSentence(it.Text), 72)
		if who := r.owner(it); who != "" {
			text = who + ": " + text
		}
		lines = append(lines, CardLine{Text: text, Sources: []Source{{Label: short(it.MeetingDate, "board"), URL: it.URL}}})
		facts = append(facts, []string{"item " + it.ID})
	}
	if len(lines) == 0 {
		return
	}
	c := f.standing
	if c == nil {
		f.tcardsMade++
		c = &tcard{n: f.tcardsMade, subject: -1, title: "Open items", done: true}
		f.standing = c
		f.tcards = append([]*tcard{c}, f.tcards...)
	}
	if len(lines) > len(c.lines) {
		c.shown = now
	}
	c.lines, c.facts = lines, facts
	f.saw(c, now)
}

// owner is whose an item is, as a card says it: the people the archive
// knows that its owner names before any comma, such as "Ana/Ben", or
// nothing when it names none, as "Group" or "the team" don't.
func (n roster) owner(it Item) string {
	var who []string
	for _, part := range owners(strings.SplitN(it.Owner, ",", 2)[0]) {
		for _, s := range n.all {
			if s.person && s.in(part) && !slices.Contains(who, part) {
				who = append(who, part)
				break
			}
		}
	}
	return strings.Join(who, "/")
}

// firstSentence is a text up to the end of its first sentence.
func firstSentence(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if i := strings.Index(text, ". "); i > 0 {
		return text[:i+1]
	}
	return text
}

// short is a date such as "2026-11-09" as a card's source says it, "Nov
// 9", or else what to say instead.
func short(date, otherwise string) string {
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(firstOf(date, 10))); err == nil {
		return t.Format("Jan 2")
	}
	return otherwise
}

func firstOf(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// heardNames takes what was said, a finished sentence or the one being
// said, and the names the archive knows in it. A name heard for the first
// time, or again once its card has been gone a while, gets a card at
// once; one that comes back, in a finished sentence, with something else
// said about it gets a card about that, if the archive has something new
// to say. People named in the first minute add their open items to the
// standing card. It says what came of each.
func (f *follow) heardNames(text string, finished bool, now time.Time) []hearing {
	if f.roster == nil || !f.listening() || !f.cfg.Topics {
		return nil
	}
	var out []hearing
	for _, k := range f.roster.mentions(text, f.cfg) {
		s := f.roster.all[k]
		if s.person && !f.listenedAt.IsZero() && now.Sub(f.listenedAt) < f.cfg.FirstMinute && !slices.Contains(f.firstNamed, k) {
			f.firstNamed = append(f.firstNamed, k)
			f.standingCard(now)
		}
		if s.ours {
			continue
		}
		c := f.topicCard(k)
		switch {
		case c == nil || now.Sub(c.heard) >= f.cfg.TopicQuiet && !f.onPanel(c, now):
			out = append(out, f.startTopic(k, nil, now))
		case finished && now.Sub(c.heard) >= f.cfg.TopicAgain:
			// Back again, it gets a card only for something else said
			// about it, which shows only what wasn't shown lately.
			aspect := aspectOf(text, s, f.cfg)
			if len(aspect) == 0 {
				continue
			}
			h := f.startTopic(k, aspect, now)
			h.again = true
			out = append(out, h)
		}
	}
	return out
}

// aspectOf is the key words of what was said with a name, but the name's
// own.
func aspectOf(text string, s subject, cfg Config) []key {
	var own []string
	for _, p := range s.says {
		own = append(own, p.words...)
	}
	return slices.DeleteFunc(keysOf(strings.Fields(text), 0), func(k key) bool {
		return slices.ContainsFunc(own, func(w string) bool { return alike(stem(w), k.stem) || heardAs(plain(k.word), w, 1, 1, true, cfg) })
	})
}

// topicCard is the newest card about a name, if any.
func (f *follow) topicCard(k int) *tcard {
	for _, c := range f.tcards {
		if c.subject == k {
			return c
		}
	}
	return nil
}

// startTopic makes a card about a name: with the lines the archive's lists
// hold about it, shown at once, unless it is about something new; and
// then, with a writer, the lines it writes from what On the Record
// gathers about it.
func (f *follow) startTopic(k int, aspect []key, now time.Time) hearing {
	s := f.roster.all[k]
	f.tcardsMade++
	c := &tcard{n: f.tcardsMade, subject: k, title: s.title, aspect: aspect, heard: now}
	if len(aspect) == 0 {
		c.lines, c.facts = f.listLines(s, now)
	}
	if len(c.lines) > 0 {
		c.shown = now
		f.saw(c, now)
	}
	c.fetch = f.writer
	c.done = !c.fetch
	f.tcards = append([]*tcard{c}, f.tcards...)
	f.trimTopics()
	f.topicsStarted++
	return hearing{card: c.n, person: s.person, started: true, lines: len(c.lines)}
}

// keptTopics is how many cards about names are kept, newest first: those
// on the panel, and those a name said again is told apart by.
const keptTopics = 24

// trimTopics forgets the oldest cards, but for the standing card.
func (f *follow) trimTopics() {
	if len(f.tcards) <= keptTopics {
		return
	}
	kept := f.tcards[:keptTopics]
	if f.standing != nil && !slices.Contains(kept, f.standing) {
		kept = append(kept, f.standing)
	}
	f.tcards = kept
}

// listLines are what the archive's lists hold about a name, in a few
// lines: its open items, newest first; the newest meeting it was in or
// named in, with the archive's line on it; and the newest notebook entry
// about it. Facts shown on another card lately are left out.
func (f *follow) listLines(s subject, now time.Time) ([]CardLine, [][]string) {
	r := f.roster
	var lines []CardLine
	var facts [][]string
	add := func(text string, src Source, fact string) {
		if len(lines) >= 3 || f.shownLately(fact, 0, now) || !r.ours(src.URL) {
			return
		}
		lines = append(lines, CardLine{Text: text, Sources: []Source{src}})
		facts = append(facts, []string{fact})
	}
	items := append([]int(nil), s.items...)
	slices.SortStableFunc(items, func(a, b int) int { return cmp.Compare(r.lib.Items[b].MeetingDate, r.lib.Items[a].MeetingDate) })
	for _, i := range items[:min(2, len(items))] {
		it := r.lib.Items[i]
		text := "Open: " + line(firstSentence(it.Text), 66)
		if who := r.owner(it); who != "" {
			text += " (" + who + ")"
		}
		add(text, Source{Label: short(it.MeetingDate, "board"), URL: it.URL}, "item "+it.ID)
	}
	if len(s.meetings) > 0 {
		m := r.lib.Meetings[s.meetings[0]]
		about := m.Summary
		if about == "" {
			about = m.Title
		}
		add(line("Last met "+short(m.Date, m.Date)+": "+firstSentence(about), 90), Source{Label: short(m.Date, "meeting"), URL: m.URL}, "meeting "+m.ID)
	}
	if len(s.notes) > 0 {
		n := r.lib.Notes[s.notes[0]]
		add(line("Notebook: "+n.Title, 90), Source{Label: short(n.Updated, "notebook"), URL: n.URL}, "note "+n.ID)
	}
	return lines, facts
}

// saw notes the facts a card shows as shown now, and forgets those shown
// longer ago than the quiet.
func (f *follow) saw(c *tcard, now time.Time) {
	if f.seen == nil {
		f.seen = map[string]seenFact{}
	}
	for fact, s := range f.seen {
		if now.Sub(s.at) >= f.cfg.TopicQuiet {
			delete(f.seen, fact)
		}
	}
	for _, fs := range c.facts {
		for _, fact := range fs {
			f.seen[fact] = seenFact{at: now, card: c.n}
		}
	}
}

// shownLately says whether a fact was shown on another card than card
// within f.cfg.TopicQuiet.
func (f *follow) shownLately(fact string, card int, now time.Time) bool {
	s, ok := f.seen[fact]
	return ok && s.card != card && now.Sub(s.at) < f.cfg.TopicQuiet
}

// onPanel says whether a card is among those the panel shows now.
func (f *follow) onPanel(c *tcard, now time.Time) bool {
	for _, d := range f.deck(now) {
		if d.topic == c {
			return true
		}
	}
	return false
}

// topicFetchDue says which card's name to ask On the Record about now: the
// newest waiting, one at a time, while listening with a writer, and what
// to ask: the name, and what else was said about it for a card about
// something new. One it returns is under way: topicFetched or
// abandonTopicFetch ends it.
func (f *follow) topicFetchDue(now time.Time) (string, int, bool) {
	if !f.listening() || !f.writer || f.fetching != 0 {
		return "", 0, false
	}
	for _, c := range f.tcards {
		if !c.fetch {
			continue
		}
		c.fetch, c.asked = false, now
		f.fetching, f.fetchAsked = c.n, now
		ask := c.title
		for _, k := range c.aspect {
			ask += " " + k.word
		}
		return ask, c.n, true
	}
	return "", 0, false
}

// topicFetched takes what On the Record gathered about a card's name: the
// notes the writer is to write the card from.
func (f *follow) topicFetched(n int, b Brief, err error, now time.Time) (notes int, taken bool) {
	if n == 0 || n != f.fetching {
		return 0, false
	}
	f.fetching = 0
	c := f.tcard(n)
	if c == nil {
		return 0, true
	}
	c.fetched = now
	if err != nil {
		c.done = true
		return 0, true
	}
	// What is said next is checked against it too.
	f.remember(b, now)
	c.from, c.fromFacts = f.topicNotes(c, b)
	if len(c.from) == 0 {
		c.done = true
		return 0, true
	}
	c.write = true
	return len(c.from), true
}

// topicNotes are the notes a card about a name is written from, best
// first, to about f.cfg.TopicTokens: the lines of the brief, those that
// say its name first, and a figure, a date or a decision, and a meeting
// of the last two weeks, counting more; of those as good, the newest.
// Action items aren't given: the card shows its open items as the lists
// hold them, with whose they are, which the model was seen to get wrong.
// Only lines the archive's own links open are given, each once.
func (f *follow) topicNotes(c *tcard, b Brief) ([]Point, []string) {
	r := f.roster
	s := r.all[c.subject]
	type scored struct {
		p     Point
		fact  string
		score float64
	}
	var all []scored
	seen := map[string]bool{}
	add := func(p Point, fact string, score float64) {
		k := strings.ToLower(strings.Join(strings.Fields(p.Text), " "))
		if k == "" || seen[k] || !r.ours(p.URL) {
			return
		}
		seen[k] = true
		all = append(all, scored{p, fact, score})
	}
	newest := ""
	for _, p := range b.Points {
		newest = max(newest, p.Date)
	}
	q := question{keys: append(keysOf(strings.Fields(c.title), 1), c.aspect...)}
	for _, p := range b.Points {
		if p.Said || p.Standing != "" {
			continue
		}
		score := 0.0
		if s.in(p.Text) {
			score += 2
		} else if s.in(p.Under + " " + p.Meeting) {
			score += 1
		}
		score += q.rank(p) / 2
		// What a person in the meeting wants first: a figure, a date or a
		// decision, rather than who someone is, and what was said lately.
		if factual(p.Text) {
			score += 2
		}
		if recent(p.Date, newest) {
			score += 0.5
		}
		add(p, p.URL+"\x00"+p.Text, score)
	}
	slices.SortStableFunc(all, func(a, b scored) int {
		if a.score != b.score {
			return cmp.Compare(b.score, a.score)
		}
		return cmp.Compare(b.p.Date, a.p.Date)
	})
	var from []Point
	var facts []string
	used := 0
	for _, s := range all {
		cost := tokens(numbered(len(from)+1, s.p))
		if used+cost > f.cfg.TopicTokens {
			continue
		}
		used += cost
		from = append(from, s.p)
		facts = append(facts, s.fact)
	}
	return from, facts
}

// topicSystem is what the writer is told about a card about a name. Its
// example lines are made up, and a line that copies one is left out.
const topicSystem = "You brief someone in a meeting about a topic, from numbered notes of earlier meetings. Use only those notes.\n" +
	"Write 3 to 5 lines. Each line is one short fact of at most 12 words, followed by the number of the note it comes from in square brackets.\n" +
	"Choose what matters in a conversation now: amounts, numbers and dates; what was decided or agreed; what is still open and who owns it; the next step. " +
	"Skip background, biographies and descriptions of products or demos, and never write about what the notes don't say. " +
	"Keep who owns or did each thing exactly as the notes say, and write what the notes say happened or was agreed: a suggestion, plan or idea is no decision. " +
	"A note marked notebook is research or advice written after the meetings: what it suggests didn't happen.\n" +
	"Write in exactly this form, and nothing else:\n" + topicExample + "\n" +
	"Only if no note is about the topic, write exactly: Not in the archive"

// topicExample shows the writer the form of a card's lines, with no fact
// in it to copy.
const topicExample = "first fact [2]\nsecond fact [5]"

// factual says whether a line holds a figure, a date or a decision.
func factual(text string) bool {
	for _, w := range strings.Fields(text) {
		b := bare(w)
		if strings.ContainsAny(b, "0123456789") || numberWords[b] || dateWords[b] || decisionWords[b] || moneyWords[b] {
			return true
		}
	}
	return false
}

// recent says whether a date is within two weeks of the newest.
func recent(date, newest string) bool {
	d, err1 := time.Parse("2006-01-02", firstOf(date, 10))
	n, err2 := time.Parse("2006-01-02", firstOf(newest, 10))
	return err1 == nil && err2 == nil && n.Sub(d) <= 14*24*time.Hour
}

// topicPrompt asks the writer for a card about a name.
func topicPrompt(c *tcard) Prompt {
	var b strings.Builder
	b.WriteString("Notes:\n")
	for i, p := range c.from {
		b.WriteString(numbered(i+1, p) + "\n")
	}
	b.WriteString("\nTopic: " + c.title)
	if len(c.aspect) > 0 {
		var words []string
		for _, k := range c.aspect {
			words = append(words, k.word)
		}
		b.WriteString(", and what the notes say about " + strings.Join(words, ", "))
	}
	return Prompt{System: topicSystem, User: b.String(), Most: 160}
}

// topicWriteDue says which card to have the writer write now: the newest
// waiting, while listening, and never while it rewords a question,
// writes an answer or checks what was said, nor while a round is out or
// a question's answer waits to be written: a question asked comes first.
func (f *follow) topicWriteDue(now time.Time) (Prompt, int, bool) {
	if !f.listening() || !f.writer || f.topicWriting != 0 || f.writing != 0 || f.checking != 0 || f.inRound != 0 || f.rewording != 0 {
		return Prompt{}, 0, false
	}
	if slices.ContainsFunc(f.cards, func(c *card) bool { return c.write }) {
		return Prompt{}, 0, false
	}
	for _, c := range f.tcards {
		if c.write {
			c.write, c.writeAsked = false, now
			f.topicWriting, f.topicAsked = c.n, now
			return topicPrompt(c), c.n, true
		}
	}
	return Prompt{}, 0, false
}

// topicWritten takes what the writer wrote for a card, and shows the lines
// of it that check out and say what wasn't shown on another card lately.
// It says how many it kept, how many of what it wrote it left out, and
// why, in On Air's own words.
func (f *follow) topicWritten(n int, text string, err error, now time.Time) (kept, left int, why string, taken bool) {
	if n == 0 || n != f.topicWriting {
		return 0, 0, "", false
	}
	f.topicWriting = 0
	c := f.tcard(n)
	if c == nil {
		return 0, 0, "", true
	}
	c.done = true
	if err != nil {
		if errors.Is(err, ErrLoading) {
			f.slow = true
		}
		return 0, 0, brief(err), true
	}
	f.topicsWritten++
	written, left, why := readTopic(text, c.from)
	var lines []CardLine
	var facts [][]string
	for _, w := range written {
		var srcs []Source
		var fs []string
		fresh := false
		for _, k := range w.cited {
			p := c.from[k-1]
			fs = append(fs, c.fromFacts[k-1])
			if !f.shownLately(c.fromFacts[k-1], c.n, now) {
				fresh = true
			}
			if len(srcs) < 2 && !slices.ContainsFunc(srcs, func(s Source) bool { return s.URL == p.URL }) {
				srcs = append(srcs, Source{Label: label(p), URL: p.URL})
			}
		}
		if !fresh {
			left++
			continue
		}
		if len(lines) == f.cfg.TopicLines {
			left++
			continue
		}
		lines = append(lines, CardLine{Text: w.text, Sources: srcs})
		facts = append(facts, fs)
	}
	if len(lines) == 0 {
		if len(c.lines) == 0 {
			c.shown = time.Time{}
		}
		return 0, left, why, true
	}
	if c.shown.IsZero() {
		c.shown = now
	}
	// The card's open items stay, as the lists hold them, before what
	// was written, to f.cfg.TopicLines in all.
	var items []CardLine
	var itemFacts [][]string
	for i, fs := range c.facts {
		if len(fs) == 1 && strings.HasPrefix(fs[0], "item ") && len(items) < 2 {
			items, itemFacts = append(items, c.lines[i]), append(itemFacts, fs)
		}
	}
	room := max(f.cfg.TopicLines-len(items), 0)
	c.lines = append(items, lines[:min(len(lines), room)]...)
	c.facts = append(itemFacts, facts[:min(len(facts), room)]...)
	c.written = true
	f.saw(c, now)
	return len(lines), left, why, true
}

// label is what a source's chip says: the date of the line's meeting or
// entry, or what it is.
func label(p Point) string {
	if p.Notebook {
		return short(p.Date, "notebook")
	}
	return short(p.Date, "source")
}

// abandonTopicFetch gives up on On the Record's answer for a card when it
// is late by a second.
func (f *follow) abandonTopicFetch(now time.Time) (n int) {
	if f.fetching == 0 || now.Sub(f.fetchAsked) <= f.cfg.Timeout+time.Second {
		return 0
	}
	n, f.fetching = f.fetching, 0
	if c := f.tcard(n); c != nil {
		c.done = true
	}
	return n
}

// abandonTopicWrite gives up on the writer for a card when it is late by a
// second.
func (f *follow) abandonTopicWrite(now time.Time) (n int) {
	if f.topicWriting == 0 || now.Sub(f.topicAsked) <= f.cfg.WriteTimeout+time.Second {
		return 0
	}
	n, f.topicWriting = f.topicWriting, 0
	if c := f.tcard(n); c != nil {
		c.done = true
	}
	return n
}

func (f *follow) tcard(n int) *tcard {
	for _, c := range f.tcards {
		if c.n == n {
			return c
		}
	}
	return nil
}

// topicLine is a line the writer wrote for a card, and the notes it cites.
type topicLine struct {
	text  string
	cited []int
}

// bullet is what may start a line the writer wrote: a dash, a dot or a
// number, which isn't kept.
var bullet = regexp.MustCompile(`^\s*(?:[-*•–]|\d{1,2}[.)])\s+`)

// readTopic checks what the writer wrote for a card, given the notes it
// was given, numbered from 1. Each line it keeps says a fact in a
// sentence of three words or more and 120 characters at most, and cites,
// in square brackets after it, only notes 1 to n and nothing else, which
// aren't shown in the line; and, as an answer must, it gives in digits
// only figures that a note it cites gives. A line that doesn't is left
// out, and why the first was says why, in On Air's own words. "Not in the
// archive" alone gives no line.
func readTopic(text string, from []Point) (lines []topicLine, left int, why string) {
	n := len(from)
	t := strings.TrimSpace(strings.ReplaceAll(text, "**", ""))
	if strings.EqualFold(strings.TrimRight(t, ".! "), "Not in the archive") {
		return nil, 0, ""
	}
	leave := func(reason string) {
		left++
		if why == "" {
			why = reason
		}
	}
	for _, l := range strings.Split(t, "\n") {
		l = strings.TrimSpace(bullet.ReplaceAllString(l, ""))
		if l == "" {
			continue
		}
		if strings.Contains(strings.ToLower(l), "not in the archive") {
			leave("it said it had nothing")
			continue
		}
		found := citation.FindAllStringSubmatch(l, -1)
		if len(found) == 0 {
			leave("a line cited nothing")
			continue
		}
		rest := strings.TrimSpace(citation.ReplaceAllString(l, ""))
		if strings.ContainsAny(rest, "[]") {
			leave("a line put something other than numbers in brackets")
			continue
		}
		var cited []int
		bad := false
		for _, m := range found {
			for _, s := range strings.Split(m[1], ",") {
				k, _ := strconv.Atoi(strings.TrimSpace(s))
				if k < 1 || k > n {
					bad = true
				} else if !slices.Contains(cited, k) {
					cited = append(cited, k)
				}
			}
		}
		if bad {
			leave("a line cited a note it wasn't given")
			continue
		}
		rest = strings.Join(strings.Fields(strings.ReplaceAll(rest, " .", ".")), " ")
		rest = strings.TrimRight(strings.TrimSuffix(rest, " ."), " ,;:")
		if strings.Contains(topicExample, rest) {
			leave("a line copied the example")
			continue
		}
		if len(strings.Fields(rest)) < 3 {
			leave("a line was too short to say anything")
			continue
		}
		if utf8.RuneCountInString(rest) > 120 {
			leave("a line was too long")
			continue
		}
		var sources []Point
		for _, k := range cited {
			sources = append(sources, from[k-1])
		}
		if !givenBy(rest, sources) {
			leave("a line gave a figure its notes don't")
			continue
		}
		lines = append(lines, topicLine{text: rest, cited: cited})
	}
	if len(lines) == 0 && why == "" && t != "" {
		why = "it wrote no line"
	}
	return lines, left, why
}

// topicLog is a card's steps for the diagnostic log, in counts and times
// only.
func (f *follow) topicLog(c *tcard, now time.Time) string {
	parts := []string{fmt.Sprintf("its name heard %s ago", after(now, c.heard))}
	if !c.asked.IsZero() && !c.fetched.IsZero() {
		parts = append(parts, "On the Record answered in "+after(c.fetched, c.asked))
	}
	if !c.writeAsked.IsZero() {
		parts = append(parts, "written in "+after(now, c.writeAsked))
	}
	return fmt.Sprintf("topic card %d: %s", c.n, strings.Join(parts, ", "))
}
