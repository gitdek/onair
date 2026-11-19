package live

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// shelf is a card on the panel, and since when it has been on top: a card
// about a name, or the standing card, or a question's answer.
type shelf struct {
	at    time.Time
	topic *tcard
	q     *card
}

// leave marks the cards the panel showed last time and doesn't now as
// gone, so that one pushed off doesn't come back when another goes.
func (f *follow) leave(deck []shelf) {
	for _, old := range f.lastDeck {
		if !slices.ContainsFunc(deck, func(s shelf) bool { return s.topic == old.topic && s.q == old.q }) {
			if old.topic != nil {
				old.topic.gone = true
			} else {
				old.q.gone = true
			}
		}
	}
	f.lastDeck = deck
}

// deck is the cards the panel shows now, newest first, f.cfg.Cards at
// most: the cards about names, and the standing card, once they have a
// line to show, a name's newest card only; and the answer cards, from
// when their question was noticed, as the answer path has them.
func (f *follow) deck(now time.Time) []shelf {
	var all []shelf
	named := map[int]bool{}
	for _, c := range f.tcards {
		if len(c.lines) == 0 || c.shown.IsZero() || c.gone || c.subject >= 0 && named[c.subject] {
			continue
		}
		named[c.subject] = true
		all = append(all, shelf{at: c.shown, topic: c})
	}
	for _, c := range f.cards {
		if c.early || c.gone {
			continue
		}
		if c.shownAt.IsZero() {
			c.shownAt = now
		}
		all = append(all, shelf{at: c.shownAt, q: c})
	}
	slices.SortStableFunc(all, func(a, b shelf) int { return cmp.Compare(b.at.UnixNano(), a.at.UnixNano()) })
	return all[:min(len(all), max(f.cfg.Cards, 0))]
}

// card is what the panel draws of a card on the shelf. An answer card is
// its question, its word under it, and its answer a sentence a line, or
// the lines that answer it, each with its sources.
func (s shelf) card(f *follow) Card {
	if s.topic != nil {
		return Card{Title: s.topic.title, Lines: append([]CardLine(nil), s.topic.lines...), Written: s.topic.written}
	}
	a := f.answerCard(s.q)
	out := Card{Title: a.Question, Note: a.Note}
	if a.Text != "" {
		out.Lines, out.Written = answerCardLines(a.Text, s.q.chips), true
		return out
	}
	for _, l := range a.Lines {
		out.Lines = append(out.Lines, CardLine{Text: l.Text, Sources: []Source{{Label: whereLabel(l.Where), URL: l.URL}}})
	}
	return out
}

// answerCardLines is a written answer as a card's lines: a line for each
// of its sentences, without its citations, each with the sources it
// cites.
func answerCardLines(text string, chips []Source) []CardLine {
	var out []CardLine
	from := 0
	ends := sentenceEnd.FindAllStringIndex(text, -1)
	if len(ends) == 0 || ends[len(ends)-1][1] < len(text) {
		ends = append(ends, []int{len(text), len(text)})
	}
	for _, end := range ends {
		part := strings.TrimSpace(text[from:end[1]])
		from = end[1]
		if part == "" {
			continue
		}
		var srcs []Source
		for _, m := range citation.FindAllStringSubmatch(part, -1) {
			for _, s := range strings.Split(m[1], ",") {
				k, _ := strconv.Atoi(strings.TrimSpace(s))
				if k >= 1 && k <= len(chips) && !slices.Contains(srcs, chips[k-1]) {
					srcs = append(srcs, chips[k-1])
				}
			}
		}
		part = strings.Join(strings.Fields(citation.ReplaceAllString(part, "")), " ")
		part = strings.ReplaceAll(strings.ReplaceAll(part, " .", "."), " ,", ",")
		if part != "" {
			out = append(out, CardLine{Text: part, Sources: srcs})
		}
	}
	return out
}

// whereLabel is the date in where a line was said, such as "Pricing
// review · 2026-11-03 · 12:31", as a source's chip says it.
func whereLabel(where string) string {
	for _, part := range strings.Split(where, " · ") {
		if l := short(part, ""); l != "" {
			return l
		}
	}
	return "source"
}
