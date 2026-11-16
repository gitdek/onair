package localmodel

import (
	"encoding/json"
	"math"

	"github.com/gitdek/onair/internal/live"
)

// questions are the typed questions a round asks, in the shape d1 takes:
// whether a decision is being made, where it stands, and whether the topic
// changed. Whether what is said differs from the record is the writing
// model's to check, with the record's own words.
func questions(live.Round) map[string]any {
	return map[string]any{
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
}

// verdict reads d1's answers: a yes-or-no question's probability of yes,
// and a choice's probabilities.
func verdict(answers map[string]json.RawMessage) (live.Verdict, error) {
	var decision, topic struct {
		Noul *float64 `json:"noul"`
	}
	var stance struct {
		Probabilities map[string]float64 `json:"probabilities"`
	}
	if json.Unmarshal(answers["decision"], &decision) != nil || decision.Noul == nil ||
		json.Unmarshal(answers["topic_changed"], &topic) != nil || topic.Noul == nil ||
		json.Unmarshal(answers["stance"], &stance) != nil || len(stance.Probabilities) == 0 {
		return live.Verdict{}, errShape
	}
	v := live.Verdict{Decision: unit(*decision.Noul), Topic: unit(*topic.Noul), Stance: map[string]float64{}}
	for k, p := range stance.Probabilities {
		v.Stance[k] = unit(p)
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
