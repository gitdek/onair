package live

import (
	"context"
	"fmt"
	"time"
)

// Judge answers typed questions about the newest minute of a call, with
// the records the panel fetched from On the Record beside it. A model
// stands behind it, and gives probabilities, never prose.
type Judge interface {
	Judge(ctx context.Context, r Round) (Verdict, error)
	// Close stops whatever runs the model, when it isn't wanted.
	Close()
}

// Round is what a judge is asked about.
type Round struct {
	Text    string // the newest minute, in the order it was said
	Records []Link // what the panel shows from On the Record, newest first
}

// Verdict is a judge's answer to a round.
type Verdict struct {
	Decision float64 // that a decision is being made
	// Stance is where that decision stands: "proposed", "agreed", "taken
	// back" or "none".
	Stance map[string]float64
	Topic  float64 // that the topic changed
}

// Thresholds are what a verdict must clear to show in the panel. They are
// first guesses, to be tuned on the owner's own meetings, as docs/PRD.md
// says thresholds must be.
var Thresholds = struct {
	Decision float64 // that a decision is being made
	Stance   float64 // that it stands where the panel says
	Topic    float64 // that the topic changed, which clears the panel
}{Decision: 0.7, Stance: 0.5, Topic: 0.8}

// Stances a decision can be in, as the panel says them.
var stances = map[string]string{"proposed": "proposed", "agreed": "agreed", "taken back": "taken back"}

// call is what a judge said that cleared a threshold, as the panel shows
// it.
type call struct {
	kind string // "decision"
	text string
	at   time.Time
}

// verdictCalls is what a verdict says that clears the thresholds. Whether
// what is said differs from the record is the Writer's to check, with the
// record's own words, not a judge's.
func verdictCalls(v Verdict, now time.Time) []call {
	var calls []call
	if v.Decision >= Thresholds.Decision {
		best, p := "", 0.0
		for stance, q := range v.Stance {
			if _, ok := stances[stance]; ok && (q > p || q == p && stance < best) {
				best, p = stance, q
			}
		}
		if best != "" && p >= Thresholds.Stance {
			calls = append(calls, call{kind: "decision", text: fmt.Sprintf("Decision: %s, at %s.", stances[best], now.Format("15:04")), at: now})
		}
	}
	return calls
}
