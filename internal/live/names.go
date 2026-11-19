package live

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// subject is a project, a person or a company the archive knows, which a card
// can be about: what its card is titled, how it may be said, and where the
// archive's lists hold it.
type subject struct {
	title  string
	person bool
	// ours says it is one of the archive's own owners, who are on the call
	// themselves: their open items are the standing card's, and they get
	// no card of their own.
	ours bool
	says []phrase
	// The meetings it was in or is named in, the open items that are its
	// or name it, and the notebook entries about it, by their place in
	// the library, newest first.
	meetings, items, notes []int
}

// phrase is one way a name may be said: its words, as plainly heard, and
// whether it is only taken as said exactly and with a capital, as a short
// name or one that is also a common word is.
type phrase struct {
	words  []string
	strict bool
}

// roster is what the archive knows by name: its library, and the names in
// it.
type roster struct {
	lib  Library
	all  []subject
	home string // the host the archive's own links are on
}

// Words that are no name, though written with a capital: what a meeting's
// title calls it, and names of months and days.
var (
	generic = set(`meeting meetings discussion discussions demo demos call calls sync syncs review reviews planning huddle follow-up followup follow up
introduction intro update updates notes note agenda kickoff standup weekly check-in checkin session sessions debrief recap workshop interview
group team us them everyone unassigned the`)
	calendar = set(`jan january feb february mar march apr april may jun june jul july aug august sep sept september oct october nov november dec december
mon monday tue tues tuesday wed wednesday thu thur thurs thursday fri friday sat saturday sun sunday`)
	// commonNames are first names that are also everyday words: one is
	// only taken with a capital.
	commonNames = set(`bill will mark grant rose may june april august frank art chase drew glen dawn summer rich chip pat sue jack bob hope joy faith sky`)
)

