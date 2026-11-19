package archive

import (
	"context"

	"github.com/gitdek/onair/internal/live"
)

// Library asks On the Record's lists what it holds: list_meetings,
// list_action_items for the open items, and list_notes. Each only reads.
// Of an item, it reads what it says, whose it is, where it stands and the
// meeting it came from, and never the evidence or sources behind it, which
// may point at a person's mail.
func (c *Client) Library(ctx context.Context) (live.Library, error) {
	var out live.Library
	var meetings struct {
		Meetings []struct {
			ID           text   `json:"id"`
			Title        text   `json:"title"`
			Date         text   `json:"date"`
			Link         text   `json:"link"`
			Summary      text   `json:"summary"`
			Participants []text `json:"participants"`
		} `json:"meetings"`
	}
	if err := c.list(ctx, "list_meetings", map[string]any{}, &meetings); err != nil {
		return live.Library{}, err
	}
	for _, m := range meetings.Meetings {
		out.Meetings = append(out.Meetings, live.Meeting{ID: string(m.ID), Title: string(m.Title), Date: string(m.Date), URL: string(m.Link),
			Summary: string(m.Summary), People: texts(m.Participants)})
	}
	var items struct {
		Items []struct {
			ID      text `json:"id"`
			Text    text `json:"text"`
			Status  text `json:"status"`
			Owner   text `json:"owner"`
			Lane    text `json:"lane"`
			Link    text `json:"link"`
			Meeting *struct {
				Title text `json:"title"`
				Date  text `json:"date"`
			} `json:"meeting"`
		} `json:"items"`
	}
	if err := c.list(ctx, "list_action_items", map[string]any{"status": "open"}, &items); err != nil {
		return live.Library{}, err
	}
	for _, it := range items.Items {
		item := live.Item{ID: string(it.ID), Text: string(it.Text), Status: string(it.Status), Owner: string(it.Owner), URL: string(it.Link), Ours: it.Lane == "us"}
		if it.Meeting != nil {
			item.Meeting, item.MeetingDate = string(it.Meeting.Title), string(it.Meeting.Date)
		}
		out.Items = append(out.Items, item)
	}
	var notes struct {
		Notes []struct {
			ID      text   `json:"id"`
			Title   text   `json:"title"`
			Updated text   `json:"updated"`
			Link    text   `json:"link"`
			Tags    []text `json:"tags"`
		} `json:"notes"`
	}
	if err := c.list(ctx, "list_notes", map[string]any{"limit": 100}, &notes); err != nil {
		return live.Library{}, err
	}
	for _, n := range notes.Notes {
		out.Notes = append(out.Notes, live.Note{ID: string(n.ID), Title: string(n.Title), Updated: string(n.Updated), URL: string(n.Link), Tags: texts(n.Tags)})
	}
	return out, nil
}

// list calls one of the archive's lists and reads its answer into out.
func (c *Client) list(ctx context.Context, tool string, args map[string]any, out any) error {
	result, err := c.call(ctx, tool, args)
	if err != nil {
		return err
	}
	return decode(result, out)
}
