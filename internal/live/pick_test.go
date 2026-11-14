package live

import (
	"strings"
	"testing"
)

// found is the shape of find_connections' answer: meetings, notebook
// entries and action items, of which the archive suggests some.
func found() Found {
	return Found{
		Semantic: true,
		Meetings: []Meeting{
			{ID: "m1", Title: "Pricing review", Date: "2026-11-03", URL: "http://127.0.0.1:8797/m/m1", Why: []string{"shares pricing, launch", "close in meaning (0.71)", "reranked #1"},
				Passages: []Passage{{Text: "we agreed to hold the price until March", At: "12:31", URL: "http://127.0.0.1:8797/m/m1?t=751"}}},
			{ID: "m2", Title: "Launch plan", Date: "2026-10-28", URL: "http://127.0.0.1:8797/m/m2", Why: []string{"shares launch"}},
			{ID: "m3", Title: "Weak", Date: "2026-09-01", URL: "http://127.0.0.1:8797/m/m3", Why: []string{"shares the"}},
			{ID: "m4", Title: "Hiring", Date: "2026-08-01", URL: "http://127.0.0.1:8797/m/m4", Why: []string{"close in meaning (0.66)"}},
		},
		Notes: []Note{
			{ID: "n0", Title: "Words only", Updated: "2026-11-01T10:00:00.000Z", URL: "http://127.0.0.1:8797/notebook/n0", Why: []string{"shares launch"}},
			{ID: "n1", Title: "Price research", Updated: "2026-11-02T10:00:00.000Z", URL: "http://127.0.0.1:8797/notebook/n1", Why: []string{"close in meaning (0.64)", "shares pricing"}},
			{ID: "n2", Title: "Second note", Updated: "2026-11-02", URL: "http://127.0.0.1:8797/notebook/n2", Why: []string{"close in meaning (0.60)"}},
		},
		Items: []Item{
			{ID: "t1", Text: "Send the price sheet", Status: "done", Owner: "Someone", URL: "http://127.0.0.1:8797/m/m1?t=800", Meeting: "Pricing review", MeetingDate: "2026-11-03"},
			{ID: "t2", Text: "Decide the launch date", Status: "open", Owner: "unassigned", URL: "http://127.0.0.1:8797/m/m2?t=90", Meeting: "Launch plan", MeetingDate: "2026-10-28"},
			{ID: "t3", Text: "Not suggested", Status: "open", Owner: "Someone", URL: "http://127.0.0.1:8797/m/m2?t=95"},
		},
		StrongMeetings: []string{"m1", "m2", "m4"},
		StrongItems:    []string{"t1", "t2"},
	}
}

func TestOnlyWhatTheArchiveCallsStrongIsPicked(t *testing.T) {
	links := pick(found(), 10)
	var got []string
	for _, l := range links {
		got = append(got, l.Kind.String()+" "+l.ID)
	}
	// The two strongest meetings, then an open item, then the first
	// notebook entry matched by meaning, then the other meeting.
	if want := "Meeting m1, Meeting m2, Open item t2, Notebook n1, Meeting m4"; strings.Join(got, ", ") != want {
		t.Fatalf("picked %s, not %s", strings.Join(got, ", "), want)
	}
	if links := pick(found(), 3); len(links) != 3 || links[2].ID != "t2" {
		t.Fatalf("at most three: %v", links)
	}
	if links := pick(Found{}, 3); len(links) != 0 {
		t.Fatalf("an empty answer picked %v", links)
	}
}

func TestEachLinkSaysWhy(t *testing.T) {
	links := pick(found(), 10)
	want := []Link{
		{Kind: MeetingLink, ID: "m1", Title: "Pricing review", Date: "2026-11-03", Why: "12:31 “we agreed to hold the price until March”", URL: "http://127.0.0.1:8797/m/m1?t=751"},
		{Kind: MeetingLink, ID: "m2", Title: "Launch plan", Date: "2026-10-28", Why: "Shares launch", URL: "http://127.0.0.1:8797/m/m2"},
		{Kind: ItemLink, ID: "t2", Title: "Decide the launch date", Date: "2026-10-28", Why: "Open, with no owner, from Launch plan", URL: "http://127.0.0.1:8797/m/m2?t=90"},
		{Kind: NoteLink, ID: "n1", Title: "Price research", Date: "2026-11-02", Why: "Close in meaning; Shares pricing", URL: "http://127.0.0.1:8797/notebook/n1"},
		{Kind: MeetingLink, ID: "m4", Title: "Hiring", Date: "2026-08-01", Why: "Close in meaning", URL: "http://127.0.0.1:8797/m/m4"},
	}
	if len(links) != len(want) {
		t.Fatalf("picked %v", links)
	}
	for i := range want {
		if links[i] != want[i] {
			t.Fatalf("link %d is\n%+v, not\n%+v", i, links[i], want[i])
		}
	}
	f := found()
	f.Items[1].Owner, f.Items[1].Status = "Someone", "on_hold"
	if l := pick(f, 10)[2]; l.Why != "On hold, Someone's, from Launch plan" {
		t.Fatalf("an item on hold says %q", l.Why)
	}
}

func TestLinksThatCantBeOpenedAreLeftOut(t *testing.T) {
	f := found()
	f.Meetings[0].Passages[0].URL = "javascript:alert(1)"
	f.Meetings[1].URL = "file:///etc/passwd"
	f.Items[1].URL = ""
	f.Notes[1].URL = "http://user:secret@127.0.0.1/notebook/n1"
	links := pick(f, 10)
	var got []string
	for _, l := range links {
		got = append(got, l.ID+" "+l.URL)
	}
	// m1 keeps its meeting's own link when its moment's can't be opened,
	// and m2, t2 and n1 are left out.
	if want := "m1 http://127.0.0.1:8797/m/m1, m4 http://127.0.0.1:8797/m/m4, n2 http://127.0.0.1:8797/notebook/n2"; strings.Join(got, ", ") != want {
		t.Fatalf("picked %s, not %s", strings.Join(got, ", "), want)
	}
	for _, link := range []string{"https://example.test/m/1", "http://127.0.0.1:8797/m/1"} {
		if !openable(link) {
			t.Fatalf("%s can't be opened", link)
		}
	}
	for _, link := range []string{"", "/m/1", "mailto:someone", "ftp://127.0.0.1/x", "http://", "http://a:b@host/"} {
		if openable(link) {
			t.Fatalf("%s can be opened", link)
		}
	}
}

func TestLongLinesAreCutAtAWord(t *testing.T) {
	long := strings.Repeat("word ", 50)
	if got := line(long, 30); got != "word word word word word word…" {
		t.Fatalf("cut to %q", got)
	}
	if got := line(quote(long), 20); !strings.HasSuffix(got, "…”") || !strings.HasPrefix(got, "“") {
		t.Fatalf("a quote cut to %q", got)
	}
	if got := line("  short \n line ", 30); got != "short line" {
		t.Fatalf("a short line is %q", got)
	}
}
