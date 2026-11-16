package live

import (
	"fmt"
	"strings"
	"time"
)

// What the real model's test, in package live_test, needs of the inside:
// the prompts On Air builds, and its checks of what the model writes.

// AnswerFor is what the writer is asked about a question said in a call,
// given a brief and what the writer reworded it as, if anything, and how
// what it writes is read: the answer shown, the lines it cites, and what
// came of it.
func AnswerFor(said string, b Brief, reworded ...string) (Prompt, func(text string) (string, []Point, string)) {
	q, ok := notice(utterance{text: said})
	if !ok {
		return Prompt{}, nil
	}
	if len(reworded) > 0 {
		q.extra = readReword(reworded[0], q)
	}
	from := passages(q, b, Defaults.AnswerTokens)
	return answerPrompt(q, from), func(text string) (string, []Point, string) {
		shown, cited, o, why := readAnswer(text, len(from))
		switch o {
		case answered:
			var lines []Point
			for _, k := range cited {
				lines = append(lines, from[k-1])
			}
			return shown, lines, "answered"
		case notInArchive:
			return "", nil, "not in the archive"
		}
		return "", nil, "rejected: " + why
	}
}

// CheckFor is what the writer is asked about statements said in a call,
// given a brief fetched just before, and how what it writes is read: the
// panel's line, and what came of it.
func CheckFor(said []string, b Brief) (Prompt, func(text string) (string, string)) {
	f := listening(Defaults)
	f.writes(true)
	f.remember(b, t0)
	for _, s := range said {
		f.state(utterance{text: s, to: t0})
	}
	p, n, _, _, ok := f.checkDue(t0.Add(time.Second))
	if !ok {
		return Prompt{}, nil
	}
	return p, func(text string) (string, string) {
		g := *f
		g.differed = map[string]time.Time{}
		result, _ := g.checkedBy(n, text, nil, t0.Add(2*time.Second))
		return g.panel(t0.Add(2 * time.Second)).Differs.Text, result
	}
}

