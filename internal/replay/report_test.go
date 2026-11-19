package replay

import (
	"strings"
	"testing"
	"time"

	"github.com/gitdek/onair/internal/live"
)

// madeUpNames are names a made-up archive knows: a company, a person, and
// one of its owners.
var madeUpNames = []live.Named{
	{Title: "Quillstone", Says: [][]string{{"quillstone"}}},
	{Title: "Priya Shah", Person: true, Says: [][]string{{"priya", "shah"}, {"priya"}}},
	{Title: "Ana Lima", Person: true, Ours: true, Says: [][]string{{"ana", "lima"}, {"ana"}}},
}

func words(text string, from time.Duration) []Word {
	var out []Word
	for i, w := range strings.Fields(text) {
		at := from + time.Duration(i)*400*time.Millisecond
		out = append(out, Word{Text: w, From: at, To: at + 300*time.Millisecond})
	}
	return out
}

func TestTheNamesSaidAreFoundInTheTranscriptItself(t *testing.T) {
	ws := append(words("hi Ana, so the quill stone numbers", 0), words("and Priya's budget, and Quilstone again", 10*time.Second)...)
	got := Mentions(ws, madeUpNames)
	if len(got) != 2 || got[0].Title != "Quillstone" || got[0].At != 1600*time.Millisecond || got[1].Title != "Priya Shah" || got[1].At != 10400*time.Millisecond {
		t.Fatalf("mentions %+v", got)
	}
}

func TestARunIsMeasuredByItsCardsAndHowSoonTheyCame(t *testing.T) {
	start := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	at := func(s float64) time.Time { return start.Add(time.Duration(s * float64(time.Second))) }
	card := func(title, text string) live.Card {
		return live.Card{Title: title, Lines: []live.CardLine{{Text: text, Sources: []live.Source{{Label: "Mar 10", URL: "https://archive.example/m/3"}}}}}
	}
	written := func(c live.Card) live.Card { c.Written = true; return c }
	run := Played{Name: "after", Shown: []Shown{
		{At: at(0), Panel: &live.Panel{Visible: true}},
		{At: at(3), Panel: &live.Panel{Visible: true, Cards: []live.Card{card("Quillstone", "Last met Mar 10: glazes")}}},
		{At: at(8), Panel: &live.Panel{Visible: true, Cards: []live.Card{written(card("Quillstone", "They want a $40 glaze kit"))}}},
		{At: at(20), Panel: &live.Panel{Visible: true, Cards: []live.Card{{Title: "How much?", Lines: []live.CardLine{{Text: "No idea"}}}}}},
	}}
	mentions := []Mention{{Title: "Quillstone", At: 1600 * time.Millisecond}, {Title: "Priya Shah", At: 10 * time.Second}}
	m := Measures(run, start, 10*time.Minute, mentions)
	if m.Cards != 2 || m.PerTen != 2 || m.WithFacts != 0.5 || m.Written != 0.5 {
		t.Errorf("measured %+v", m)
	}
	if len(m.Found) != 1 || m.Found[0].Title != "Quillstone" || m.Found[0].After != 1400*time.Millisecond || len(m.Missed) != 1 || m.Missed[0] != "Priya Shah" {
		t.Errorf("found %+v, missed %v", m.Found, m.Missed)
	}
	var b strings.Builder
	if err := Report(&b, "A made-up replay", words("so the quill stone numbers", time.Second), start, []Played{run}, mentions); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"after: 2 cards", "00:03  said: “…so the quill stone numbers”", "+ Quillstone", "~ Quillstone", "· They want a $40 glaze kit  (Mar 10)"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("the report doesn't say %q:\n%s", want, b.String())
		}
	}
}
