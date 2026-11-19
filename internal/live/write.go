package live

import (
	"cmp"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The local model writes two things, and plain Go decides what it is
// asked and checks what it writes before any of it is shown: an answer to a
// question asked in the call, from the lines of On the Record's brief; and
// whether something just said differs from what the archive recorded.

// Prompts the model is given. What follows them is numbered material, and
// what was asked or said.
const (
	answerSystem = "You answer a question asked in a meeting from notes of earlier meetings. Use only the numbered notes you are given. " +
		"Write one or two short sentences, 40 words at most, that answer the question, naming the items of a list briefly, and after each fact put the number of the note it comes from in square brackets, like [3]. " +
		"Notes may use other words for what is asked, such as capabilities or plans for features. " +
		"A question about what someone wanted, planned or would like is answered by notes about their goals, plans, roadmap or future work, even when worded differently. " +
		"Don't add anything the notes don't say. Only if no note is about what the question asks, write exactly: Not in the archive"
	rewordSystem = "You help search the notes of earlier meetings for what a question asked in a meeting is about. " +
		"Reply with one line: other words the notes may use for the things it asks about, separated by commas, at most 8, and nothing else. " +
		"For a question about features, say: capabilities, roadmap, plans, improvements, new version. " +
		"For one about money: funding, round, investors, raise."
	checkSystem = "You check what was just said in a meeting against notes of earlier meetings. " +
		"Look for a statement that contradicts a note about the same thing: a different number, amount, date, name or decision. " +
		"Something the notes don't mention isn't a difference, and neither is a statement that agrees with a note, or rounds or approximates what it says: " +
		`"over two thousand" fits a note that says 2,300. ` +
		`Reply with JSON only. If nothing said differs from the notes, reply {"differs": false}. ` +
		`If something does, reply {"differs": true, "statement": 2, "note": 5, "said": "the differing words, copied from statement S2", "record": "what note [5] says instead, copied from it"}, ` +
		"with the statement's number and the note's number."
)

// Limits on what the model is given and what it may write.
const (
	passageChars = 400 // of a line of a brief, at most, as the model is given it
	answerTokens = 120 // the model may write for an answer
	answerMost   = 300 // characters of an answer shown, at most; a longer one is rejected
	checkTokens  = 160 // the model may write for a check
	rewordTokens = 40  // the model may write for a question's other words
	rewordMost   = 8   // of its words added to a question, at most
	checkSaid    = 8   // statements a check gives, the newest
	checkRecords = 12  // and records, the most related
	checkMost    = 900 // tokens of records a check gives, about
)

// outcome is what came of what the model wrote.
type outcome int

const (
	answered outcome = iota + 1
	notInArchive
	rejected
)

// tokens is about how many tokens a text takes: a token is about four
// characters of English.
func tokens(s string) int { return (utf8.RuneCountInString(s)+3)/4 + 1 }

// whereSaid is where a line of a brief was said: its meeting, date and
// moment, such as "Pricing review · 2026-11-03 · 12:31".
func whereSaid(p Point) string {
	var where []string
	for _, w := range []string{line(p.Meeting, 60), p.Date, p.At} {
		if w != "" {
			where = append(where, w)
		}
	}
	return strings.Join(where, " · ")
}

// numbered is a line of a brief as the model is given it, numbered n:
// its meeting, date and what it sits under, then its text.
func numbered(n int, p Point) string {
	about := []string{}
	for _, w := range []string{line(p.Meeting, 60), p.Date} {
		if w != "" {
			about = append(about, w)
		}
	}
	text := line(p.Text, passageChars)
	switch {
	case p.Said && p.At != "":
		about = append(about, "said at "+p.At)
		text = quote(text)
	case p.Said:
		about = append(about, "said")
		text = quote(text)
	case p.Under != "":
		about = append(about, line(p.Under, 80))
	}
	return fmt.Sprintf("[%d] %s: %s", n, strings.Join(about, ", "), text)
}

// passages are the lines of a brief the model answers a question from:
// each line with text that a browser can open, the same line once, ranked
// by rank, the cheap first ranking, which counts the writer's other words
// for the question's and what a line sits under, and else in the brief's
// order, up to about budget tokens. A line that shares no word with the
// question is given too, below those that do: the answer often shares
// none.
func passages(q question, b Brief, budget int) []Point {
	type scored struct {
		p Point
		r float64
	}
	var all []scored
	seen := map[string]bool{}
	for _, p := range b.Points {
		key := strings.ToLower(strings.Join(strings.Fields(p.Text), " "))
		if key == "" || !openable(p.URL) || seen[key] {
			continue
		}
		seen[key] = true
		all = append(all, scored{p, q.rank(p)})
	}
	slices.SortStableFunc(all, func(a, b scored) int { return cmp.Compare(b.r, a.r) })
	var out []Point
	used := 0
	for _, s := range all {
		cost := tokens(numbered(len(out)+1, s.p))
		if used+cost > budget {
			break
		}
		used += cost
		out = append(out, s.p)
	}
	return out
}

// answerPrompt asks the model to answer a question from passages.
func answerPrompt(q question, from []Point) Prompt {
	var b strings.Builder
	b.WriteString("Notes:\n")
	for i, p := range from {
		b.WriteString(numbered(i+1, p) + "\n")
	}
	asked := q.shown
	if !strings.HasSuffix(asked, "?") {
		asked = "What do the notes say about " + strings.TrimRight(q.topic, ".,;:!?…") + "?"
	}
	b.WriteString("\nQuestion: " + asked)
	return Prompt{System: answerSystem, User: b.String(), Most: answerTokens}
}

// rewordPrompt asks the model for other words for what a question asks
// about, for searching the notes with. It is asked not to wait for the
// model to load: a question goes as heard rather than late.
func rewordPrompt(q question) Prompt {
	asked := q.shown
	if !strings.HasSuffix(asked, "?") {
		asked = "What do the notes say about " + strings.TrimRight(q.topic, ".,;:!?…") + "?"
	}
	return Prompt{System: rewordSystem, User: "Question: " + asked, Most: rewordTokens, Quick: true}
}

// readReword reads the model's other words for a question: of the first
// line it wrote, the words that aren't too common, aren't the question's
// own, and are plain words or numbers of 3 to 24 letters, once each,
// rewordMost at most. Each counts as one key word.
func readReword(text string, q question) []key {
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	own := map[string]bool{}
	for _, w := range strings.Fields(q.topic) {
		own[stem(bare(w))] = true
	}
	var out []key
	for _, w := range strings.FieldsFunc(first, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\'' && r != '’' && r != '-'
	}) {
		b := strings.Trim(bare(w), "'-")
		if len(b) < 3 || stop[b] || utf8.RuneCountInString(b) > 24 {
			continue
		}
		if s := stem(b); !own[s] {
			own[s] = true
			out = append(out, key{stem: s, weight: 1, word: b})
		}
		if len(out) == rewordMost {
			break
		}
	}
	return out
}