// learn reads the names in what the archive's lists hold: the people who
// were in its meetings, and those its board's items belong to; and the
// projects and companies its meetings' and notebook's titles, its
// summaries and its items write with a capital where a plain word would
// have none. A person named on most of the items on the owners' lane of
// the board is one of its owners.
func learn(lib Library) roster {
	n := roster{lib: lib, home: home(lib)}
	people := map[string]int{} // by full name, in lower case
	firsts := map[string][]int{}
	person := func(full string) int {
		full = strings.Join(strings.Fields(full), " ")
		key := strings.ToLower(full)
		if i, ok := people[key]; ok {
			return i
		}
		n.all = append(n.all, subject{title: full, person: true})
		people[key] = len(n.all) - 1
		first := plain(strings.Fields(full)[0])
		firsts[first] = append(firsts[first], len(n.all)-1)
		return len(n.all) - 1
	}
	for i, m := range lib.Meetings {
		for _, p := range m.People {
			if strings.TrimSpace(p) == "" {
				continue
			}
			k := person(p)
			n.all[k].meetings = append(n.all[k].meetings, i)
		}
	}
	// Who each item belongs to, as its owner says: "Priya", "Priya and
	// Tomas", "Tomas, with Priya".
	ours := map[int]int{}
	lane := 0
	for i, it := range lib.Items {
		if it.Ours {
			lane++
		}
		for _, part := range owners(it.Owner) {
			k, ok := -1, false
			if ks := firsts[plain(part)]; len(ks) == 1 {
				k, ok = ks[0], true
			} else if j, found := people[strings.ToLower(part)]; found {
				k, ok = j, true
			} else if len(ks) == 0 && !strings.Contains(part, " ") && !generic[plain(part)] && !stop[plain(part)] {
				k, ok = person(part), true
			}
			if !ok {
				continue
			}
			n.all[k].items = append(n.all[k].items, i)
			if it.Ours {
				ours[k]++
			}
		}
	}
	for k, c := range ours {
		n.all[k].ours = lane > 0 && 2*c >= lane
	}
	for k := range n.all {
		full := strings.Fields(n.all[k].title)
		n.all[k].says = append(n.all[k].says, phraseOf(full))
		if len(full) > 1 && len(firsts[plain(full[0])]) == 1 {
			n.all[k].says = append(n.all[k].says, phraseOf(full[:1]))
		}
	}
	// Projects and companies.
	var titles, prose []string
	for _, m := range lib.Meetings {
		titles = append(titles, m.Title)
		prose = append(prose, m.Summary)
	}
	for _, it := range lib.Items {
		prose = append(prose, it.Text)
	}
	for _, note := range lib.Notes {
		titles = append(titles, note.Title)
	}
	lower := map[string]bool{}
	for _, t := range append(append([]string(nil), titles...), prose...) {
		for _, w := range strings.FieldsFunc(t, notWord) {
			if r, _ := utf8.DecodeRuneInString(w); unicode.IsLower(r) {
				lower[plain(w)] = true
			}
		}
	}
	isPerson := func(w string) bool {
		_, full := people[plain(w)]
		return len(firsts[plain(w)]) > 0 || full
	}
	found := map[string]int{}
	add := func(run []string) {
		key := strings.ToLower(strings.Join(run, " "))
		if _, ok := found[key]; ok {
			return
		}
		n.all = append(n.all, subject{title: strings.Join(run, " ")})
		k := len(n.all) - 1
		found[key] = k
		n.all[k].says = append(n.all[k].says, phraseOf(run))
		// "Quillstone AI" is said as "Quillstone" too.
		if last := run[len(run)-1]; len(run) > 1 && acronym(last) {
			n.all[k].says = append(n.all[k].says, phraseOf(run[:len(run)-1]))
		}
	}
	for _, t := range titles {
		for _, r := range runs(t) {
			if r.name(lower, isPerson, true) {
				add(r.words)
			}
		}
	}
	for _, t := range prose {
		for _, r := range runs(t) {
			if r.name(lower, isPerson, false) {
				add(r.words)
			}
		}
	}
	// Where the lists hold each project or company: the meetings whose
	// title or summary says it, the items that say it, and the notebook
	// entries whose title or tags do.
	for k := range n.all {
		nm := &n.all[k]
		if nm.person {
			continue
		}
		for i, m := range lib.Meetings {
			if nm.in(m.Title) || nm.in(m.Summary) {
				nm.meetings = append(nm.meetings, i)
			}
		}
		for i, it := range lib.Items {
			if nm.in(it.Text) || nm.in(it.Owner) {
				nm.items = append(nm.items, i)
			}
		}
	}
	for k := range n.all {
		nm := &n.all[k]
		for i, note := range lib.Notes {
			tagged := slices.ContainsFunc(note.Tags, func(tag string) bool {
				return nm.in(strings.ReplaceAll(tag, "-", " ")) || joined(tag) == joined(nm.title)
			})
			if nm.in(note.Title) || tagged && !nm.person {
				nm.notes = append(nm.notes, i)
			}
		}
		if nm.person {
			// The items that name a person, as well as those that are
			// theirs.
			for i, it := range lib.Items {
				if !slices.Contains(nm.items, i) && nm.in(it.Text) {
					nm.items = append(nm.items, i)
				}
			}
			slices.Sort(nm.items)
		}
	}
	return n
}

// counts is what the roster holds, for the diagnostic log: in counts only.
func (n roster) counts() string {
	names, people, ours := 0, 0, 0
	for _, s := range n.all {
		switch {
		case s.ours:
			ours++
		case s.person:
			people++
		default:
			names++
		}
	}
	open := 0
	for _, it := range n.lib.Items {
		if it.Ours {
			open++
		}
	}
	return fmt.Sprintf("%d meetings, %d open items, %d of them the owners', %d notebook entries; %d projects and companies, %d people and %d owners known by name",
		len(n.lib.Meetings), len(n.lib.Items), open, len(n.lib.Notes), names, people, ours)
}

