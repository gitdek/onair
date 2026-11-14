package localmodel

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/gitdek/onair/internal/live"
)

// questions are the typed questions a round asks, in the shape d1 takes:
// whether a decision is being made, where it stands, whether the topic
// changed, and, when the panel shows a record, how far what is said
// differs from it.
func questions(r live.Round) map[string]any {
	q := map[string]any{
		"decision": map[string]any{"type": "noul", "instructions": "Is a decision being made in this passage?"},
		"stance": map[string]any{"type": "choice", "instructions": "Where does the decision being discussed stand by the end of the passage?",
			"criteria": map[string]string{
				"proposed":   "someone has proposed it, and it isn't agreed yet",
				"agreed":     "the people speaking have agreed on it",
				"taken back": "a decision made earlier is being taken back or reversed",
				"none":       "no decision is being discussed",
			}},
		"topic_changed": map[string]any{"type": "noul", "instructions": "Has the topic changed by the end of the passage?"},
	}
	if len(r.Records) > 0 {
		record := r.Records[0]
		q["differs"] = map[string]any{"type": "score",
			"instructions": fmt.Sprintf("How far does what is said in the passage differ from this earlier record? The record: %s, %s. %s", record.Title, record.Date, record.Why),
			"criteria":     []string{"it agrees with the record, or doesn't touch it", "it differs from the record in part", "it contradicts the record"}}
	}
	return q
}

// verdict reads d1's answers: a yes-or-no question's probability of yes,
// a choice's probabilities, and a score's level, from 0 to 2, as 0 to 1.
func verdict(answers map[string]json.RawMessage, compared bool) (live.Verdict, error) {
	var decision, topic struct {
		Noul *float64 `json:"noul"`
	}
	var stance struct {
		Probabilities map[string]float64 `json:"probabilities"`
	}
	var differs struct {
		Score *float64 `json:"score"`
	}
	bad := errors.New("the local model's answer wasn't in the shape On Air reads")
	if json.Unmarshal(answers["decision"], &decision) != nil || decision.Noul == nil ||
		json.Unmarshal(answers["topic_changed"], &topic) != nil || topic.Noul == nil ||
		json.Unmarshal(answers["stance"], &stance) != nil || len(stance.Probabilities) == 0 {
		return live.Verdict{}, bad
	}
	v := live.Verdict{Decision: unit(*decision.Noul), Topic: unit(*topic.Noul), Stance: map[string]float64{}}
	for k, p := range stance.Probabilities {
		v.Stance[k] = unit(p)
	}
	if compared {
		if json.Unmarshal(answers["differs"], &differs) != nil || differs.Score == nil {
			return live.Verdict{}, bad
		}
		v.Differs, v.Compared = unit(*differs.Score/2), true
	}
	return v, nil
}

// unit keeps a probability between 0 and 1, and takes one that isn't a
// number as 0.
func unit(p float64) float64 {
	if math.IsNaN(p) {
		return 0
	}
	return math.Max(0, math.Min(1, p))
}