var (
	// citation is a citation of passages by number, such as "[2]" or
	// "[1, 3]".
	citation = regexp.MustCompile(`\[\s*(\d{1,3}(?:\s*,\s*\d{1,3})*)\s*\]`)
	// sentenceEnd ends a sentence: a full stop, question mark or
	// exclamation mark, the citations after it, and a space or the end.
	sentenceEnd = regexp.MustCompile(`[.!?](?:\s*\[[0-9,\s]*\])*(?:\s+|$)`)
)

// readAnswer checks what the model wrote for a question, given the
// passages it was given, numbered from 1. It is either exactly "Not in the
// archive", or an answer: those of its first two sentences that cite, at
// most answerMost characters, which cite only the numbers 1 to n, in
// square brackets, and put nothing else in them. An answer is shown with
// its citations numbered again from 1, in the order they are first cited;
// cited is the passages' numbers in that order. Anything else is rejected,
// and why says why in On Air's own words.
func readAnswer(text string, lines []Point) (shown string, cited []int, o outcome, why string) {
	n := len(lines)
	t := strings.Join(strings.Fields(strings.ReplaceAll(text, "**", "")), " ")
	t = strings.Trim(t, "\"'`“”")
	t = strings.TrimSpace(t)
	if strings.EqualFold(strings.TrimRight(t, ".! "), "Not in the archive") {
		return "", nil, notInArchive, ""
	}
	if t == "" {
		return "", nil, rejected, "it wrote nothing"
	}
	if strings.Contains(strings.ToLower(t), "not in the archive") {
		return "", nil, rejected, "it answered and said it couldn't"
	}
	// The first two sentences, each kept only when it cites.
	var parts, kept []string
	from := 0
	for _, end := range sentenceEnd.FindAllStringIndex(t, -1) {
		parts = append(parts, strings.TrimSpace(t[from:end[1]]))
		from = end[1]
	}
	if rest := strings.TrimSpace(t[from:]); rest != "" {
		parts = append(parts, rest)
	}
	for _, one := range parts[:min(2, len(parts))] {
		if citation.MatchString(one) {
			kept = append(kept, one)
		}
	}
	t = strings.Join(kept, " ")
	if utf8.RuneCountInString(t) > answerMost {
		return "", nil, rejected, "its answer was too long"
	}
	if rest := citation.ReplaceAllString(t, ""); strings.ContainsAny(rest, "[]") {
		return "", nil, rejected, "it put something other than numbers in brackets"
	}
	renumbered := map[int]int{}
	bad := false
	shown = citation.ReplaceAllStringFunc(t, func(c string) string {
		var mine []string
		for _, s := range strings.Split(citation.FindStringSubmatch(c)[1], ",") {
			k, _ := strconv.Atoi(strings.TrimSpace(s))
			if k < 1 || k > n {
				bad = true
				continue
			}
			if renumbered[k] == 0 {
				cited = append(cited, k)
				renumbered[k] = len(cited)
			}
			if m := strconv.Itoa(renumbered[k]); !slices.Contains(mine, m) {
				mine = append(mine, m)
			}
		}
		return "[" + strings.Join(mine, ", ") + "]"
	})
	switch {
	case bad:
		return "", nil, rejected, "it cited a passage it wasn't given"
	case len(cited) == 0:
		return "", nil, rejected, "it cited nothing"
	}
	return shown, cited, answered, ""
}

