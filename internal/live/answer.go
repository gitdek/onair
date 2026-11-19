package live

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

// question is a question asked in the call, or a topic brought up, that On
// the Record is asked about.
type question struct {
	shown string // as its card shows it, as heard
	topic string // as On the Record is asked
	keys  []key
	want  want
	// recall says it asks about the last time something happened, so the
	// newest meeting's lines come first.
	recall bool
	who    Speaker
	how    ending    // how the sentence it was heard in ended
	said   time.Time // when its last word was said
	// extra are the writer's other words for what it asks about, which
	// the lines the writer answers from are ranked by too.
	extra []key
}

// key is a key word of a question: its stem, and what it counts for, two
// for a name or a number and one for any other word.
type key struct {
	stem   string
	weight float64
	word   string // as written, for the writer's words
}

// want is the kind of answer a question wants, which a line that has it
// is more likely to give.
type want int

const (
	anything want = iota
	amount        // how much: a sum or a price
	count         // how many
	moment        // when: a date, a day or a time
	person        // who
)

// notice says whether a sentence asks something worth asking On the
// Record about, and what. It does when it ends with a question mark; when
// it starts, after any filler such as "so" or "um", with a question word,
// or with a word such as "did" or "is" followed by who or what it asks
// about, as in "did they" or "is the"; when it brings up a topic, as "let's
// talk about" does; and when it asks about the last time something
// happened, as "the last time we spoke with" does. A question or a recall
// needs key words that count two at least, which one name or number does,
// or two other words, so that "how are you" or "can you see my screen" is
// left alone; a topic needs one.
func notice(s utterance) (question, bool) {
	all := strings.Fields(s.text)
	start := 0
	for start < len(all) && fillers[bare(all[start])] {
		start++
	}
	words := all[start:]
	if len(words) == 0 {
		return question{}, false
	}
	plain := make([]string, len(words))
	for i, w := range words {
		plain[i] = bare(w)
	}
	q := question{who: s.who, how: s.ended, said: s.to, recall: holds(plain, recalls)}
	asks := strings.HasSuffix(s.text, "?") || questionWords[plain[0]] ||
		len(plain) > 1 && auxiliaries[plain[0]] && (subjects[plain[1]] || named(words[1]))
	shown, topic, least, from := words, words, 2.0, 0
	if !asks {
		if i, n := find(plain, topics); i >= 0 {
			shown, topic, least, from = words[i:], words[i+n:], 1, i+n
		} else if !q.recall {
			return question{}, false
		}
	}
	q.keys = keysOf(topic, from)
	if weight(q.keys) < least {
		return question{}, false
	}
	q.want = wants(plain)
	q.shown = strings.Join(shown, " ")
	if r := []rune(q.shown); len(r) > 0 {
		q.shown = string(unicode.ToUpper(r[0])) + string(r[1:])
	}
	if asks && !strings.HasSuffix(q.shown, "?") {
		q.shown = strings.TrimRight(q.shown, ".,;:!…") + "?"
	}
	q.shown = line(q.shown, 140)
	q.topic = strings.Join(topic[:min(len(topic), 40)], " ")
	return q, true
}

// Words that start or bring up a question.
var (
	fillers       = set("so and but okay ok um umm uh uhh er hmm yeah yes well hey oh alright right now also actually anyway like then just")
	questionWords = set("who whom whose what what's when where which why how how's")
	auxiliaries   = set("did does do is are was were can could should would will has have")
	subjects      = set("i you he she it we they there this that these those the a an our your his her their my its any anyone anybody someone somebody everyone everybody")
	// topics bring a topic up: what follows is the topic.
	topics = [][]string{
		{"let's", "talk", "about"}, {"lets", "talk", "about"}, {"let", "us", "talk", "about"},
		{"let's", "discuss"}, {"lets", "discuss"}, {"let's", "go", "over"}, {"let's", "look", "at"},
		{"let's", "move", "on", "to"}, {"moving", "on", "to"}, {"move", "on", "to"},
		{"next", "topic", "is"}, {"tell", "me", "about"}, {"remind", "me", "about"}, {"speaking", "of"},
	}
	// recalls ask about the last time something happened.
	recalls = [][]string{
		{"last", "time", "we"}, {"last", "time", "you"}, {"last", "time", "they"},
		{"last", "we", "spoke"}, {"last", "we", "talked"}, {"last", "we", "met"},
		{"remember", "when"}, {"do", "you", "remember"}, {"most", "recent"}, {"last", "meeting"}, {"last", "call"},
	}
)

