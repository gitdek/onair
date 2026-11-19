package replay

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/gitdek/onair/internal/live"
)

// Screen is the Mac as the live side sees it in a replay. It keeps each
// panel shown and each line of the diagnostic log, with when by the
// replay's clock, and asks the Mac for nothing.
type Screen struct {
	clock *Clock
	mu    sync.Mutex
	shown []Shown
}

// Shown is a panel shown, or a line of the diagnostic log, and when.
type Shown struct {
	At    time.Time   `json:"at"`
	Panel *live.Panel `json:"panel,omitempty"`
	Log   string      `json:"log,omitempty"`
}

func (s *Screen) Ask()                       {}
func (s *Screen) Listen(bool, time.Duration) {}

func (s *Screen) Show(p live.Panel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shown = append(s.shown, Shown{At: s.clock.Now(), Panel: &p})
}

func (s *Screen) Log(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shown = append(s.shown, Shown{At: s.clock.Now(), Log: line})
}

// Shown is what the panel showed and the log said, in order.
func (s *Screen) Shown() []Shown {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Shown(nil), s.shown...)
}

// Run is a replay: the words said and when the meeting began, how
// recognition hands them over, how fast the clock runs, the numbers the
// live side decides by, and what it asks. Judge and Writer may be nil.
type Run struct {
	Words   []Word
	Start   time.Time
	Speed   float64
	Heard   Recognition
	Config  live.Config
	Archive live.Archive
	Judge   live.Judge
	Writer  live.Writer
	// Progress, when set, is told how far the replay has got, by its
	// clock, now and then.
	Progress func(done, of time.Duration)
}

// Play plays the words through the live side, as a recording with Live
// Context and the local models on, and returns what the panel showed and
// the log said. clock, if not nil, is the clock the replay runs on, which
// the archive and models it asks must use too.
func (r Run) Play(clock *Clock) []Shown {
	if clock == nil {
		clock = NewClock(r.Start, r.Speed)
	}
	screen := &Screen{clock: clock}
	cfg := r.Config
	// The controller looks at the clock this often, in real time.
	cfg.Tick = max(time.Duration(float64(cfg.Tick)/clock.speed), time.Millisecond)
	judges, writers := map[string]live.Judge{}, map[string]live.Writer{}
	if r.Judge != nil {
		judges["local"] = r.Judge
	}
	if r.Writer != nil {
		writers["local"] = r.Writer
	}
	c := live.Start(cfg, r.Archive, screen, clock.Now, judges, writers)
	c.Judging("local")
	c.Report(live.SpeechAllowed, "")
	c.Enabled(true)
	c.Capturing("", true)
	c.Recording(true)
	events := r.Heard.Events(r.Words, r.Start)
	var end time.Duration
	if len(events) > 0 {
		end = events[len(events)-1].At
	}
	told := time.Duration(0)
	for _, e := range events {
		clock.Until(r.Start.Add(e.At), nil)
		c.Said(e.Said)
		if r.Progress != nil && e.At-told >= time.Minute {
			r.Progress(e.At, end)
			told = e.At
		}
	}
	// Answers still on their way come, and then the recording stops.
	clock.Until(r.Start.Add(end+15*time.Second), nil)
	c.Recording(false)
	clock.Until(r.Start.Add(end+20*time.Second), nil)
	c.Enabled(false)
	clock.Until(r.Start.Add(end+21*time.Second), nil)
	return screen.Shown()
}

// Write writes what a replay showed as JSON lines.
func Write(w io.Writer, shown []Shown) error {
	e := json.NewEncoder(w)
	for _, s := range shown {
		if err := e.Encode(s); err != nil {
			return err
		}
	}
	return nil
}

// Read reads what a replay showed from JSON lines.
func Read(r io.Reader) ([]Shown, error) {
	var out []Shown
	d := json.NewDecoder(r)
	for d.More() {
		var s Shown
		if err := d.Decode(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
