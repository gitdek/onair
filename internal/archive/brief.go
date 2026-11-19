package archive

import (
	"regexp"
	"strings"

	"github.com/gitdek/onair/internal/live"
)

// The lines of a brief from gather_context, as On the Record writes them.
var (
	// A meeting's section starts "## 2026-11-03 · Pricing review", and
	// the meeting's link follows on the next line.
	meetingHeading = regexp.MustCompile(`^## (\d{4}-\d{2}-\d{2}) · (.+)$`)
	// A citation ends a line: " ([6:12](link))", with ", [board item](link)"
	// for an action item, or the board item alone.
	citation = regexp.MustCompile(`\s*\((?:\[[^\]]*\]\([^)\s]*\)(?:, )?)+\)$`)
	cited    = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]*)\)`)
	moment   = regexp.MustCompile(`^\d+:\d{2}(:\d{2})?$`)
	// A topic of the notes is its number and title in bold, and its
	// bullets follow, indented.
	topicTitle = regexp.MustCompile(`^\*\*\d+\. (.+)\*\*$`)
	// An action item says where it stands on the board first.
	standing = regexp.MustCompile(`^\[(open|open · on hold|done by [^\]]*|canceled by [^\]]*)\] `)
	// A passage of a transcript is its meeting's date and title, its
	// moment, and what was said in quotation marks.
	passage = regexp.MustCompile(`^- (\d{4}-\d{2}-\d{2}) (.+), \[([^\]]+)\]\(([^)\s]+)\): "(.*)"$`)
	// A notebook entry's line under its title: who wrote it, the date,
	// its tags and its link, in italics.
	entryLine = regexp.MustCompile(`^_.*?(\d{4}-\d{2}-\d{2}).*?(https?://\S+?)_$`)
	// A link in markdown, whose words are kept and whose target isn't.
	markdownLink = regexp.MustCompile(`\[([^\]]*)\]\([^)\s]*\)`)
	bareLink     = regexp.MustCompile(`https?://\S+`)
)

// points reads the lines of a brief that come from meetings: each bullet
// of a meeting's notes, with the moment it cites, or its topic's when it
// cites none, or else the meeting, and where it stands on the board when
// it is an action item; each passage of the transcripts; and each line
// the brief quotes of a notebook entry, marked as such, with the entry's
// link, and the words of any link in it without where it leads. What the
// board says about an action item, the board's other items and documents
// are left out: none is a moment in a meeting.
func points(brief string) []live.Point {
	var out []live.Point
	var section, date, title, link, heading string
	var topic *live.Point // the topic the indented bullets belong to
	action := false       // the indented lines are what the board says about an action item
	for _, l := range strings.Split(brief, "\n") {
		l = strings.TrimRight(l, " \r")
		switch {
		case strings.HasPrefix(l, "## "):
			section, heading, topic, action, link = "other", "", nil, false, ""
			if m := meetingHeading.FindStringSubmatch(l); m != nil {
				section, date, title = "meeting", m[1], m[2]
			} else if strings.HasPrefix(l, "## What was said") {
				section = "said"
			} else if strings.HasPrefix(l, "## Notebook") {
				section, title, date = "notebook", "", ""
			}
		case section == "notebook":
			if p, ok := entry(l, &title, &date, &link); ok {
				out = append(out, p)
			}
		case section == "said":
			if m := passage.FindStringSubmatch(l); m != nil {
				out = append(out, live.Point{Text: m[5], Meeting: m[2], Date: m[1], At: m[3], URL: m[4], Said: true})
			}
		case section != "meeting" || l == "":
		case link == "" && (strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")):
			link = l
		case strings.HasPrefix(l, "### "):
			heading, topic, action = strings.TrimPrefix(l, "### "), nil, false
		case strings.HasPrefix(l, "  - "):
			if action {
				continue
			}
			p := live.Point{Text: strings.TrimPrefix(l, "  - "), Under: heading, Meeting: title, Date: date, URL: link}
			if topic != nil {
				p.Under, p.At, p.URL = strings.TrimPrefix(topic.Text+" · "+heading, " · "), topic.At, topic.URL
			}
			out = append(out, p)
		default:
			text, at, url := cut(l)
			if url == "" {
				url = link
			}
			if m := topicTitle.FindStringSubmatch(text); m != nil {
				topic, action = &live.Point{Text: m[1], At: at, URL: url}, false
				continue
			}
			text, isBullet := strings.CutPrefix(text, "- ")
			if !isBullet {
				continue
			}
			topic = nil
			var standing string
			text, standing = trimStanding(text)
			action = standing != ""
			out = append(out, live.Point{Text: text, Under: heading, Meeting: title, Date: date, At: at, URL: url, Standing: standing})
		}
	}
	return out
}

// cut takes a line's citation off, and returns the moment it cites, such
// as "6:12", and that moment's link. An action item's board link is no
// moment.
func cut(l string) (text, at, url string) {
	loc := citation.FindStringIndex(l)
	if loc == nil {
		return l, "", ""
	}
	for _, m := range cited.FindAllStringSubmatch(l[loc[0]:], -1) {
		if moment.MatchString(m[1]) {
			return l[:loc[0]], m[1], m[2]
		}
	}
	return l[:loc[0]], "", ""
}

// trimStanding takes where an action item stands on the board off its
// line, and says where it stands: "open", "on hold", "done" or
// "canceled", or nothing when it isn't one.
func trimStanding(text string) (string, string) {
	m := standing.FindStringSubmatch(text)
	if m == nil {
		return text, ""
	}
	where := m[1]
	switch {
	case strings.HasPrefix(where, "done"):
		where = "done"
	case strings.HasPrefix(where, "canceled"):
		where = "canceled"
	case strings.Contains(where, "on hold"):
		where = "on hold"
	}
	return text[len(m[0]):], where
}

// entry reads a line of the brief's notebook: an entry's title, the line
// under it that dates and links it, or a line it quotes, which it returns
// as a point of that entry. A quoted heading, an empty quote and anything
// before the entry's link is known are left out.
func entry(l string, title, date, link *string) (live.Point, bool) {
	switch {
	case strings.HasPrefix(l, "### "):
		*title, *date, *link = strings.TrimPrefix(l, "### "), "", ""
	case entryLine.MatchString(l):
		m := entryLine.FindStringSubmatch(l)
		*date, *link = m[1], m[2]
	case strings.HasPrefix(l, ">") && *link != "":
		text := strings.TrimSpace(strings.TrimPrefix(l, ">"))
		if strings.HasPrefix(text, "#") {
			return live.Point{}, false
		}
		text = strings.TrimSpace(strings.TrimPrefix(text, "- "))
		text = markdownLink.ReplaceAllString(text, "$1")
		text = bareLink.ReplaceAllString(text, "")
		text = strings.Join(strings.Fields(strings.ReplaceAll(text, "**", "")), " ")
		if len([]rune(text)) < 12 {
			return live.Point{}, false
		}
		return live.Point{Text: text, Meeting: *title, Date: *date, URL: *link, Under: "notebook", Notebook: true}, true
	}
	return live.Point{}, false
}
