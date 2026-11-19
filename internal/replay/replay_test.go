package replay

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gitdek/onair/internal/live"
)

// whisper is a made-up transcript in the shape Whisper writes.
const whisper = `{"text": "", "segments": [
 {"start": 0.0, "end": 2.0, "text": " So how much did Halcyon raise?", "words": [
  {"word": " So", "start": 0.0, "end": 0.2}, {"word": " how", "start": 0.3, "end": 0.5}, {"word": " much", "start": 0.6, "end": 0.8},
  {"word": " did", "start": 0.9, "end": 1.0}, {"word": " Halcyon", "start": 1.1, "end": 1.6}, {"word": " raise?", "start": 1.7, "end": 2.0}]},
 {"start": 16.0, "end": 17.0, "text": " Okay.", "words": [{"word": " Okay.", "start": 16.0, "end": 17.0}]}
]}`

func TestAWhisperTranscriptIsReadWithItsWordTimes(t *testing.T) {
	words, err := ReadWhisper(strings.NewReader(whisper))
	if err != nil {
		t.Fatal(err)
	}
	if len(words) != 7 || words[4].Text != "Halcyon" || words[4].From != 1100*time.Millisecond || words[6].To != 17*time.Second {
		t.Fatalf("read %+v", words)
	}
	if _, err := ReadWhisper(strings.NewReader(`{"segments": []}`)); err == nil {
		t.Error("a transcript with no words was taken")
	}
}

// A word whose times can't be in a meeting of a day at most is left out,
// rather than read as a time no clock can hold.
func TestATranscriptsWordsAtTimesThatCantBeAreLeftOut(t *testing.T) {
	words, err := ReadWhisper(strings.NewReader(`{"segments": [{"words": [
		{"word": " one", "start": 1.0, "end": 1.5},
		{"word": " never", "start": 1e12, "end": 5},
		{"word": " before", "start": -1, "end": 2},
		{"word": " after", "start": 2, "end": 1e9},
		{"word": " two", "start": 2.0, "end": 2.5}]}]}`))
	if err != nil || len(words) != 2 || words[0].Text != "one" || words[1].Text != "two" {
		t.Fatalf("read %+v, %v", words, err)
	}
}

func TestRecognitionHandsOverPartialResultsBehindTheSoundAndAFinalWithTimes(t *testing.T) {
	words, _ := ReadWhisper(strings.NewReader(whisper))
	start := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	r := Recognition{Request: 15 * time.Second, Every: 250 * time.Millisecond, Behind: time.Second, Final: 150 * time.Millisecond}
	events := r.Events(words, start)
	var first, final *Event
	for i, e := range events {
		if e.Said.Request == 1 && e.Said.Partial && len(e.Said.Segments) > 0 && first == nil {
			first = &events[i]
		}
		if e.Said.Request == 1 && !e.Said.Partial {
			final = &events[i]
		}
	}
	// "So" ends at 0.2 s, and is heard a second later, at the first
	// partial result from then.
	if first == nil || first.At != 1250*time.Millisecond || first.Said.Segments[0].Text != "So" || first.Said.Segments[0].At != 0 || first.Said.Segments[0].Length != 0 {
		t.Fatalf("first partial result with words: %+v", first)
	}
	if got := first.Said.Began.Add(time.Duration(len(first.Said.Mic)) * first.Said.Step); !got.Equal(start.Add(1200 * time.Millisecond)) {
		t.Errorf("the first partial result's sound goes to %v", got.Sub(start))
	}
	if final == nil || final.At != 15150*time.Millisecond || len(final.Said.Segments) != 6 || final.Said.Segments[4].At != 1100*time.Millisecond {
		t.Fatalf("final: %+v", final)
	}
	last := events[len(events)-1]
	if last.Said.Partial || last.Said.Request != 2 || last.Said.Segments[0].Text != "Okay." {
		t.Errorf("the last request's final: %+v", last)
	}
	for i := 1; i < len(events); i++ {
		if events[i].At < events[i-1].At {
			t.Fatalf("events out of order at %d", i)
		}
	}
}

func TestRecognitionLosesItsShareOfWordsInRuns(t *testing.T) {
	var words []Word
	for i := range 6000 {
		words = append(words, Word{Text: "w", From: time.Duration(i) * 300 * time.Millisecond, To: time.Duration(i)*300*time.Millisecond + 200*time.Millisecond})
	}
	heard := Recognition{Lose: 0.35, Seed: 1}.heard(words)
	if share := 1 - float64(len(heard))/float64(len(words)); share < 0.31 || share > 0.39 {
		t.Errorf("lost %.2f of the words", share)
	}
	again := Recognition{Lose: 0.35, Seed: 1}.heard(words)
	if len(again) != len(heard) || again[100] != heard[100] {
		t.Error("the same seed lost other words")
	}
}

func TestTheClockRunsFastAndStandsStillWhileHeld(t *testing.T) {
	from := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	c := NewClock(from, 50)
	time.Sleep(20 * time.Millisecond)
	if d := c.Now().Sub(from); d < 900*time.Millisecond || d > 3*time.Second {
		t.Errorf("after 20 ms at 50 times, the clock says %v", d)
	}
	release := c.Hold()
	held := c.Now()
	time.Sleep(20 * time.Millisecond)
	if !c.Now().Equal(held) {
		t.Error("the clock moved while held")
	}
	release()
	release()
	if !c.Until(held.Add(time.Second), nil) || c.Now().Before(held.Add(time.Second)) {
		t.Error("the clock didn't get there")
	}
	stop := make(chan struct{})
	close(stop)
	if c.Until(c.Now().Add(time.Hour), stop) {
		t.Error("a wait that was stopped got there")
	}
}