// stop are words too common to tell one line from another.
var stop = set(`a about above after again against all also am an and any are aren't as at be because been before being below between
both but by can can't cannot could couldn't did didn't do does doesn't doing don't down during each few for from further had hadn't
has hasn't have haven't having he he'd he'll he's her here here's hers herself him himself his how how's i i'd i'll i'm i've if in
into is isn't it it's its itself let let's lets me more most my myself no nor not of off on once only or other our ours ourselves out
over own same she she'd she'll she's should shouldn't so some such than that that's the their theirs them themselves then there there's
these they they'd they'll they're they've this those through to too under until up very was wasn't we we'd we'll we're we've were weren't
what what's when where which while who whom whose why will with won't would wouldn't you you'd you'll you're you've your yours yourself
yeah yes okay ok um uh hmm oh well just really actually like anyway right alright sure maybe
get got getting go going gone went want wanted wants need needed say said says tell told talk talked talking speak spoke spoken think
thought know knew mean meant see saw hear heard ask asked remember remind make made take took come came give gave look looked
thing things something anything everything stuff way lot lots kind sort bit time times last first next new old good great much many
one ones guys guy people someone anyone everyone again still even ever never always already now today yesterday tomorrow`)

// kin are words that name the same thing in a meeting's notes, by stem.
var kin = func() map[string]string {
	groups := map[string]string{
		"launch":  "launch launched release released ship shipped shipping live golive rollout",
		"money":   "money cash fund funding fundraise fundraising raise raised invest investment investor capital seed valuation",
		"price":   "price pricing cost costs fee fees charge budget",
		"date":    "date dates deadline due schedule scheduled timeline",
		"screen":  "screen screens page pages view mockup mockups",
		"demo":    "demo demoed presentation walkthrough prototype",
		"feature": "feature features functionality capability capabilities improvement improvements enhancement enhancements roadmap",
		"client":  "customer customers client clients user users",
	}
	out := map[string]string{}
	for group, words := range groups {
		for _, w := range strings.Fields(words) {
			out[stem(w)] = group
		}
	}
	return out
}()

// keysOf is the key words of a question: those not too common, by stem,
// once each, a name or a number counting two. Words that are kin count
// once, as one. from is where the words start in their sentence, whose
// first word has a capital whether or not it is a name.
func keysOf(words []string, from int) []key {
	var out []key
	seen := map[string]int{}
	for i, w := range words {
		b := bare(w)
		if b == "" || stop[b] {
			continue
		}
		s := stem(b)
		strong := strings.ContainsAny(b, "0123456789") || i+from > 0 && named(w) || len(b) > 1 && strings.ToUpper(w) == w && strings.ToLower(w) != w
		if len(s) < 3 && !strong {
			continue
		}
		weight := 1.0
		if strong {
			weight = 2
		}
		id := s
		if g := kin[s]; g != "" {
			id = "~" + g
		}
		if j, ok := seen[id]; ok {
			out[j].weight = max(out[j].weight, weight)
			continue
		}
		seen[id] = len(out)
		out = append(out, key{stem: s, weight: weight})
	}
	return out
}

func weight(keys []key) float64 {
	total := 0.0
	for _, k := range keys {
		total += k.weight
	}
	return total
}

// wants is the kind of answer a question wants, by its words.
func wants(words []string) want {
	text := " " + strings.Join(words, " ") + " "
	has := func(group string) bool {
		return slices.ContainsFunc(words, func(w string) bool { return kin[stem(w)] == group })
	}
	switch {
	case strings.Contains(text, " how many ") || strings.Contains(text, " number of "):
		return count
	case strings.Contains(text, " how much ") || has("money") || has("price"):
		return amount
	case words[0] == "when" || has("date") || strings.Contains(text, " what time ") || strings.Contains(text, " what day ") || strings.Contains(text, " how soon "):
		return moment
	case words[0] == "who" || words[0] == "whose" || words[0] == "whom":
		return person
	}
	return anything
}

