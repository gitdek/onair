package tone

import (
	"math"
	"testing"
)

// made is a second of stereo at 48 kHz: 440 Hz at 0.2 on the left and 0.1
// on the right, and 880 Hz at 0.15 on both but for a gap.
func made(start, gapFrom, gapTo float64) Sound {
	const rate = 48000
	s := Sound{Channels: 2, Rate: rate, Start: start, Samples: make([]float32, 2*rate)}
	for i := 0; i < rate; i++ {
		t := float64(i) / rate
		a := math.Sin(2 * math.Pi * 440 * t)
		b := 0.15 * math.Sin(2*math.Pi*880*t+1)
		if t+start >= gapFrom && t+start < gapTo {
			b = 0
		}
		s.Samples[2*i] = float32(0.2*a + b)
		s.Samples[2*i+1] = float32(0.1*a + b)
	}
	return s
}

func near(got, want float64) bool { return math.Abs(got-want) < 0.001 }

func TestEachToneIsMeasuredInEachChannel(t *testing.T) {
	s := made(0, 2, 2)
	if s.Seconds() != 1 {
		t.Fatalf("a second lasts %v", s.Seconds())
	}
	for _, c := range []struct {
		channel int
		hz      float64
		want    float64
	}{{0, 440, 0.2}, {1, 440, 0.1}, {0, 880, 0.15}, {1, 880, 0.15}, {0, 660, 0}, {1, 660, 0}} {
		if got := s.Level(c.channel, c.hz, 0.1, 0.6); !near(got, c.want) {
			t.Errorf("channel %d at %v Hz: %v, want %v", c.channel, c.hz, got, c.want)
		}
	}
	if least, most := s.Levels(0, 440, 0, 1, 0.05); !near(least, 0.2) || !near(most, 0.2) {
		t.Fatalf("440 Hz in blocks: %v to %v", least, most)
	}
}

func TestAGapIsFoundToWithinABlock(t *testing.T) {
	s := made(0, 0.4, 0.6)
	spans := s.Quiet(1, 880, 0.075, 0, 1, 0.025)
	if len(spans) != 1 || !near(spans[0].From, 0.4) || !near(spans[0].To, 0.6) {
		t.Fatalf("quiet: %v", spans)
	}
	if _, most := s.Levels(0, 880, 0.45, 0.55, 0.05); most > 0.001 {
		t.Fatalf("880 Hz in the gap: at most %v", most)
	}
	if least, _ := s.Levels(0, 880, 0, 1, 0.05); least > 0.001 {
		t.Fatalf("the gap didn't show: the least was %v", least)
	}
	// A gap that falls across blocks makes a block of each side quieter.
	if spans := made(0, 0.41, 0.61).Quiet(0, 880, 0.075, 0, 1, 0.025); len(spans) != 1 || math.Abs(spans[0].From-0.41) > 0.025 || math.Abs(spans[0].To-0.61) > 0.025 {
		t.Fatalf("quiet: %v", spans)
	}
	if spans := s.Quiet(0, 440, 0.1, 0, 1, 0.025); len(spans) != 0 {
		t.Fatalf("a steady tone was quiet: %v", spans)
	}
}

// Times count from the recording's first frame, wherever the sound starts.
func TestTimesCountFromTheFirstFrame(t *testing.T) {
	s := made(0.5, 0.9, 1.1)
	if got := s.Level(0, 880, 0.5, 0.9); !near(got, 0.15) {
		t.Fatalf("before the gap: %v", got)
	}
	if got := s.Level(0, 880, 0.9, 1.1); got > 0.001 {
		t.Fatalf("in the gap: %v", got)
	}
	if got := s.Level(0, 440, 0, 0.5); got != 0 {
		t.Fatalf("before the sound starts: %v", got)
	}
	if got := s.Level(2, 440, 0.5, 1); got != 0 {
		t.Fatalf("a channel it doesn't have: %v", got)
	}
}
