package live

import "github.com/gitdek/onair/internal/recorder"

// Tap is the recorder's edge with the live side listening in. Each call
// goes to the edge first, unchanged, and then tells the controller what it
// needs to know, which returns at once and never panics into the recorder:
// whether Live Context is on, what capture records, and whether a
// recording runs.
func Tap(edge recorder.Edge, c *Controller) recorder.Edge {
	return tap{Edge: edge, c: c}
}

type tap struct {
	recorder.Edge
	c *Controller
}

func (t tap) Start(cfg recorder.Config) {
	t.Edge.Start(cfg)
	// A window's picture holds that window alone, so the panel is never in
	// it.
	t.tell(func() { t.c.Capturing(cfg.Target.Display, cfg.LeaveOutOwnWindows || cfg.Target.Window) })
}

func (t tap) Show(recording bool, last, note string) {
	t.Edge.Show(recording, last, note)
	t.tell(func() { t.c.Recording(recording) })
}

func (t tap) Choices(c recorder.Choices) {
	t.Edge.Choices(c)
	t.tell(func() { t.c.Enabled(c.Live) })
}

// tell passes word to the live side, and keeps anything that goes wrong
// there from reaching the recorder.
func (t tap) tell(word func()) {
	defer func() { _ = recover() }()
	word()
}
