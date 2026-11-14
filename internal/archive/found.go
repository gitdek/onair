package archive

import (
	"encoding/json"
	"strconv"

	"github.com/gitdek/onair/internal/live"
)

// found is find_connections' answer, as On the Record's read-only door
// gives it in its structured content. Only what the panel needs is read.
type found struct {
	Semantic bool `json:"semantic"`
	Meetings []struct {
		ID       text   `json:"id"`
		Title    text   `json:"title"`
		Date     text   `json:"date"`
		Why      []text `json:"why"`
		Link     text   `json:"link"`
		Passages []struct {
			Text text `json:"text"`
			At   text `json:"at"`
			Link text `json:"link"`
		} `json:"passages"`
	} `json:"meetings"`
	Notebook []struct {
		ID      text   `json:"id"`
		Title   text   `json:"title"`
		Updated text   `json:"updated"`
		Link    text   `json:"link"`
		Why     []text `json:"why"`
	} `json:"notebook"`
	ActionItems []struct {
		ID      text `json:"id"`
		Text    text `json:"text"`
		Status  text `json:"status"`
		Owner   text `json:"owner"`
		Link    text `json:"link"`
		Meeting *struct {
			Title text `json:"title"`
			Date  text `json:"date"`
		} `json:"meeting"`
	} `json:"action_items"`
	Suggested struct {
		Meetings []text `json:"meeting_ids"`
		Items    []text `json:"task_ids"`
	} `json:"suggested_links"`
}

func (f found) live() live.Found {
	out := live.Found{Semantic: f.Semantic, StrongMeetings: texts(f.Suggested.Meetings), StrongItems: texts(f.Suggested.Items)}
	for _, m := range f.Meetings {
		meeting := live.Meeting{ID: string(m.ID), Title: string(m.Title), Date: string(m.Date), URL: string(m.Link), Why: texts(m.Why)}
		for _, p := range m.Passages {
			meeting.Passages = append(meeting.Passages, live.Passage{Text: string(p.Text), At: string(p.At), URL: string(p.Link)})
		}
		out.Meetings = append(out.Meetings, meeting)
	}
	for _, n := range f.Notebook {
		out.Notes = append(out.Notes, live.Note{ID: string(n.ID), Title: string(n.Title), Updated: string(n.Updated), URL: string(n.Link), Why: texts(n.Why)})
	}
	for _, a := range f.ActionItems {
		item := live.Item{ID: string(a.ID), Text: string(a.Text), Status: string(a.Status), Owner: string(a.Owner), URL: string(a.Link)}
		if a.Meeting != nil {
			item.Meeting, item.MeetingDate = string(a.Meeting.Title), string(a.Meeting.Date)
		}
		out.Items = append(out.Items, item)
	}
	return out
}

func texts(ts []text) []string {
	var out []string
	for _, t := range ts {
		out = append(out, string(t))
	}
	return out
}

// text reads a string, and a number or a true or false as its digits or
// words, so that a field whose kind changes doesn't lose the whole answer.
type text string

func (t *text) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*t = text(s)
		return nil
	}
	var n json.Number
	if json.Unmarshal(b, &n) == nil {
		*t = text(n.String())
		return nil
	}
	var v bool
	if json.Unmarshal(b, &v) == nil {
		*t = text(strconv.FormatBool(v))
		return nil
	}
	*t = ""
	return nil
}