// owners are the people an item's owner names.
func owners(owner string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(owner, func(r rune) bool { return r == '/' || r == ',' || r == '&' || r == ';' }) {
		for _, p := range splitWords(part, " and ", " with ") {
			// "Priya to start" is Priya.
			ws := strings.Fields(p)
			var run []string
			for _, w := range ws {
				if r, _ := utf8.DecodeRuneInString(w); !unicode.IsUpper(r) {
					break
				}
				run = append(run, w)
			}
			if len(run) > 0 && len(run) <= 3 {
				out = append(out, strings.Join(run, " "))
			}
		}
	}
	return out
}

func splitWords(s string, seps ...string) []string {
	out := []string{s}
	for _, sep := range seps {
		var next []string
		for _, p := range out {
			next = append(next, strings.Split(p, sep)...)
		}
		out = next
	}
	return out
}

// run is a run of words written with a capital in a title or a sentence,
// and whether it starts the text or a part of it.
type run struct {
	words []string
	first bool
}

// runs are the runs of words with a capital in a text, each marked as
// starting the text or a part of it: what follows a mark that ends a
// sentence, a colon, a slash, a dash or an opening bracket.
func runs(text string) []run {
	var out []run
	var cur []string
	first, starts := true, true
	flush := func() {
		if len(cur) > 0 {
			out = append(out, run{words: cur, first: first})
		}
		cur = nil
	}
	isWord := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	for _, f := range strings.Fields(strings.NewReplacer("/", " / ", "—", " — ").Replace(text)) {
		lead := strings.IndexFunc(f, isWord)
		if lead < 0 {
			flush()
			starts = strings.ContainsAny(f, ".!?:;/(—–-")
			continue
		}
		last := strings.LastIndexFunc(f, isWord)
		_, size := utf8.DecodeRuneInString(f[last:])
		before, w, after := f[:lead], f[lead:last+size], f[last+size:]
		if before != "" {
			flush()
			starts = starts || strings.ContainsAny(before, "(")
		}
		w = strings.TrimSuffix(strings.TrimSuffix(w, "'s"), "’s")
		if r, _ := utf8.DecodeRuneInString(w); unicode.IsUpper(r) && !stop[plain(w)] && !generic[plain(w)] && !calendar[plain(w)] {
			if len(cur) == 0 {
				first = starts
			}
			cur = append(cur, w)
		} else {
			flush()
		}
		starts = false
		if after != "" && !strings.HasPrefix(after, "'") && !strings.HasPrefix(after, "’") {
			flush()
			starts = strings.ContainsAny(after, ".!?:;(")
		}
	}
	flush()
	return out
}

// name says whether a run is a project's or a company's name: not a
// person's, nor an acronym alone; and written with a capital inside a
// word, as "BrightLeaf" is, or where a plain word would have none, in the
// middle of a sentence, or, in a title, with none of its words ever
// written without one.
func (r run) name(lower map[string]bool, isPerson func(string) bool, title bool) bool {
	words := slices.DeleteFunc(append([]string(nil), r.words...), func(w string) bool { return isPerson(w) })
	if len(words) != len(r.words) || len(words) == 0 {
		return false
	}
	if len(words) == 1 && (acronym(words[0]) || utf8.RuneCountInString(words[0]) < 3) {
		return false
	}
	for _, w := range words {
		if camel(w) {
			return true
		}
	}
	if !r.first {
		return true
	}
	if !title {
		return false
	}
	for _, w := range words {
		if !acronym(w) && lower[plain(w)] {
			return false
		}
	}
	return true
}

// acronym says whether a word is written mostly in capitals, as "AI",
// "APIs" and "PhD" are: two capitals or more, and two small letters at
// most.
func acronym(w string) bool {
	upper, lower := 0, 0
	for _, r := range w {
		switch {
		case unicode.IsUpper(r):
			upper++
		case unicode.IsLower(r):
			lower++
		}
	}
	return upper >= 2 && lower <= 2
}

// camel says whether a word has a capital inside it, after two small
// letters at least, as a made-up name such as "BrightLeaf" does.
func camel(w string) bool {
	rs := []rune(w)
	if len(rs) < 6 {
		return false
	}
	small := 0
	for _, r := range rs[1:] {
		switch {
		case unicode.IsLower(r):
			small++
		case unicode.IsUpper(r) && small >= 2:
			return true
		}
	}
	return false
}