// What a line that gives each kind of answer has.
var (
	sum  = regexp.MustCompile(`[$€£]\s?\d|\d\s?(k|m|bn)\b|\d\s?%|\b(million|billion|thousand|hundred|percent|dollars?|euros?|pounds?)\b`)
	many = regexp.MustCompile(`\d|\b(two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|fifteen|twenty|thirty|dozen|hundred)\b`)
	day  = regexp.MustCompile(`\b(jan|january|feb|february|apr|april|june?|july?|aug|august|sept?|september|oct|october|nov|november|dec|december|monday|tuesday|wednesday|thursday|friday|saturday|sunday|tomorrow|tonight|q[1-4]|20\d\d|\d+(st|nd|rd|th)|\d{1,2}/\d{1,2}|\d{1,2}(:\d\d)?\s?(am|pm))\b|\b(march|may)\s+\d|\d\s+(of\s+)?(march|may)\b|\b(next|this|end of the|end of) (week|month|quarter|year)\b`)
	name = regexp.MustCompile(`\s\p{Lu}\p{Ll}`)
)

// gives says whether a line has the kind of answer a question wants.
func gives(w want, text string) bool {
	lower := strings.ToLower(text)
	switch w {
	case amount:
		return sum.MatchString(lower)
	case count:
		return many.MatchString(lower)
	case moment:
		return day.MatchString(lower)
	case person:
		return name.MatchString(text)
	}
	return false
}

// answers says whether a key word is what a kind of answer gives, as a
// sum answers "money" and a date answers "deadline".
func answers(w want, s string) bool {
	switch kin[s] {
	case "money", "price":
		return w == amount
	case "date":
		return w == moment
	}
	return false
}

// relevance is how well a line of a brief answers a question: each key
// word it has counts whole, and each that only what it sits under has, its
// topic, its section or its meeting, counts half; having the kind of
// answer the question wants counts one and a half. It is 0 when the line
// itself has no key word, unless it has the kind of answer and what it
// sits under has a key word.
func (q question) relevance(p Point) float64 {
	own, under := stems(p.Text), stems(p.Under+" "+p.Meeting)
	gave := q.want != anything && gives(q.want, p.Text)
	var mine, near float64
	for _, k := range q.keys {
		switch {
		case like(own, k.stem) || gave && answers(q.want, k.stem):
			mine += k.weight
		case like(under, k.stem):
			near += k.weight / 2
		}
	}
	if mine == 0 && !(gave && near > 0) {
		return 0
	}
	if gave {
		return mine + near + 1.5
	}
	return mine + near
}

// rank is how well a line of a brief seems to answer a question, for
// choosing the lines the writer reads: each key word, or word the writer
// gave for the same thing, that the line has counts whole, and each that
// only what it sits under has counts half, so that the lines under a
// heading that matches rank above those that share nothing; having the
// kind of answer the question wants counts one and a half.
func (q question) rank(p Point) float64 {
	own, under := stems(p.Text), stems(p.Under+" "+p.Meeting)
	gave := q.want != anything && gives(q.want, p.Text)
	r := 0.0
	for _, k := range append(append([]key(nil), q.keys...), q.extra...) {
		switch {
		case like(own, k.stem) || gave && answers(q.want, k.stem):
			r += k.weight
		case like(under, k.stem):
			r += k.weight / 2
		}
	}
	if gave {
		r += 1.5
	}
	return r
}

// Enough is the relevance a line needs to be shown as an answer: two key
// words of its own, say, or a name, or one key word and the kind of answer
// the question wants. Below it, the card says the archive has no answer.
const Enough = 2.0

// answerLines picks, from a brief, the lines that answer a question best,
// at most n: those with Enough relevance and at least six tenths of the
// best's, ranked by it, a line with a moment to open coming before one
// without, for a question about the last time the newest meeting's first,
// and of two as good, the newer meeting's. The same line is shown once,
// and one a browser can't open is left out.
func answerLines(q question, b Brief, n int) []AnswerLine {
	type scored struct {
		p         Point
		relevance float64
		rank      float64
	}
	var all []scored
	best, newest := 0.0, ""
	for _, p := range b.Points {
		if strings.TrimSpace(p.Text) == "" || !openable(p.URL) {
			continue
		}
		if r := q.relevance(p); r >= Enough {
			all = append(all, scored{p: p, relevance: r, rank: r})
			best, newest = max(best, r), max(newest, p.Date)
		}
	}
	for i := range all {
		if all[i].p.At != "" {
			all[i].rank += 0.5
		}
		if q.recall && all[i].p.Date == newest {
			all[i].rank++
		}
	}
	slices.SortStableFunc(all, func(a, b scored) int {
		switch {
		case a.rank != b.rank:
			return cmp.Compare(b.rank, a.rank)
		default:
			// Of two as good, the newer meeting's: what was said last is
			// likelier to stand.
			return cmp.Compare(b.p.Date, a.p.Date)
		}
	})
	var out []AnswerLine
	seen := map[string]bool{}
	for _, s := range all {
		text := line(s.p.Text, 110)
		if len(out) == n || s.relevance < 0.6*best || seen[strings.ToLower(text)] {
			continue
		}
		seen[strings.ToLower(text)] = true
		if s.p.Said {
			text = quote(text)
		}
		var where []string
		for _, w := range []string{line(s.p.Meeting, 60), s.p.Date, s.p.At} {
			if w != "" {
				where = append(where, w)
			}
		}
		out = append(out, AnswerLine{Text: text, Where: strings.Join(where, " · "), URL: s.p.URL})
	}
	return out
}

