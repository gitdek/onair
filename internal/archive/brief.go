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
)

// points reads the lines of a brief that come from meetings: each bullet
// of a meeting's notes, with the moment it cites, or its topic's when it
// cites none, or else the meeting; and each passage of the transcripts.
// What the board says about an action item, the board's other items,
// documents and the notebook are left out: none is a moment in a meeting.
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
			text, action = trimStanding(text)
			out = append(out, live.Point{Text: text, Under: heading, Meeting: title, Date: date, At: at, URL: url})
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
// line, and says whether it was one.
func trimStanding(text string) (string, bool) {
	if loc := standing.FindStringIndex(text); loc != nil {
		return text[loc[1]:], true
	}
	return text, false
}
