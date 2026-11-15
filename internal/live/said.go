package live

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Said is a final result of speech recognition, which hears the call's
// sound and the microphone added into one: its segments, a word or so each,
// when its sound began, and how loud each of the two sounds was through it,
// a step at a time. The Mac measures, and whose each word was is decided
// here.
type Said struct {
	Began    time.Time
	Segments []Segment
	// Step is how long each step of loudness lasts. The first starts at
	// Began.
	Step time.Duration
	// Mic and Call are each sound's loudness in each step: its RMS, from 0
	// to 1 of full scale, measured before the two were added.
	Mic, Call []float64
}

// Segment is a word or so of a result, and when it was said, from the start
// of the result's sound.
type Segment struct {
	Text       string
	At, Length time.Duration
}

// Louder is how many times louder than the call's sound the microphone must
// be, while a word is said, for the word to be the person's: twice, which is
// 6 dB. It is a first guess, to be tuned on the owner's own calls.
const Louder = 2.0

// whose says whose the sound from..to of a result was: the person's when
// the microphone was clearly louder than the call's sound over the steps
// it spans, and everyone else's otherwise, as when both were silent, or
// as loud as each other, or the stretch is outside what was measured. A
// stretch with no length takes the step it starts in.
func (s Said) whose(from, to time.Duration) Speaker {
	if s.Step <= 0 {
		return Others
	}
	first, last := s.step(from), s.step(to-1)
	if to <= from {
		last = first
	}
	steps := min(len(s.Mic), len(s.Call))
	var mic, call float64
	for i := max(first, 0); i <= last && i < steps; i++ {
		mic += s.Mic[i] * s.Mic[i]
		call += s.Call[i] * s.Call[i]
	}
	if mic > 0 && mic > Louder*Louder*call {
		return Person
	}
	return Others
}

// step is the step a moment of the result falls in.
func (s Said) step(d time.Duration) int {
	n := d / s.Step
	if d < 0 && d%s.Step != 0 {
		n--
	}
	return int(n)
}

// measured is how long the loudness covers.
func (s Said) measured() time.Duration {
	return time.Duration(min(len(s.Mic), len(s.Call))) * s.Step
}

// heard is what a result says in turns: its segments in order, each the
// person's or everyone else's, and a speaker's segments in a row joined
// into one piece, with when they were said. When none of its segments has
// a time, the whole result goes by the loudness of all its sound.
func (s Said) heard() []Heard {
	timed := false
	for _, seg := range s.Segments {
		timed = timed || seg.At > 0 || seg.Length > 0
	}
	whole := s.whose(0, s.measured())
	var out []Heard
	for _, seg := range s.Segments {
		text := strings.Join(strings.Fields(seg.Text), " ")
		if text == "" {
			continue
		}
		who, from, to := whole, s.Began, s.Began.Add(s.measured())
		if timed {
			who, from, to = s.whose(seg.At, seg.At+seg.Length), s.Began.Add(seg.At), s.Began.Add(seg.At+max(seg.Length, 0))
		}
		if n := len(out); n > 0 && out[n-1].Speaker == who {
			out[n-1].Text += " " + text
			if to.After(out[n-1].To) {
				out[n-1].To = to
			}
			continue
		}
		out = append(out, Heard{Speaker: who, Text: text, From: from, To: to})
	}
	return out
}

// counts is a result for the diagnostic log, in counts and levels only:
// its words and segments, those with no length, where they fall, whose its
// turns were, in how many steps the microphone was clearly louder, and how
// loud each sound was on average.
func (s Said) counts(turns []Heard) string {
	words, untimed := 0, 0
	var from, to time.Duration
	for i, seg := range s.Segments {
		words += len(strings.Fields(seg.Text))
		if seg.Length <= 0 {
			untimed++
		}
		if i == 0 || seg.At < from {
			from = seg.At
		}
		to = max(to, seg.At+seg.Length)
	}
	person, others := 0, 0
	for _, h := range turns {
		if n := len(strings.Fields(h.Text)); h.Speaker == Person {
			person += n
		} else {
			others += n
		}
	}
	steps, louder := min(len(s.Mic), len(s.Call)), 0
	for i := 0; i < steps; i++ {
		if s.Mic[i] > 0 && s.Mic[i] > Louder*s.Call[i] {
			louder++
		}
	}
	return fmt.Sprintf("said %d words in %d segments (%d with no length), from %.1f s to %.1f s of %.1f s measured: %d turns, %d words yours and %d others; "+
		"the microphone clearly louder in %d of %d steps, at %s on average, the call's sound at %s",
		words, len(s.Segments), untimed, from.Seconds(), to.Seconds(), s.measured().Seconds(), len(turns), person, others, louder, steps, level(s.Mic), level(s.Call))
}

// level is a loudness series' average power, in decibels of full scale.
func level(series []float64) string {
	if len(series) == 0 {
		return "unmeasured"
	}
	var power float64
	for _, v := range series {
		power += v * v
	}
	if power /= float64(len(series)); power <= 0 {
		return "silence"
	}
	return fmt.Sprintf("%.0f dBFS", 10*math.Log10(power))
}