func TestAnAnswerIsAskedOnceAndThenTakesAsLongByTheClock(t *testing.T) {
	store := &Store{Dir: t.TempDir()}
	c := NewClock(time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC), 20)
	var real atomic.Int32
	slow := func(ctx context.Context) (string, error) {
		real.Add(1)
		time.Sleep(50 * time.Millisecond)
		return "made up", nil
	}
	asked := c.Now()
	got, err := ask(context.Background(), c, store, "q", errors.New("late"), nil, slow)
	if err != nil || got != "made up" {
		t.Fatalf("got %q, %v", got, err)
	}
	// Asked for real, the clock stood still while it was, and then went
	// on as long as it took.
	if took := c.Now().Sub(asked); took < 50*time.Millisecond || took > 600*time.Millisecond {
		t.Errorf("by the clock it took %v", took)
	}
	asked = c.Now()
	got, err = ask(context.Background(), c, store, "q", errors.New("late"), nil, slow)
	if err != nil || got != "made up" || real.Load() != 1 {
		t.Fatalf("again: %q, %v, asked for real %d times", got, err, real.Load())
	}
	if took := c.Now().Sub(asked); took < 50*time.Millisecond || took > 600*time.Millisecond {
		t.Errorf("answered from what was kept, by the clock it took %v", took)
	}
	if store.Asked.Load() != 1 || store.Kept.Load() != 1 {
		t.Errorf("asked %d, kept %d", store.Asked.Load(), store.Kept.Load())
	}
	// What the live side allows it, by its deadline, holds by the clock.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := ask(ctx, c, store, "q", errors.New("late"), nil, slow); err == nil || err.Error() != "late" {
		t.Errorf("an answer that took longer than allowed: %v", err)
	}
	// Given up on, it ends at once.
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if _, err := ask(ctx, c, store, "q", errors.New("late"), nil, slow); !errors.Is(err, context.Canceled) {
		t.Errorf("an answer given up on: %v", err)
	}
	failing := func(context.Context) (string, error) { return "", live.ErrLoading }
	if _, err := ask(context.Background(), c, store, "loading", errors.New("late"), nil, failing); !errors.Is(err, live.ErrLoading) {
		t.Errorf("a model loading: %v", err)
	}
	if _, err := ask(context.Background(), c, store, "loading", errors.New("late"), nil, failing); !errors.Is(err, live.ErrLoading) {
		t.Errorf("a model loading, kept: %v", err)
	}
}

// A model still loading when it is first asked loads with the clock held,
// so what the live side allows a question, by its deadline, starts once
// the model is ready: the answer that comes in that time is the one kept,
// not the deadline that passed while the model loaded.
func TestAnAnswerAskedWhileTheModelLoadsIsKeptAsAnAnswer(t *testing.T) {
	store := &Store{Dir: t.TempDir()}
	c := NewClock(time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC), 20)
	loading := func() { time.Sleep(100 * time.Millisecond) }
	answer := func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "made up", nil
	}
	for i, ready := range []func(){loading, nil} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		got, err := ask(ctx, c, store, "q", errors.New("late"), ready, answer)
		cancel()
		if err != nil || got != "made up" {
			t.Fatalf("asked %d times: %q, %v", i+1, got, err)
		}
	}
	if store.Asked.Load() != 1 || store.Kept.Load() != 1 {
		t.Errorf("asked %d, kept %d", store.Asked.Load(), store.Kept.Load())
	}
}

// madeUpArchive answers every question with a made-up brief.
type madeUpArchive struct{}

func (madeUpArchive) Connections(context.Context, string) (live.Found, error) {
	return live.Found{}, nil
}
func (madeUpArchive) Answer(context.Context, string, bool) (live.Brief, error) {
	return live.Brief{Points: []live.Point{{Text: "Halcyon raised $750k from two angels", Meeting: "Halcyon sync", Date: "2026-02-10", At: "6:12", URL: "https://archive.example/m/1?t=372"}}}, nil
}

func TestAReplayShowsWhatTheLiveSideWould(t *testing.T) {
	words, _ := ReadWhisper(strings.NewReader(whisper))
	start := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	clock := NewClock(start, 40)
	store := &Store{Dir: t.TempDir()}
	shown := Run{Words: words, Start: start, Heard: Recognition{Request: 15 * time.Second, Every: 250 * time.Millisecond, Behind: time.Second, Final: 150 * time.Millisecond},
		Config: live.Defaults, Archive: Archive{Real: madeUpArchive{}, Clock: clock, Store: store}}.Play(clock)
	answered := time.Time{}
	for _, s := range shown {
		if s.Panel != nil && len(s.Panel.Answers) > 0 && len(s.Panel.Answers[0].Lines) > 0 && answered.IsZero() {
			answered = s.At
		}
	}
	if answered.IsZero() {
		t.Fatal("the question was never answered")
	}
	// The question's last word ends at 2 s; recognition hears it a second
	// later, and the sentence is finished 0.7 s of sound after that.
	if d := answered.Sub(start); d < 3*time.Second || d > 6*time.Second {
		t.Errorf("answered %v into the meeting", d)
	}
	var b strings.Builder
	if err := Write(&b, shown); err != nil {
		t.Fatal(err)
	}
	back, err := Read(strings.NewReader(b.String()))
	if err != nil || len(back) != len(shown) {
		t.Fatalf("read back %d of %d: %v", len(back), len(shown), err)
	}
}
