package live_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gitdek/onair/internal/live"
	"github.com/gitdek/onair/internal/localmodel"
)

// fictional is a made-up brief about a made-up company, about as long as
// the archive's: 25 lines of four meetings' notes.
func fictional() live.Brief {
	var b live.Brief
	add := func(meeting, date, under, at, text string) {
		b.Points = append(b.Points, live.Point{Text: text, Under: under, Meeting: meeting, Date: date, At: at, URL: "https://archive.example/m/" + date + "?t=" + at})
	}
	for _, l := range [][4]string{
		{"Investors · Discussion", "6:12", "Halcyon hopes to close a $750k pre-seed round before the end of the year, led by two angel investors who already know the founders"},
		{"Investors · Discussion", "11:40", "Halcyon will hire three engineers next quarter once the round closes, starting with a mobile developer"},
		{"Next steps", "", "A follow-up with the partners is planned for the first week of October to go over the term sheet"},
		{"Pricing", "15:02", "The team settled on $12 a seat per month for the starter plan, with a discount for annual billing"},
		{"Pricing", "16:30", "Enterprise pricing stays custom and is quoted by the sales lead after a call"},
	} {
		add("Halcyon partner sync", "2026-09-14", l[0], l[1], l[2])
	}
	for _, l := range [][4]string{
		{"Release timeline", "1:05:09", "Doors open to the public on the 9th of December, after a two week private beta with forty testers"},
		{"Release timeline", "1:06:30", "The press kit still needs a logo and three screenshots before the public opening"},
		{"Walkthrough", "4:21", "Priya clicked through 9 mockups for the team, covering sign up, the dashboard and settings"},
		{"Requests", "18:30", "Priya listed the features she needs next: offline sync, a calendar view and shared folders"},
		{"Requests", "19:10", "Voice notes were asked for again, but the team left them for next year"},
		{"Decisions", "40:02", "The team agreed to drop the Android tablet layout from the first release"},
		{"Decisions", "41:15", "Support will run through email only until the public opening"},
		{"Risks", "47:30", "The payment provider review may take three weeks, which could push the paid plans back"},
	} {
		add("Halcyon product walkthrough", "2026-10-08", l[0], l[1], l[2])
	}
	for _, l := range [][4]string{
		{"Hiring", "2:10", "Marco accepted the offer for the mobile developer role and starts on November 3"},
		{"Hiring", "3:05", "The second engineering role is still open, with five candidates in the pipeline"},
		{"Metrics", "8:40", "The waitlist reached 2,300 sign ups, up from 1,400 at the start of the month"},
		{"Metrics", "9:15", "Weekly active testers held at about 35 of the 40 in the private beta"},
		{"Marketing", "14:00", "The launch post will go out on the company blog and two newsletters on opening day"},
		{"Infrastructure", "20:20", "Hosting moves to a second region before the opening to keep the app up during an outage"},
		{"Budget", "25:45", "Monthly cloud spend is about $4,800 and should stay under $6,000 after the opening"},
	} {
		add("Halcyon weekly", "2026-10-21", l[0], l[1], l[2])
	}
	for _, l := range [][4]string{
		{"Updates", "0:55", "Tomas demoed the Beacon assistant to the board, who asked for a clearer privacy page"},
		{"Updates", "7:45", "Tomas says the Beacon assistant waits on a new model before its next release"},
		{"Partnership", "12:00", "Beacon and Halcyon talked about a shared calendar integration, with no date set"},
		{"Next steps", "30:10", "Tomas will send the integration outline to Priya by the end of next week"},
		{"Open questions", "33:00", "Nobody has decided who owns the shared customer list between the two companies"},
	} {
		add("Beacon check-in", "2026-10-30", l[0], l[1], l[2])
	}
	b.Passages = 8
	return b
}

// TestTheRealWriter runs the real writing model, from the environment
// tools/setup-models makes, on made-up notes and made-up words, when
// ONAIR_LOCAL_MODEL_PYTHON names its Python, and times it. It is skipped
// otherwise, as in CI.
func TestTheRealWriter(t *testing.T) {
	python := os.Getenv("ONAIR_LOCAL_MODEL_PYTHON")
	if python == "" {
		t.Skip("ONAIR_LOCAL_MODEL_PYTHON isn't set")
	}
	w := localmodel.NewWriter(python)
	defer w.Close()
	write := func(p live.Prompt, timeout time.Duration) (string, time.Duration) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		began := time.Now()
		text, err := w.Write(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		return text, time.Since(began)
	}
	b := fictional()
	began := time.Now()
	w.Warm()
	p, _ := live.AnswerFor("how much were they looking to raise for Halcyon", b)
	write(live.Prompt{System: p.System, User: "Notes:\n[1] made up\n\nQuestion: is it loaded?", Most: 4}, 2*time.Minute)
	t.Logf("loaded in %v", time.Since(began))
	slowest := time.Duration(0)
	for _, tc := range []struct{ said, has, cites, result string }{
		{"how much were they looking to raise for Halcyon", "$750k", "Halcyon hopes to close a $750k pre-seed round", "answered"},
		{"what day are we shipping it", "9", "Doors open to the public on the 9th of December", "answered"},
		{"what features is Halcyon planning for the next version", "offline sync", "Priya listed the features she needs next", "answered"},
		{"what did the lawyers say about the office lease", "", "", "not in the archive"},
	} {
		p, read := live.AnswerFor(tc.said, b)
		text, took := write(p, 30*time.Second)
		answer, cited, result := read(text)
		t.Logf("%q in %v, %d prompt characters: %s %q", tc.said, took, len(p.System+p.User), result, answer)
		slowest = max(slowest, took)
		if result != tc.result || !strings.Contains(answer, tc.has) || tc.cites != "" && (len(cited) == 0 || !strings.HasPrefix(cited[0].Text, tc.cites)) {
			t.Errorf("%q: %s %q citing %+v, from %q", tc.said, result, answer, cited, text)
		}
	}
	for _, tc := range []struct {
		said  []string
		shown string
	}{
		{[]string{"Honestly the weather has been great this week.", "So Halcyon is closing a two million dollar seed round next month."}, "the record says “Halcyon hopes to close a $750k pre-seed round"},
		{[]string{"Halcyon is still aiming for the $750k pre-seed round.", "And the waitlist is past two thousand now."}, ""},
	} {
		p, read := live.CheckFor(tc.said, b)
		text, took := write(p, 30*time.Second)
		shown, result := read(text)
		t.Logf("a check in %v, %d prompt characters: %s %q", took, len(p.System+p.User), result, shown)
		slowest = max(slowest, took)
		if !strings.Contains(shown, tc.shown) || tc.shown == "" && shown != "" {
			t.Errorf("%q: %s %q, from %q", tc.said, result, shown, text)
		}
	}
	if slowest > 5*time.Second {
		t.Errorf("the slowest took %v, once loaded", slowest)
	}
}
