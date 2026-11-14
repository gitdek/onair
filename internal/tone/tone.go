// Package tone measures how loud a tone is in recorded sound, so the
// self-test can tell that a file holds the sound that was played into it,
// at the moments it was played, with nothing else in it.
package tone

import "math"

// Sound is decoded sound: frames of interleaved channels at a rate. Start is
// when its first frame is played, in seconds from the recording's first
// frame, which is where every time given here is counted from.
type Sound struct {
	Samples  []float32
	Channels int
	Rate     float64
	Start    float64
}

// Seconds is how long the sound lasts.
func (s Sound) Seconds() float64 {
	if s.Channels <= 0 || s.Rate <= 0 {
		return 0
	}
	return float64(len(s.Samples)/s.Channels) / s.Rate
}

// Level is how loud the tone at hz is in one channel from one moment to
// another, as an amplitude: 0.2 for a sine that swings from -0.2 to 0.2. It
// uses the Goertzel algorithm, which measures one frequency. A stretch that
// holds a whole number of the tone's cycles measures it exactly, and any
// other tone that also fits whole cycles in it not at all.
func (s Sound) Level(channel int, hz, from, to float64) float64 {
	first, last := s.frame(from), s.frame(to)
	if last <= first || channel < 0 || channel >= s.Channels {
		return 0
	}
	c := 2 * math.Cos(2*math.Pi*hz/s.Rate)
	var s1, s2 float64
	for i := first; i < last; i++ {
		s0 := float64(s.Samples[i*s.Channels+channel]) + c*s1 - s2
		s2, s1 = s1, s0
	}
	power := s1*s1 + s2*s2 - c*s1*s2
	return 2 * math.Sqrt(math.Max(power, 0)) / float64(last-first)
}

// frame is the frame played at a moment, kept within the sound.
func (s Sound) frame(at float64) int {
	if s.Channels <= 0 {
		return 0
	}
	f := int(math.Round((at - s.Start) * s.Rate))
	return min(max(f, 0), len(s.Samples)/s.Channels)
}

// blocks calls each with the start of every whole block of the given length
// from one moment to another.
func blocks(from, to, block float64, each func(at float64)) {
	for i := 0; from+float64(i+1)*block <= to+1e-9; i++ {
		each(from + float64(i)*block)
	}
}

// Levels measures the tone in each whole block of the given length from one
// moment to another, and returns the least and the most it found.
func (s Sound) Levels(channel int, hz, from, to, block float64) (least, most float64) {
	least = math.Inf(1)
	blocks(from, to, block, func(at float64) {
		l := s.Level(channel, hz, at, at+block)
		least, most = math.Min(least, l), math.Max(most, l)
	})
	if math.IsInf(least, 1) {
		least = 0
	}
	return least, most
}

// Span is a stretch of a recording, in seconds.
type Span struct{ From, To float64 }

// Quiet lists the stretches from one moment to another where the tone stays
// below a level, measured in blocks of the given length, so that each edge
// is found to within a block.
func (s Sound) Quiet(channel int, hz, level, from, to, block float64) []Span {
	var spans []Span
	blocks(from, to, block, func(at float64) {
		if s.Level(channel, hz, at, at+block) >= level {
			return
		}
		if n := len(spans); n > 0 && math.Abs(spans[n-1].To-at) < 1e-9 {
			spans[n-1].To = at + block
			return
		}
		spans = append(spans, Span{at, at + block})
	})
	return spans
}