// lately is a question asked lately, by its key words, and when.
type lately struct {
	keys []key
	at   time.Time
}

// same says whether two questions ask the same: six tenths of their key
// words alike, of all the two hold.
func same(a, b []key) bool {
	both := 0
	for _, x := range a {
		if slices.ContainsFunc(b, func(y key) bool { return alike(x.stem, y.stem) }) {
			both++
		}
	}
	all := len(a) + len(b) - both
	return all > 0 && float64(both) >= 0.6*float64(all)
}

// stems is the stems of a text's words that aren't too common.
func stems(text string) []string {
	var out []string
	for _, w := range strings.Fields(text) {
		if b := bare(w); b != "" && !stop[b] {
			out = append(out, stem(b))
		}
	}
	return out
}

func like(stems []string, s string) bool {
	return slices.ContainsFunc(stems, func(t string) bool { return alike(s, t) })
}

// alike says whether two stems name the same thing: the same stem, kin
// such as "launch" and "release", or a long word misheard by a letter, or
// by two in a word of nine letters or more.
func alike(a, b string) bool {
	if a == b || kin[a] != "" && kin[a] == kin[b] {
		return true
	}
	ra, rb := []rune(a), []rune(b)
	if len(ra) < 6 || len(rb) < 6 {
		return false
	}
	most := 1
	if min(len(ra), len(rb)) >= 9 {
		most = 2
	}
	return apart(ra, rb) <= most
}

// apart is how many letters to add, drop or change to make one word the
// other.
func apart(a, b []rune) int {
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

// stem is a word without the endings that make it plural, past or
// ongoing, so "launches", "launched" and "launching" are all "launch".
func stem(w string) string {
	w = strings.TrimSuffix(w, "'s")
	n := len(w)
	switch {
	case n > 5 && strings.HasSuffix(w, "ing"):
		w = w[:n-3]
	case n > 4 && (strings.HasSuffix(w, "ies") || strings.HasSuffix(w, "ied")):
		w = w[:n-3] + "y"
	case n > 4 && strings.HasSuffix(w, "ed"):
		w = w[:n-2]
	case n > 4 && (strings.HasSuffix(w, "ches") || strings.HasSuffix(w, "shes") || strings.HasSuffix(w, "sses") || strings.HasSuffix(w, "xes")):
		w = w[:n-2]
	case n > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us") && !strings.HasSuffix(w, "is"):
		w = w[:n-1]
	}
	if len(w) > 3 && strings.HasSuffix(w, "e") {
		w = w[:len(w)-1]
	}
	return w
}

// bare is a word in lower case, without the marks around it.
func bare(w string) string {
	w = strings.ToLower(strings.ReplaceAll(w, "’", "'"))
	return strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '$' && r != '%' })
}

// named says whether a word is written as a name is, with a capital.
func named(w string) bool {
	r := []rune(strings.TrimLeft(w, "\"'“‘("))
	return len(r) > 1 && unicode.IsUpper(r[0]) && w != "I" && !strings.HasPrefix(w, "I'")
}

// find is where in words one of phrases starts, and its length, or -1.
func find(words []string, phrases [][]string) (int, int) {
	for i := range words {
		for _, p := range phrases {
			if i+len(p) <= len(words) && slices.Equal(words[i:i+len(p)], p) {
				return i, len(p)
			}
		}
	}
	return -1, 0
}

// holds says whether words hold one of phrases.
func holds(words []string, phrases [][]string) bool {
	i, _ := find(words, phrases)
	return i >= 0
}

func set(words string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(words) {
		out[w] = true
	}
	return out
}