// phraseOf is how a run of written words is said: its words as plainly
// heard, taken only exactly and with a capital when it is one short word,
// or a name that is also an everyday word.
func phraseOf(words []string) phrase {
	var s phrase
	for _, w := range words {
		if p := plain(w); p != "" {
			s.words = append(s.words, p)
		}
	}
	one := strings.Join(s.words, "")
	s.strict = len(s.words) == 1 && (utf8.RuneCountInString(one) <= 4 || commonNames[one] || stop[one])
	return s
}

// in says whether a text says the name, as written.
func (nm subject) in(text string) bool {
	words := heardWords(text)
	// In what the archive wrote, a capital is a name's wherever it is.
	for i, f := range spokenFields(text) {
		if i < len(words) {
			words[i].capital = words[i].capital || f
		}
	}
	for _, s := range nm.says {
		if s.strict {
			continue
		}
		for i := 0; i+len(s.words) <= len(words); i++ {
			ok := true
			for j, w := range s.words {
				ok = ok && words[i+j].plain == w
			}
			if ok {
				return true
			}
		}
	}
	// A short name is said with a capital.
	for _, s := range nm.says {
		if s.strict && len(s.words) == 1 {
			for _, w := range words {
				if w.plain == s.words[0] && w.capital {
					return true
				}
			}
		}
	}
	return false
}

// heardWord is a word as heard: in plain letters, and whether it was
// written with a capital where a sentence doesn't start, as a name is.
type heardWord struct {
	plain   string
	capital bool
}

// spokenFields says, of each word of a text, whether it is written with a
// capital, wherever it is.
func spokenFields(text string) []bool {
	var out []bool
	for _, f := range fields(text) {
		if plain(f) == "" {
			continue
		}
		w := strings.TrimLeftFunc(f, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		r, _ := utf8.DecodeRuneInString(w)
		out = append(out, unicode.IsUpper(r))
	}
	return out
}

func fields(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) || r == '/' || r == '-' || r == '—' })
}

