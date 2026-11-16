package archive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gitdek/onair/internal/live"
)

// madeUpBrief is gather_context's brief, in the shape On the Record writes
// it, learned from its code, with made-up meetings: a topic with its
// bullets, a bullet with its moment, an action item with what the board
// says about it, a bullet with no moment, the board's other items, a
// passage of a transcript, a document and a notebook entry.
const madeUpBrief = `# Context: how much cash does Halcyon need

_4 passages from 2 meetings and 1 notebook entry, from the notes. Action items show where they stand on the board today. Follow the links (or get_transcript) for exact wording._

## 2026-09-14 · Halcyon partner sync
http://127.0.0.1:8797/m/m-1

### Discussion
**2. Investors** ([6:12](http://127.0.0.1:8797/m/m-1?t=372))
  - Halcyon hopes to close a $750k pre-seed round
  - The round should close before winter
- [open] Priya to share the model with the partners ([9:50](http://127.0.0.1:8797/m/m-1?t=590), [board item](http://127.0.0.1:8797/actions?show=all&focus=t-1))
  - note: waiting on the forecast (Someone)

### Summary
- The sync was short

## 2026-10-08 · Halcyon product walkthrough (demo)
http://127.0.0.1:8797/m/m-2

### Release timeline
- Doors open to the public on the 9th ([1:05:09](http://127.0.0.1:8797/m/m-2?t=3909))

## Also on the board
http://127.0.0.1:8797/actions?show=all

- [open] Priya: Book the venue ([id t-2](http://127.0.0.1:8797/actions?show=all&focus=t-2), tied to 2026-10-08 Halcyon product walkthrough)

## What was said (closest transcript passages)
- 2026-10-08 Halcyon product walkthrough, [52:10](http://127.0.0.1:8797/m/m-2?t=3130.4&tab=transcript&transcript=x): "we would rather have it out before the long weekend"

## Attached documents
- [Halcyon plan.pdf](http://127.0.0.1:8797/m/m-1?tab=excerpt&file=f-1):
  > A pre-seed round

## Notebook

### Halcyon research
_Someone, 2026-09-20 · investors · http://127.0.0.1:8797/notebook/n-1_
> Halcyon's round
`

func TestItReadsTheLinesOfABriefThatComeFromMeetings(t *testing.T) {
	m1, m2 := "http://127.0.0.1:8797/m/m-1", "http://127.0.0.1:8797/m/m-2"
	want := []live.Point{
		{Text: "Halcyon hopes to close a $750k pre-seed round", Under: "Investors · Discussion", Meeting: "Halcyon partner sync", Date: "2026-09-14", At: "6:12", URL: m1 + "?t=372"},
		{Text: "The round should close before winter", Under: "Investors · Discussion", Meeting: "Halcyon partner sync", Date: "2026-09-14", At: "6:12", URL: m1 + "?t=372"},
		{Text: "Priya to share the model with the partners", Under: "Discussion", Meeting: "Halcyon partner sync", Date: "2026-09-14", At: "9:50", URL: m1 + "?t=590"},
		{Text: "The sync was short", Under: "Summary", Meeting: "Halcyon partner sync", Date: "2026-09-14", URL: m1},
		{Text: "Doors open to the public on the 9th", Under: "Release timeline", Meeting: "Halcyon product walkthrough (demo)", Date: "2026-10-08", At: "1:05:09", URL: m2 + "?t=3909"},
		{Text: "we would rather have it out before the long weekend", Meeting: "Halcyon product walkthrough", Date: "2026-10-08", At: "52:10", URL: m2 + "?t=3130.4&tab=transcript&transcript=x", Said: true},
	}
	if got := points(madeUpBrief); !reflect.DeepEqual(got, want) {
		t.Fatalf("read\n%+v\nnot\n%+v", got, want)
	}
	if got := points("No meetings here.\n## Notebook\n- a line"); len(got) != 0 {
		t.Fatalf("read %+v from a brief with no meetings", got)
	}
}

func TestItAsksGatherContextInPlainWordsAndNeverForTheScreen(t *testing.T) {
	d := &door{session: "session-1"}
	c, _ := open(t, d)
	ctx, cancel := contextWithin(time.Second)
	defer cancel()
	b, err := c.Answer(ctx, `how much cash does "Halcyon" need in:screen person:priya @priya -round on: 2026?`, false)
	if err != nil {
		t.Fatal(err)
	}
	if b.Passages != 4 || len(b.Points) != 6 || b.Points[0].At != "6:12" {
		t.Fatalf("read %+v", b)
	}
	d.mu.Lock()
	args := d.gathered[0]
	d.mu.Unlock()
	want := map[string]any{"topic": "how much cash does Halcyon need in screen person priya priya round on 2026?", "max_items": float64(12), "include_transcript": false, "include_screen": false}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("asked %v", args)
	}
	if _, err := c.Answer(ctx, "what was said", true); err != nil || d.gathered[1]["include_transcript"] != true || d.gathered[1]["include_screen"] != false {
		t.Fatalf("asked %v: %v", d.gathered[1], err)
	}
	if long := topic(strings.Repeat("é", 3000)); len([]rune(long)) != maxTopic {
		t.Fatalf("a long topic is %d characters", len([]rune(long)))
	}
}

func TestABriefThatGoesWrongComesBackAsAnError(t *testing.T) {
	for name, tc := range map[string]struct {
		result string
		says   string
	}{
		"a tool error": {`{"content":[{"type":"text","text":"topic is required"}],"isError":true}`, "On the Record couldn't answer: topic is required"},
		"no brief":     {`{"content":[]}`, "On the Record's answer had no brief"},
		"nonsense":     {`"nonsense"`, "On the Record's answer couldn't be read"},
	} {
		t.Run(name, func(t *testing.T) {
			d := &door{brief: func(w http.ResponseWriter, id json.RawMessage) { reply(w, id, tc.result) }}
			c, _ := open(t, d)
			ctx, cancel := contextWithin(time.Second)
			defer cancel()
			if _, err := c.Answer(ctx, "how much", false); err == nil || err.Error() != tc.says {
				t.Fatalf("said %v, not %q", err, tc.says)
			}
		})
	}
	// A brief whose counts can't be read is still read.
	d := &door{brief: func(w http.ResponseWriter, id json.RawMessage) {
		reply(w, id, fmt.Sprintf(`{"content":[{"type":"text","text":%q},{"type":"text","text":"not json"}]}`, madeUpBrief))
	}}
	c, _ := open(t, d)
	ctx, cancel := contextWithin(time.Second)
	defer cancel()
	if b, err := c.Answer(ctx, "how much", false); err != nil || len(b.Points) != 6 || b.Passages != 0 {
		t.Fatalf("read %+v, %v", b, err)
	}
}

func contextWithin(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
