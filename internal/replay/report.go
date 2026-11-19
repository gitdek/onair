package replay

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gitdek/onair/internal/live"
)

// Played is a run of a replay, as kept: its name and what it showed.
type Played struct {
	Name  string
	Shown []Shown
}

// Mention is a name the archive knows, said in the meeting, and when, by
// the transcript itself: not by what recognition heard, nor by what the
// live side made of it.
type Mention struct {
	Title string
	At    time.Duration
}

// Mentions are the names said in a meeting, the first time each is said,
// in order. A name is said when its words come together in the
// transcript, run together or split, or with a letter wrong in a name of
// six letters or more, or two in one of nine or more. The archive's own
// owners, who are on the call, aren't counted.
func Mentions(words []Word, names []live.Named) []Mention {
	plainWords := make([]string, len(words))
	for i, w := range words {
		plainWords[i] = plainWord(w.Text)
	}
	var out []Mention
	for _, n := range names {
		if n.Ours {
			continue
		}
		at, found := time.Duration(0), false
		for _, say := range n.Says {
			want := strings.Join(say, "")
			for size := max(len(say)-1, 1); size <= len(say)+1; size++ {
				for i := 0; i+size <= len(words); i++ {
					got := strings.Join(plainWords[i:i+size], "")
					if got == "" || !near(got, want) {
						continue
					}
					if !found || words[i].From < at {
						at, found = words[i].From, true
					}
					break
				}
			}
		}
		if found {
			out = append(out, Mention{Title: n.Title, At: at})
		}
	}
	slices.SortStableFunc(out, func(a, b Mention) int { return int(a.At - b.At) })
	return out
}

func plainWord(w string) string {
	w = strings.ToLower(strings.ReplaceAll(w, "’", "'"))
	w = strings.TrimSuffix(strings.TrimRightFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), "'s")
	var b strings.Builder
	for _, r := range w {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// near says whether a word heard is a name, by letters alone.
func near(got, want string) bool {
	if got == want {
		return true
	}
	n := utf8.RuneCountInString(want)
	if n < 6 {
		return false
	}
	most := 1
	if n >= 9 {
		most = 2
	}
	return distance([]rune(got), []rune(want)) <= most
}

func distance(a, b []rune) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			change := prev[j-1]
			if a[i-1] != b[j-1] {
				change++
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, change)
		}
		prev = cur
	}
	return prev[len(b)]
}

// shown is a card as a report follows it: what it is titled and says.
type shown struct {
	title string
	lines []string
	facts bool // a line has a source
	model bool // a line is an answer or a fact the local model wrote and On Air checked
}

// cards is what a panel showed, as cards: what it draws, when the run
// drew cards; or else its answer cards and links, as the panel drew them
// before.
func cards(p *live.Panel, drawn bool) []shown {
	var out []shown
	if p == nil || !p.Visible {
		return nil
	}
	if drawn {
		for _, c := range p.Cards {
			s := shown{title: c.Title, model: c.Written}
			for _, l := range c.Lines {
				var labels []string
				for _, src := range l.Sources {
					labels = append(labels, src.Label)
				}
				text := l.Text
				if len(labels) > 0 {
					text += "  (" + strings.Join(labels, ", ") + ")"
					s.facts = true
				}
				s.lines = append(s.lines, text)
			}
			if c.Note != "" {
				s.lines = append(s.lines, c.Note)
			}
			out = append(out, s)
		}
		return out
	}
	for _, a := range p.Answers {
		s := shown{title: "Question: " + a.Question}
		if a.Text != "" {
			s.lines = append(s.lines, a.Text)
			s.facts, s.model = true, true
		}
		for _, l := range a.Lines {
			s.lines = append(s.lines, l.Text+"  ("+l.Where+")")
			s.facts = true
		}
		if a.Note != "" {
			s.lines = append(s.lines, a.Note)
		}
		out = append(out, s)
	}
	for _, l := range p.Links {
		out = append(out, shown{title: "Link: " + l.Kind.String() + " · " + l.Date + " · " + l.Title, lines: []string{l.Why}, facts: true})
	}
	return out
}

// Measure is how a run did against the meeting's mentions.
type Measure struct {
	Name string
	// Cards is how many cards appeared, and PerTen how many in ten
	// minutes.
	Cards  int
	PerTen float64
	// WithFacts is the share of cards that showed a line with a source,
	// and Written the share that showed an answer or a fact the local
	// model wrote and On Air checked.
	WithFacts, Written float64
	// Found is the names whose card came after they were first said,
	// and how long after; Missed those whose card never came. Linked is
	// the same for a card or a link to a related meeting, whichever came
	// first.
	Found  []Found
	Missed []string
	Linked []Found
	// Links is how many links to related meetings appeared.
	Links int
}