func heardWords(text string) []heardWord {
	var out []heardWord
	starts := true
	for _, f := range fields(text) {
		p := plain(f)
		ends := strings.ContainsAny(f[len(f)-1:], ".?!")
		if p == "" {
			starts = starts || ends
			continue
		}
		w := strings.TrimLeftFunc(f, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		r, _ := utf8.DecodeRuneInString(w)
		out = append(out, heardWord{plain: p, capital: unicode.IsUpper(r) && !starts})
		starts = ends
	}
	return out
}

// plain is a word in lower case, without what marks it as owned, and
// without anything but its letters and digits.
func plain(w string) string {
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

func notWord(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\'' && r != '’'
}

// joined is a text's words run together, in plain letters.
func joined(text string) string {
	var b strings.Builder
	for _, w := range heardWords(text) {
		b.WriteString(w.plain)
	}
	return b.String()
}

// sound is a word as it sounds, roughly: letters that sound alike made
// one, and a letter said twice said once, so that "Pria" sounds as
// "Priya" does, and "Quilstone" as "Quillstone".
func sound(w string) string {
	r := strings.NewReplacer("ph", "f", "ck", "k", "qu", "kw", "q", "k", "x", "ks", "oo", "u", "ou", "u", "ee", "i", "ie", "i", "y", "i", "z", "s").Replace(w)
	var b strings.Builder
	var last rune
	for i, c := range r {
		if c == 'c' {
			// A c before e or i sounds as an s, and else as a k.
			if i+1 < len(r) && strings.ContainsRune("ei", rune(r[i+1])) {
				c = 's'
			} else {
				c = 'k'
			}
		}
		if c != last {
			b.WriteRune(c)
		}
		last = c
	}
	return b.String()
}

// mentions are the names said in a run of words, by their place in
// n.all, once each, in the order said. A name is heard when its words
// come together, as written, run together or split, as "Bright Leaf";
// for a long enough name, as it sounds; written with a capital where no
// sentence starts, as a name is, misheard by a letter, or by two in a
// long name; and, for a name of eight letters or more, as two words with
// its first four and last three letters and a few wrong between. A
// sweep of a dictionary's words through an archive's names showed that
// everyday words are often a letter or two from a name, as "millstone"
// is from a made-up "Quillstone".
func (n roster) mentions(text string, cfg Config) []int {
	words := heardWords(text)
	var out []int
	for k, nm := range n.all {
		if nm.heardIn(words, cfg) {
			out = append(out, k)
		}
	}
	return out
}

func (nm subject) heardIn(words []heardWord, cfg Config) bool {
	for _, s := range nm.says {
		want := strings.Join(s.words, "")
		// A long name may be heard split in three, as "quill a stone".
		most := len(s.words) + 1
		if utf8.RuneCountInString(want) >= cfg.HearLong {
			most++
		}
		for size := max(len(s.words)-1, 1); size <= most; size++ {
			for i := 0; i+size <= len(words); i++ {
				var b strings.Builder
				common := true
				for _, w := range words[i : i+size] {
					b.WriteString(w.plain)
					common = common && (stop[w.plain] || generic[w.plain])
				}
				got := b.String()
				if common || got == "" {
					continue
				}
				if s.strict {
					// A short name is taken as written, or with a
					// capital as it sounds, as "Anna" for "Ana"; one
					// that is also an everyday word only with a capital.
					if size == 1 && !stop[got] && (got == want && (words[i].capital || !commonNames[want]) || words[i].capital && sound(got) == sound(want)) {
						return true
					}
					continue
				}
				capital := true
				for _, w := range words[i : i+size] {
					capital = capital && w.capital
				}
				if heardAs(got, want, size, len(s.words), capital, cfg) {
					return true
				}
			}
		}
	}
	return false
}

// heardAs says whether what was heard, size words run together, is a
// name of words words, by the tolerances in cfg: as written; as it
// sounds, in as many words; with a letter or two wrong only when written
// with capitals, in as many words; and a long name as two words with its
// ends as written, as "quill stone" or "quills tone" for "Quillstone".
func heardAs(got, want string, size, words int, capital bool, cfg Config) bool {
	if got == want {
		return true
	}
	n := utf8.RuneCountInString(want)
	g, w := sound(got), sound(want)
	if size >= words && g == w {
		return true
	}
	off := apart([]rune(g), []rune(w))
	switch {
	case !capital || size < words:
	case n >= cfg.HearLong && off <= 2:
		return true
	case n >= cfg.HearShort && off <= 1:
		return true
	}
	if n < cfg.HearLong || size != 2 {
		return false
	}
	m := utf8.RuneCountInString(got)
	return m >= n-1 && m <= n+3 && strings.HasPrefix(got, string([]rune(want)[:4])) && strings.HasSuffix(got, string([]rune(want)[n-3:]))
}

// home is the host most of the archive's own links are on, which a
// card's sources must be on too.
func home(lib Library) string {
	count := map[string]int{}
	best := ""
	for _, m := range lib.Meetings {
		if u, err := url.Parse(m.URL); err == nil && u.Host != "" {
			count[u.Host]++
			if count[u.Host] > count[best] {
				best = u.Host
			}
		}
	}
	return best
}

// ours says whether a link is the archive's own, on its host, which a
// browser can open.
func (n roster) ours(link string) bool {
	u, err := url.Parse(link)
	return err == nil && openable(link) && n.home != "" && u.Host == n.home
}

// Named is a name the archive knows, as a replay scores a meeting by:
// what its card is titled, whether it is a person, and one of the
// archive's owners, and how it may be said, each in plain words.
type Named struct {
	Title        string
	Person, Ours bool
	Says         [][]string
}

// Names are the names the archive's lists hold, as the live side learns
// them.
func Names(lib Library) []Named {
	var out []Named
	for _, s := range learn(lib).all {
		n := Named{Title: s.title, Person: s.person, Ours: s.ours}
		for _, p := range s.says {
			n.Says = append(n.Says, p.words)
		}
		out = append(out, n)
	}
	return out
}
