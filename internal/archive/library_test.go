package archive

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gitdek/onair/internal/live"
)

// The archive's lists, in the shapes On the Record's read-only door gives
// them, with made-up meetings, items and entries. An item's evidence and
// sources are there to be left alone.
var madeUpLists = map[string]string{
	"list_meetings": `{"meetings": [
  {"id": "m-2", "title": "Halcyon product walkthrough", "kind": "demo", "date": "2026-10-08", "minutes": 40,
   "participants": ["Priya Shah", "Tomas Weber"], "summary": "Release timeline, mockups.", "has_notes": true, "has_transcript": true,
   "has_screens": false, "open_action_items": 1, "link": "http://127.0.0.1:8797/m/m-2"}]}`,
	"list_action_items": `{"count": 2, "board": {"open": 2}, "items": [
  {"id": "t-1", "rev": 2, "owner": "Priya", "lane": "them", "text": "Send the press kit", "status": "open",
   "evidence": "a private message", "sources": [{"ref": "somewhere private"}], "note": "private",
   "meeting": {"id": "m-2", "title": "Halcyon product walkthrough", "date": "2026-10-08"}, "said_at": "1:06:30",
   "link": "http://127.0.0.1:8797/m/m-2?t=3990"},
  {"id": "t-2", "rev": 1, "owner": "Us", "lane": "us", "text": "Book the venue", "status": "open", "link": "http://127.0.0.1:8797/actions?focus=t-2"}]}`,
	"list_notes": `{"count": 1, "notes": [
  {"id": "n-1", "title": "Halcyon research", "author": "Someone", "excerpt": "Made up.", "link": "http://127.0.0.1:8797/notebook/n-1",
   "meetings": [{"id": "m-2", "title": "Halcyon product walkthrough"}], "tags": ["halcyon", "research"], "updated": "2026-10-09T10:00:00.000Z"}]}`,
}

func TestTheLibraryIsReadFromTheArchivesLists(t *testing.T) {
	d := &door{lists: madeUpLists}
	c, _ := open(t, d)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l, err := c.Library(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := live.Library{
		Meetings: []live.Meeting{{ID: "m-2", Title: "Halcyon product walkthrough", Date: "2026-10-08", URL: "http://127.0.0.1:8797/m/m-2",
			Summary: "Release timeline, mockups.", People: []string{"Priya Shah", "Tomas Weber"}}},
		Items: []live.Item{
			{ID: "t-1", Text: "Send the press kit", Status: "open", Owner: "Priya", URL: "http://127.0.0.1:8797/m/m-2?t=3990", Meeting: "Halcyon product walkthrough", MeetingDate: "2026-10-08"},
			{ID: "t-2", Text: "Book the venue", Status: "open", Owner: "Us", URL: "http://127.0.0.1:8797/actions?focus=t-2", Ours: true},
		},
		Notes: []live.Note{{ID: "n-1", Title: "Halcyon research", Updated: "2026-10-09T10:00:00.000Z", URL: "http://127.0.0.1:8797/notebook/n-1", Tags: []string{"halcyon", "research"}}},
	}
	if !reflect.DeepEqual(l, want) {
		t.Errorf("read\n%+v\nnot\n%+v", l, want)
	}
	methods, _ := d.asked()
	if got := strings.Join(methods, ", "); got != "initialize , notifications/initialized , tools/call list_meetings, tools/call list_action_items, tools/call list_notes" {
		t.Errorf("asked %s", got)
	}
	if d.listed[1]["status"] != "open" {
		t.Errorf("asked the board for %v", d.listed[1])
	}
	// A list the door doesn't give is an error, not an empty library.
	delete(d.lists, "list_notes")
	if _, err := c.Library(ctx); err == nil {
		t.Error("a missing list wasn't an error")
	}
}