// Found is a name's card, and how long after the name was first said it
// came.
type Found struct {
	Title string
	After time.Duration
}

// Measures how a run did: the cards it showed, and how soon after each
// name the archive knows was first said a card about it came.
func Measures(run Played, start time.Time, length time.Duration, mentions []Mention) Measure {
	m := Measure{Name: run.Name}
	drawn := slices.ContainsFunc(run.Shown, func(s Shown) bool { return s.Panel != nil && len(s.Panel.Cards) > 0 })
	type state struct {
		facts, model bool
	}
	seen := map[string]*state{}
	var order []string
	first := map[string]time.Duration{}  // a name's first card about it, by when
	linked := map[string]time.Duration{} // and its first card or link
	var prev map[string]bool
	for _, s := range run.Shown {
		if s.Panel == nil {
			continue
		}
		now := map[string]bool{}
		for _, c := range cards(s.Panel, drawn) {
			key := c.title
			now[key] = true
			at := s.At.Sub(start)
			if strings.HasPrefix(c.title, "Link: ") {
				if !prev[key] {
					m.Links++
				}
				for _, mention := range mentions {
					if _, ok := linked[mention.Title]; !ok && at >= mention.At && about(c, mention.Title) {
						linked[mention.Title] = at
					}
				}
				continue
			}
			if !prev[key] {
				order = append(order, key)
				seen[key+fmt.Sprint(len(order))] = &state{}
			}
			st := seen[key+fmt.Sprint(lastIndex(order, key)+1)]
			st.facts = st.facts || c.facts
			st.model = st.model || c.model
			for _, mention := range mentions {
				if at < mention.At || !about(c, mention.Title) {
					continue
				}
				if _, ok := first[mention.Title]; !ok {
					first[mention.Title] = at
				}
				if _, ok := linked[mention.Title]; !ok {
					linked[mention.Title] = at
				}
			}
		}
		prev = now
	}
	m.Cards = len(order)
	if length > 0 {
		m.PerTen = float64(m.Cards) / length.Minutes() * 10
	}
	facts, model := 0, 0
	for i, key := range order {
		st := seen[key+fmt.Sprint(i+1)]
		if st.facts {
			facts++
		}
		if st.model {
			model++
		}
	}
	if m.Cards > 0 {
		m.WithFacts, m.Written = float64(facts)/float64(m.Cards), float64(model)/float64(m.Cards)
	}
	for _, mention := range mentions {
		if at, ok := first[mention.Title]; ok {
			m.Found = append(m.Found, Found{Title: mention.Title, After: at - mention.At})
		} else {
			m.Missed = append(m.Missed, mention.Title)
		}
		if at, ok := linked[mention.Title]; ok {
			m.Linked = append(m.Linked, Found{Title: mention.Title, After: at - mention.At})
		}
	}
	return m
}

func lastIndex(order []string, key string) int {
	for i := len(order) - 1; i >= 0; i-- {
		if order[i] == key {
			return i
		}
	}
	return -1
}

func firstLine(c shown) string {
	if len(c.lines) == 0 {
		return ""
	}
	return c.lines[0]
}

