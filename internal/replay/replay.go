// Package replay plays what was said in a meeting through the live side,
// as speech recognition would hand it over, on a clock that runs faster
// than the real one, and keeps what the panel showed and when. The archive
// and the local models it asks are real. What they answered is kept with
// how long they took, so that a run asks each question once, with the
// clock held while it waits, and a later run asks nothing and still takes
// as long by its clock. It is for tuning the live side on a meeting its
// owner chose: what it keeps is that meeting's, in the folder it is given.
package replay

import (
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/gitdek/onair/internal/live"
)

// Word is a word said, and when, from the start of the meeting.
type Word struct {
	Text     string
	From, To time.Duration
}

// ReadWhisper reads the words of a transcript in the shape Whisper writes
// it with word times: segments, each with its words, and each word's start
// and end in seconds.
func ReadWhisper(r io.Reader) ([]Word, error) {
	var t struct {
		Segments []struct {
			Words []struct {
				Word  string  `json:"word"`
				Start float64 `json:"start"`
				End   float64 `json:"end"`
			} `json:"words"`
		} `json:"segments"`
	}
	if err := json.NewDecoder(r).Decode(&t); err != nil {
		return nil, errors.New("the transcript isn't Whisper's JSON with word times")
	}
	var out []Word
	for _, s := range t.Segments {
		for _, w := range s.Words {
			text := strings.TrimSpace(w.Word)
			if text == "" || w.Start < 0 || w.End > 24*3600 {
				continue
			}
			from := time.Duration(w.Start * float64(time.Second))
			to := max(time.Duration(w.End*float64(time.Second)), from)
			out = append(out, Word{Text: text, From: from, To: to})
		}
	}
	slices.SortStableFunc(out, func(a, b Word) int { return int(a.From - b.From) })
	if len(out) == 0 {
		return nil, errors.New("the transcript has no words with times")
	}
	return out, nil
}

// Recognition is how speech recognition hands over what it hears: a
// request for every so much sound, partial results as it goes, each with
// all of its request's words so far, and a final result with every word's
// time once its sound has ended.
type Recognition struct {
	Request time.Duration // how much sound a request hears
	Every   time.Duration // how often a partial result comes, with new words or with the sound gone further
	Behind  time.Duration // how far behind the sound a partial result's words come
	Final   time.Duration // how long after its sound ends a request's final result comes
	Timed   bool          // whether partial results give their words' times
	Lose    float64       // the share of words never heard, lost in runs of a few words
	Seed    uint64        // which runs are lost
}

// Measured is recognition as On Air's log of a real call measured it on
// the owner's Mac: a request every 15 seconds of sound; partial results
// four times a second, almost none of them with word times, their newest
// words about 1.9 seconds behind the sound; and each final result 0.15
// seconds after its sound ended, with times. That call's recognition
// heard about 63 words of every hundred Whisper heard in its recording.
var Measured = Recognition{Request: 15 * time.Second, Every: 250 * time.Millisecond, Behind: 1900 * time.Millisecond, Final: 150 * time.Millisecond, Lose: 0.35, Seed: 1}

// Event is a result of recognition, and when it comes, from the start of
// the meeting.
type Event struct {
	At   time.Duration
	Said live.Said
}

// step is how long each step of loudness lasts. A replay has no
// loudness, so every word is everyone else's.
const step = 100 * time.Millisecond

// Events is what recognition hands over of words said from start, in the
// order it comes.
func (r Recognition) Events(words []Word, start time.Time) []Event {
	heard := r.heard(words)
	if len(words) == 0 || r.Request <= 0 || r.Every <= 0 {
		return nil
	}
	end := words[len(words)-1].To + time.Second
	var out []Event
	for k := 0; time.Duration(k)*r.Request < end; k++ {
		from, to := time.Duration(k)*r.Request, time.Duration(k+1)*r.Request
		var mine []Word
		for _, w := range heard {
			if w.From >= from && w.From < to {
				mine = append(mine, w)
			}
		}
		said := func(partial bool, extent time.Duration, ws []Word) live.Said {
			s := live.Said{Began: start.Add(from), Partial: partial, Request: k + 1, Step: step,
				Mic: make([]float64, extent/step), Call: make([]float64, extent/step)}
			for _, w := range ws {
				seg := live.Segment{Text: w.Text}
				if !partial || r.Timed {
					seg.At, seg.Length = w.From-from, w.To-w.From
				}
				s.Segments = append(s.Segments, seg)
			}
			return s
		}
		for at := from + r.Every; at < to && at < end; at += r.Every {
			var so []Word
			for _, w := range mine {
				if w.To <= at-r.Behind {
					so = append(so, w)
				}
			}
			out = append(out, Event{At: at, Said: said(true, at-from, so)})
		}
		out = append(out, Event{At: min(to, end) + r.Final, Said: said(false, min(to, end)-from, mine)})
	}
	return out
}

// heard is the words recognition hears: all but r.Lose of them, lost in
// runs of six, which runs chosen by r.Seed.
func (r Recognition) heard(words []Word) []Word {
	if r.Lose <= 0 {
		return words
	}
	var out []Word
	x := r.Seed*0x9E3779B97F4A7C15 + 1
	for i := 0; i < len(words); i += 6 {
		// A step of xorshift for each run.
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		if float64(x%10000)/10000 < r.Lose {
			continue
		}
		out = append(out, words[i:min(i+6, len(words))]...)
	}
	return out
}