// statement is a sentence said in the call that states a fact, and when
// it ended.
type statement struct {
	text  string
	at    time.Time
	facts []string
}

// record is a line of a brief fetched lately that states a fact, and when
// it was fetched.
type record struct {
	p     Point
	at    time.Time
	facts []string
}

// Words that state a fact, besides numbers and names: amounts, dates and
// decisions.
var (
	numberWords   = set("zero one two three four five six seven eight nine ten eleven twelve fifteen twenty thirty forty fifty sixty seventy eighty ninety hundred thousand million billion half dozen first second third fourth fifth tenth percent")
	dateWords     = set("january february march april june july august september october november december monday tuesday wednesday thursday friday saturday sunday today tomorrow yesterday tonight week month quarter year weekend")
	decisionWords = set("decide decided agree agreed approve approved drop dropped cancel cancelled canceled postpone postponed delay delayed move moved sign signed chose choose pick picked reject rejected keep kept hire hired")
	moneyWords    = set("dollar dollars euro euros pound pounds k")
)

// norm is a word as facts compare it: in lower case, a number without its
// commas, currency sign or ordinal ending, and any other word its stem.
func norm(w string) string {
	b := bare(w)
	if b == "" {
		return ""
	}
	if unicode.IsDigit([]rune(strings.TrimLeft(b, "$"))[0]) {
		b = strings.NewReplacer("$", "", ",", "").Replace(b)
		for _, end := range []string{"st", "nd", "rd", "th"} {
			if strings.HasSuffix(b, end) && len(b) > len(end) && unicode.IsDigit(rune(b[len(b)-len(end)-1])) {
				b = strings.TrimSuffix(b, end)
			}
		}
		return b
	}
	return stem(b)
}