// Roadmap is a made-up brief about a made-up company, about as long as
// the archive's of twelve passages, whose lines in a call came to 70 to 80
// tokens each: 39 lines of five meetings' notes, about 2,200 tokens. The
// lines about what Rafael wants next sit under a heading on capabilities
// and the roadmap, late in the brief, and none of them says "feature",
// while many other lines name Rafael or a version.
func Roadmap() Brief {
	var b Brief
	add := func(meeting, date, under string, lines ...string) {
		for i, l := range lines {
			at := fmt.Sprintf("%d:%02d", 5+i*3, (i*17)%60)
			b.Points = append(b.Points, Point{Text: l, Under: under, Meeting: meeting, Date: date, At: at, URL: "https://archive.example/m/" + date + "?t=" + at})
		}
	}
	add("Kestrel weekly with Rafael", "2026-09-02", "Team and the winter plan · Hiring",
		"Rafael wants a second designer before the spring campaign, and the budget allows one more hire at most this year, so the developer role waits until the seed money is in",
		"Rafael will interview the two shortlisted designers next Tuesday and Wednesday, and asked the team to send him their questions for the portfolio review by Monday",
		"The contractor who built the first version of the app leaves at the end of the month, and has agreed to write down how the sync code works before she goes")
	add("Kestrel weekly with Rafael", "2026-09-02", "How the app is doing · Metrics",
		"Weekly active gardeners reached 1,850, up from 1,400 at the start of the month, most of the growth coming after the newsletter mentioned the app",
		"Rafael noted that retention after the first week is still under forty percent, and that most people who leave never set up a second plant",
		"Version 1.3 cut the crash rate in half on older phones, and the reviews in the app store have gone from three stars to just over four since it came out")
	add("Kestrel weekly with Rafael", "2026-09-02", "Spending · Budget",
		"Monthly cloud spend is about $2,100 and should stay under $3,000 through the winter, as long as photos are stored at a lower resolution after thirty days",
		"Rafael asked for a cheaper image store before the next version ships, and the developers will compare two providers and report back at the next weekly")
	add("Kestrel product review with Rafael", "2026-09-23", "Getting the release out · Release",
		"The new version goes to the app stores on the 14th of October if the review passes, with the press note going out the same morning to the two newsletters",
		"Rafael wants the release notes written by the support lead, not the developers, so that they say what changed for a gardener rather than what changed in the code",
		"The beta group of 60 testers gets the new version a week earlier, and anything they report as broken has to be fixed or switched off before the public release",
		"Version numbers move to the year and month, starting with 2026.10, so that support can tell at a glance how old the copy someone is using is")
	add("Kestrel product review with Rafael", "2026-09-23", "How it looks · Design",
		"The onboarding screens drop from five to three after the usability sessions showed most people skipping the last two without reading them",
		"Rafael disliked the green on the settings page and asked for the brand palette everywhere, which the designer will apply before the release")
	add("Kestrel partner call", "2026-10-07", "Working with garden centres · Discussion",
		"Two more garden centres want flyers for their tills, and one of them asked for a discount code it can give to customers who buy seedlings",
		"The partner programme pays a small fee for each gardener who stays three months, which the garden centres said is enough to keep the flyers out",
		"Rafael will draft the partner agreement with the lawyer by the end of October, so that the new garden centres can sign before the spring")
	add("Kestrel partner call", "2026-10-07", "The seed supplier · Discussion",
		"The seed supplier offered to list Kestrel in its spring catalogue for free, which reaches about forty thousand households by post",
		"In return it wants its seed packets to link to the care guides in the app, with a code on each packet that opens the right guide")
	add("Kestrel weekly with Rafael", "2026-10-14", "The October release · Release",
		"The new version passed review on the first try and went out on the 14th, a day after the beta group said the last two problems were fixed",
		"Rafael said the release went smoothly and thanked the support lead for the release notes, which got two replies from gardeners saying they were clear",
		"Crash reports since the release are down to two a day, both on one older tablet that the team has decided to stop supporting in the spring")
	add("Kestrel weekly with Rafael", "2026-10-14", "Answering gardeners · Support",
		"Support answered 140 emails last week, most of them about the watering schedule, and the average answer took just under two working days",
		"Rafael wants answers within one working day, and the team will try canned replies for the five most common questions to get there",
		"A help page on moving plants between beds would cut about a fifth of the emails, and the support lead will write it this month")
	add("Kestrel weekly with Rafael", "2026-10-14", "Money and runway · Money",
		"The seed round of $600k closed with three angel investors, one of whom runs a chain of garden centres and offered to introduce the team to others",
		"Rafael will send the investors a short update every quarter, with the number of active gardeners, the money left and the next quarter's plan",
		"Runway is now about fourteen months at the current spend, or eleven if both the designer and the developer are hired before the summer")
	add("Kestrel board update", "2026-10-28", "Where Kestrel stands · Updates",
		"The board asked how Kestrel compares with the two bigger garden apps, and Rafael answered that it wins on reminders and loses on plant identification",
		"Rafael presented the growth numbers and the plan for the winter, and the board asked for the spring campaign's budget in writing by December",
		"Rafael said the spring campaign starts in March with the two garden centres and the seed catalogue, and expects the paid plan's waitlist to pass 500 by February")
	add("Kestrel board update", "2026-10-28", "What could go wrong · Risks",
		"The licensed plant classifier raises its price in January by about a third, which would add roughly $400 a month to the cloud bill",
		"The one developer who knows the sync code is a single point of failure, and the board asked for a second person to learn it before the spring")
	add("Kestrel product review with Rafael", "2026-10-30", "What Rafael wants next · Capabilities and longer-term roadmap",
		"Desktop and tablet apps once the phone app settles, with the same account and the same reminders, so gardeners can plan beds on a bigger screen",
		"A plant-health classifier trained in-house rather than the licensed one used today, starting from the photos gardeners have agreed to share",
		"Step-by-step watering reminders tied to the local weather forecast, so that nobody is told to water the day before a storm",
		"Shared gardens, so that a family or an allotment group can look after the same plants and see who watered what")
	add("Kestrel board update", "2026-10-28", "After the meeting · Next steps",
		"Rafael will bring a hiring plan for the developer role to the next board meeting, with two options for when to start",
		"The finance lead will compare two accounting tools before December and pick the one the accountant already knows")
	b.Passages = 12
	return b
}

// RewordFor is what the writer is asked to reword a question said in a
// call as, and how what it writes is read: the words added, and what On
// the Record is then asked.
func RewordFor(said string) (Prompt, func(text string) ([]string, string)) {
	q, ok := notice(utterance{text: said})
	if !ok {
		return Prompt{}, nil
	}
	return rewordPrompt(q), func(text string) ([]string, string) {
		var added []string
		for _, k := range readReword(text, q) {
			added = append(added, k.word)
		}
		return added, strings.TrimSpace(q.topic + " " + strings.Join(added, " "))
	}
}
