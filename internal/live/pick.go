package live

import (
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"
)

// pick chooses, from what On the Record answered, the links worth
// showing, at most n: the earlier meetings and open action items that the
// archive itself suggests linking, which are those nearly as strong as
// its best match, and the first notebook entry it matched by meaning,
// which passed the archive's own cut for that. The two strongest meetings
// come first, then an open item, then the notebook entry, then any other
// meeting. A link that a browser can't open is left out, and so is
// everything else the archive returned.
func pick(f Found, n int) []Link {
	var meetings, items, notes []Link
	for _, m := range f.Meetings {
		if m.ID == "" || !slices.Contains(f.StrongMeetings, m.ID) {
			continue
		}
		l := Link{Kind: MeetingLink, ID: m.ID, Title: line(m.Title, 90), Date: m.Date, URL: m.URL, Why: reasons(m.Why)}
		if len(m.Passages) > 0 {
			p := m.Passages[0]
			l.Why = line(strings.TrimSpace(p.At+" "+quote(p.Text)), 160)
			if openable(p.URL) {
				l.URL = p.URL
			}
		}
		if openable(l.URL) {
			meetings = append(meetings, l)
		}
	}
	for _, it := range f.Items {
		if it.ID == "" || !slices.Contains(f.StrongItems, it.ID) {
			continue
		}
		status := map[string]string{"open": "Open", "on_hold": "On hold"}[it.Status]
		if status == "" || !openable(it.URL) {
			continue
		}
		why := status
		if owner := strings.TrimSpace(it.Owner); owner != "" && owner != "unassigned" {
			why += ", " + owner + "'s"
		} else {
			why += ", with no owner"
		}
		if it.Meeting != "" {
			why += ", from " + it.Meeting
		}
		items = append(items, Link{Kind: ItemLink, ID: it.ID, Title: line(it.Text, 90), Date: it.MeetingDate, Why: line(why, 160), URL: it.URL})
	}
	for _, note := range f.Notes {
		if note.ID == "" || !openable(note.URL) || !byMeaning(note.Why) {
			continue
		}
		date := note.Updated
		if len(date) > 10 && date[4] == '-' && date[7] == '-' {
			date = date[:10]
		}
		notes = append(notes, Link{Kind: NoteLink, ID: note.ID, Title: line(note.Title, 90), Date: date, Why: reasons(note.Why), URL: note.URL})
		break
	}
	var links []Link
	links = append(links, meetings[:min(2, len(meetings))]...)
	links = append(links, items[:min(1, len(items))]...)
	links = append(links, notes...)
	links = append(links, meetings[min(2, len(meetings)):]...)
	if len(links) > n {
		links = links[:n]
	}
	return links
}

// byMeaning says whether the archive matched something by meaning, which
// it does only past its own threshold.
func byMeaning(why []string) bool {
	for _, w := range why {
		if strings.HasPrefix(w, "close in meaning") {
			return true
		}
	}
	return false
}

// reasons says the archive's reasons in a line: words it shares, and
// whether it is close in meaning. How it ranked is left out.
func reasons(why []string) string {
	var said []string
	meaning := false
	for _, w := range why {
		switch {
		case strings.HasPrefix(w, "shares "):
			said = append(said, "Shares "+strings.TrimPrefix(w, "shares "))
		case strings.HasPrefix(w, "close in meaning"):
			meaning = true
		}
	}
	if meaning {
		said = append([]string{"Close in meaning"}, said...)
	}
	if len(said) > 2 {
		said = said[:2]
	}
	return line(strings.Join(said, "; "), 160)
}

// openable says whether a browser can open a link: http or https, to a
// host, with no name or password in it.
func openable(link string) bool {
	u, err := url.Parse(link)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

// quote puts what was said in quotation marks.
func quote(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return "“" + s + "”"
}

// line is s on one line, at most n characters, cut at a word with an
// ellipsis when it is longer.
func line(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	cut := string(r[:n-1])
	// Back to the last whole word, unless the cut falls between two.
	if i := strings.LastIndex(cut, " "); r[n-1] != ' ' && i > n/2 {
		cut = cut[:i]
	}
	cut = strings.TrimRight(cut, " ,;:.“")
	if strings.Count(cut, "“") > strings.Count(cut, "”") {
		return cut + "…”"
	}
	return cut + "…"
}