// facts are the words of a text that state a fact: numbers, amounts,
// dates, decisions, and names, a word with a capital that doesn't start
// the text, normalized.
func facts(text string) []string {
	var out []string
	for i, w := range strings.Fields(text) {
		b := bare(w)
		if b == "" {
			continue
		}
		digit := strings.IndexFunc(b, unicode.IsDigit) >= 0
		if digit || numberWords[b] || dateWords[b] || decisionWords[b] || moneyWords[b] && i > 0 || i > 0 && named(w) && !stop[b] {
			if n := norm(w); n != "" && !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	return out
}

// normWords is every word of a text, normalized as facts are.
func normWords(text string) []string {
	var out []string
	for _, w := range strings.Fields(text) {
		if n := norm(w); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// related is how many key words a statement and a record share, by stem.
func related(said, rec string) int {
	n := 0
	theirs := stems(rec)
	for _, s := range stems(said) {
		if like(theirs, s) {
			n++
		}
	}
	return n
}

// checkPrompt asks the model whether statements differ from records.
func checkPrompt(said []statement, records []record) Prompt {
	var b strings.Builder
	b.WriteString("Notes:\n")
	for i, r := range records {
		b.WriteString(numbered(i+1, r.p) + "\n")
	}
	b.WriteString("\nJust said:\n")
	for i, s := range said {
		fmt.Fprintf(&b, "S%d. %s\n", i+1, line(s.text, passageChars))
	}
	return Prompt{System: checkSystem, User: strings.TrimSpace(b.String()), Most: checkTokens}
}

// leading are a statement's or a note's number, as the model may copy it
// into what it quotes.
var leading = regexp.MustCompile(`^(?:S\d+[.:]?|\[\d+\])\s*`)

// readCheck checks what the model wrote about statements and records. It
// must be JSON, saying whether something said differs; when it does, the
// statement's and the record's numbers must be ones it was given, the
// words it quotes from each must be in it, and differ, and the two must
// share a key word, and the statement must state a fact the record's line
// doesn't hold, so that a number said again in other words, or a
// statement about something else, isn't taken for a difference. It
// returns the statement and record that differ, or why it was rejected in
// On Air's own words. quoted and recorded are what it quoted from each.
func readCheck(text string, said []statement, records []record) (s *statement, r *record, quoted, recorded, why string) {
	i, j := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if i < 0 || j < i {
		return nil, nil, "", "", "it didn't write JSON"
	}
	var v struct {
		Differs   *bool           `json:"differs"`
		Statement json.RawMessage `json:"statement"`
		Note      json.RawMessage `json:"note"`
		Said      string          `json:"said"`
		Record    string          `json:"record"`
	}
	if json.Unmarshal([]byte(text[i:j+1]), &v) != nil || v.Differs == nil {
		return nil, nil, "", "", "its JSON wasn't in the shape asked for"
	}
	if !*v.Differs {
		return nil, nil, "", "", ""
	}
	si, ri := number(v.Statement), number(v.Note)
	if si < 1 || si > len(said) || ri < 1 || ri > len(records) {
		return nil, nil, "", "", "it named a statement or a note it wasn't given"
	}
	s, r = &said[si-1], &records[ri-1]
	quoted, recorded = strings.TrimSpace(leading.ReplaceAllString(v.Said, "")), strings.TrimSpace(leading.ReplaceAllString(v.Record, ""))
	switch q, rec := flat(quoted), flat(recorded); {
	case q == "" || rec == "":
		return nil, nil, "", "", "it didn't quote both sides"
	case !strings.Contains(flat(s.text), q) || !strings.Contains(flat(numbered(ri, r.p)), rec):
		return nil, nil, "", "", "what it quoted isn't in what it was given"
	case q == rec:
		return nil, nil, "", "", "both sides it quoted are the same"
	case related(s.text, r.p.Text) == 0:
		return nil, nil, "", "", "the statement and the note aren't about the same thing"
	}
	held := normWords(r.p.Text)
	numbers := false
	for _, f := range s.facts {
		// A number is compared whole: 750000 and 760000 differ, though
		// a word misheard by a letter is the same word.
		if slices.Contains(held, f) || !numeric(f) && like(held, f) {
			continue
		}
		if !numeric(f) {
			return s, r, quoted, recorded, ""
		}
		numbers = true
	}
	if numbers && !fits(s.text, r.p.Text) {
		return s, r, quoted, recorded, ""
	}
	return nil, nil, "", "", "the statement holds no fact the note doesn't, but for figures that fit it"
}

// numeric says whether a fact is a number, in digits or in words.
func numeric(f string) bool { return unicode.IsDigit([]rune(f)[0]) || numberWords[f] }

// rough are words that say a figure is only roughly so.
var rough = set("about around roughly nearly almost approximately over past above beyond under below some than")

// fits says whether every figure a statement gives fits a record's: the
// same as one of the record's, or, said roughly, as "about", "over" or
// "more than" say it, within half again of one of them, so that "past two
// thousand" fits 2,300 and "about $2 million" doesn't fit $750k.
func fits(said, rec string) bool {
	theirs := figures(rec)
	for _, f := range figures(said) {
		ok := false
		for _, g := range theirs {
			hi, lo := max(f.value, g.value), min(f.value, g.value)
			ok = ok || f.value == g.value || f.rough && lo > 0 && hi <= 1.5*lo
		}
		if !ok {
			return false
		}
	}
	return true
}

// figure is a number a text gives, and whether it is said roughly.
type figure struct {
	value float64
	rough bool
}

// Number words, by value, and those that multiply.
var (
	small = func() map[string]float64 {
		out := map[string]float64{}
		for i, w := range strings.Fields("zero one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen eighteen nineteen") {
			out[w] = float64(i)
		}
		for i, w := range strings.Fields("twenty thirty forty fifty sixty seventy eighty ninety") {
			out[w] = float64(20 + 10*i)
		}
		return out
	}()
	times = map[string]float64{"hundred": 100, "thousand": 1e3, "k": 1e3, "million": 1e6, "m": 1e6, "billion": 1e9, "bn": 1e9}
)

// figures are the numbers a text gives, such as 2000000 for "$2 million",
// 750000 for "$750k" and 2000 for "two thousand".
func figures(text string) []figure {
	var out []figure
	all := strings.Fields(text)
	for i := 0; i < len(all); i++ {
		v, ok := worth(bare(all[i]))
		if !ok {
			continue
		}
		f := figure{rough: i > 0 && rough[bare(all[i-1])] || i > 1 && rough[bare(all[i-2])]}
		for i+1 < len(all) {
			next := bare(all[i+1])
			if m, ok := times[next]; ok {
				v *= m
			} else if n, ok := small[next]; ok && v >= 20 && v < 100 && n < 10 {
				v += n
			} else {
				break
			}
			i++
		}
		f.value = v
		out = append(out, f)
	}
	return out
}

// worth is what a word is worth as a number: digits, with a currency
// sign, commas, a decimal point, an ordinal ending or a k, m or bn after
// them, or a number word.
func worth(w string) (float64, bool) {
	if n, ok := small[w]; ok {
		return n, true
	}
	w = strings.NewReplacer("$", "", "€", "", "£", "", ",", "").Replace(w)
	if w == "" || !unicode.IsDigit([]rune(w)[0]) {
		return 0, false
	}
	m := 1.0
	for _, end := range []string{"st", "nd", "rd", "th", "bn", "k", "m"} {
		if strings.HasSuffix(w, end) {
			w = strings.TrimSuffix(w, end)
			m = max(1, times[end])
			break
		}
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(w, "%"), 64)
	return v * m, err == nil
}

// number is a statement's or a note's number as the model wrote it: a
// number, or the first number in a string, such as "S2" or "[5]".
func number(raw json.RawMessage) int {
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return int(n)
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return 0
	}
	digits := strings.TrimLeftFunc(s, func(r rune) bool { return !unicode.IsDigit(r) })
	end := strings.IndexFunc(digits, func(r rune) bool { return !unicode.IsDigit(r) })
	if end >= 0 {
		digits = digits[:end]
	}
	k, _ := strconv.Atoi(digits)
	return k
}

// flat is a text as quotations are compared: in lower case, on one line,
// without quotation marks or the marks that end it.
func flat(s string) string {
	s = strings.NewReplacer("“", "", "”", "", "\"", "", "‘", "'", "’", "'").Replace(strings.ToLower(s))
	return strings.TrimRight(strings.Join(strings.Fields(s), " "), ".,;:!? ")
}

// around is s, at most n characters, cut so that part stays in sight when
// it is longer.
func around(s, part string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	at := strings.Index(strings.ToLower(s), strings.ToLower(strings.TrimSpace(part)))
	if at < 0 {
		return line(s, n)
	}
	from := max(0, utf8.RuneCountInString(s[:at])-n/3)
	if from == 0 {
		return line(s, n)
	}
	rest := line(string(r[from:]), n-1)
	if i := strings.Index(rest, " "); i >= 0 && i < 15 {
		rest = rest[i+1:]
	}
	return "…" + rest
}

// differsLine is what the panel shows for a statement that differs from a
// record, with where the record says it and the moment a click opens.
func differsLine(s statement, r record, said, rec string) AnswerLine {
	text := fmt.Sprintf("Differs from the record: said %s; the record says %s",
		quote(around(s.text, said, 100)), quote(around(r.p.Text, rec, 110)))
	return AnswerLine{Text: text, Where: whereSaid(r.p), URL: r.p.URL}
}