// about says whether a card is about a name: titled with it, or saying
// it in its title or first line.
func about(c shown, title string) bool {
	if strings.EqualFold(c.title, title) {
		return true
	}
	text := strings.ToLower(c.title + " " + firstLine(c))
	for _, w := range strings.Fields(strings.ToLower(title)) {
		if len(w) >= 4 && strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// Report writes a run's, or two runs', timeline as a person reads it: for
// each moment the panel's cards changed, when, by the meeting's clock,
// what was said just before, in a few words, and what each run's panel
// showed; and first, how each run measured up.
func Report(w io.Writer, title string, words []Word, start time.Time, runs []Played, mentions []Mention) error {
	length := time.Duration(0)
	if len(words) > 0 {
		length = words[len(words)-1].To
	}
	p := func(format string, args ...any) { fmt.Fprintf(w, format, args...) }
	p("%s\n%s long, %d words, %d names the archive knows said in it.\n\n", title, clock(length), len(words), len(mentions))
	p("How each run did\n")
	for _, r := range runs {
		m := Measures(r, start, length, mentions)
		p("\n  %s: %d cards, %.1f every ten minutes; %.0f%% showed a line from the archive with its source, %.0f%% an answer or a fact the local model wrote and On Air checked",
			m.Name, m.Cards, m.PerTen, 100*m.WithFacts, 100*m.Written)
		if m.Links > 0 {
			p("; and %d links to related meetings", m.Links)
		}
		p(".\n")
		if len(m.Found) > 0 {
			var afters []time.Duration
			for _, f := range m.Found {
				afters = append(afters, f.After)
			}
			slices.Sort(afters)
			p("  A card about a name came after it was first said for %d of %d names: %s after it at the median, %s to %s.\n",
				len(m.Found), len(mentions), secs(afters[len(afters)/2]), secs(afters[0]), secs(afters[len(afters)-1]))
			for _, f := range m.Found {
				p("    %-28s %s\n", f.Title, secs(f.After))
			}
		}
		if len(m.Missed) > 0 {
			p("  No card about: %s.\n", strings.Join(m.Missed, ", "))
		}
		if m.Links > 0 && len(m.Linked) > 0 {
			var afters []time.Duration
			for _, f := range m.Linked {
				afters = append(afters, f.After)
			}
			slices.Sort(afters)
			p("  Counting links too, something about a name came for %d of %d names: %s after it at the median, %s to %s.\n",
				len(m.Linked), len(mentions), secs(afters[len(afters)/2]), secs(afters[0]), secs(afters[len(afters)-1]))
			for _, f := range m.Linked {
				p("    %-28s %s\n", f.Title, secs(f.After))
			}
		}
	}
	p("\nNames said, the first time, by the transcript\n")
	for _, m := range mentions {
		p("  %s  %s\n", clock(m.At), m.Title)
	}
	p("\nTimeline\n")
	type event struct {
		at   time.Duration
		run  string
		text []string
	}
	var events []event
	for _, r := range runs {
		drawn := slices.ContainsFunc(r.Shown, func(s Shown) bool { return s.Panel != nil && len(s.Panel.Cards) > 0 })
		before := map[string]string{}
		var differs string
		for _, s := range r.Shown {
			if s.Panel == nil {
				continue
			}
			at := s.At.Sub(start)
			now := map[string]string{}
			var lines []string
			for _, c := range cards(s.Panel, drawn) {
				body := strings.Join(c.lines, "\n")
				now[c.title] = body
				old, was := before[c.title]
				switch {
				case !was:
					lines = append(lines, "+ "+c.title)
				case old != body:
					lines = append(lines, "~ "+c.title)
				default:
					continue
				}
				for _, l := range c.lines {
					lines = append(lines, "    · "+l)
				}
			}
			for t := range before {
				if _, ok := now[t]; !ok && s.Panel.Visible {
					lines = append(lines, "- "+t)
				}
			}
			if d := s.Panel.Differs.Text; d != "" && d != differs {
				lines = append(lines, "! "+d+"  ("+s.Panel.Differs.Where+")")
			}
			differs = s.Panel.Differs.Text
			for _, c := range s.Panel.Calls {
				lines = append(lines, "* "+c)
			}
			if len(lines) > 0 {
				events = append(events, event{at: at, run: r.Name, text: lines})
			}
			before = now
		}
	}
	slices.SortStableFunc(events, func(a, b event) int { return int(a.at - b.at) })
	calls := map[string]bool{}
	for _, e := range events {
		var kept []string
		for _, l := range e.text {
			if strings.HasPrefix(l, "* ") {
				if calls[e.run+l] {
					continue
				}
				calls[e.run+l] = true
			}
			kept = append(kept, l)
		}
		if len(kept) == 0 {
			continue
		}
		p("\n%s  %s\n", clock(e.at), said(words, e.at))
		for _, l := range kept {
			p("       %-7s %s\n", e.run, l)
		}
	}
	return nil
}

// said is what was said in the few seconds before a moment, in a dozen
// words at most.
func said(words []Word, at time.Duration) string {
	var got []string
	for _, w := range words {
		if w.To <= at && w.To > at-8*time.Second {
			got = append(got, w.Text)
		}
	}
	if len(got) > 12 {
		got = got[len(got)-12:]
	}
	if len(got) == 0 {
		return "(quiet)"
	}
	return "said: “…" + strings.Join(got, " ") + "”"
}

func clock(d time.Duration) string {
	d = max(d, 0).Round(time.Second)
	return fmt.Sprintf("%02d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func secs(d time.Duration) string {
	return fmt.Sprintf("%.1f s", d.Seconds())
}
