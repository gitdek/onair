package recorder

import (
	"fmt"
	"math"
	"strings"

	"github.com/gitdek/onair/internal/settings"
)

// Display is a connected display, as the edge sees it.
type Display struct {
	ID            string  // stays the same across launches and reconnections
	Name          string  // what macOS calls it
	Width, Height float64 // in points
	Main          bool    // the one with the menu bar
}

// Area is a part of a display, in its points from its top left corner.
type Area struct{ X, Y, Width, Height float64 }

// Target is what a recording captures: the picked window, which the edge
// holds, or a display, the main one when Display is empty, whole or an area
// of it.
type Target struct {
	Window  bool
	Display string
	Area    Area // zero for the whole display
}

// Choices is the part of the menu that picks what to record.
type Choices struct {
	Open     bool     // whether a choice can be made: not while recording
	Displays []Choice // the connected displays, with the one Start records picked
	Area     string   // names the picked area, when there is one
	Window   string   // names the picked window, when there is one
}

// Choice is a display in the menu.
type Choice struct {
	ID, Title string
	Picked    bool
}

// smallest is the least an area may be on each side, in points. Anything
// smaller is likelier a slip of the mouse than a meeting.
const smallest = 64

// picked is what is remembered between launches: a display, and maybe an
// area of it. A window has no name that outlasts it, so it isn't kept.
type picked struct {
	Display string `json:"display,omitempty"`
	Area    *Area  `json:"area,omitempty"`
}

// fallback is why a recording isn't of what was picked, if it isn't.
type fallback int

const (
	asPicked    fallback = iota
	displayGone          // the picked display isn't connected
	areaOff              // the picked area doesn't fit its display
	windowGone           // the picked window is gone
)

// now is what the menu says while such a recording runs.
func (f fallback) now() string {
	switch f {
	case displayGone:
		return "The picked display isn't connected, so On Air is recording the main display."
	case areaOff:
		return "The picked area doesn't fit the display any more, so On Air is recording the whole display."
	case windowGone:
		return "The picked window is gone, so On Air is recording the whole display."
	}
	return ""
}

// then is what it says once such a recording is saved.
func (f fallback) then() string {
	switch f {
	case displayGone:
		return "The picked display wasn't connected, so On Air recorded the main display."
	case areaOff:
		return "The picked area didn't fit the display, so On Air recorded the whole display."
	case windowGone:
		return "The picked window was gone, so On Air recorded the whole display."
	}
	return ""
}

// Displays takes the connected displays, which the edge reports at launch
// and whenever they change.
func (a *App) Displays(displays []Display) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.displays = append([]Display(nil), displays...)
	a.show()
}

// PickDisplay keeps a display chosen from the menu, whole, for the
// recordings that follow.
func (a *App) PickDisplay(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.canChoose() {
		return
	}
	if _, ok := a.display(id); !ok {
		a.note = "That display isn't connected any more."
		a.show()
		return
	}
	a.window = ""
	a.remember(picked{Display: id})
}

// AreaPicked keeps an area drawn on a display, for the recordings that
// follow.
func (a *App) AreaPicked(id string, area Area) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.canChoose() {
		return
	}
	d, ok := a.display(id)
	switch {
	case !ok:
		a.note = "That display isn't connected any more."
	case area.Width < smallest || area.Height < smallest:
		a.note = fmt.Sprintf("That area is too small to record. Drag one at least %d points on each side.", smallest)
	case !fits(area, d):
		a.note = "That area isn't all on the display."
	default:
		a.window = ""
		a.remember(picked{Display: id, Area: &area})
		return
	}
	a.show()
}

// WindowPicked keeps the window a person picked, which the edge holds, for
// the recordings that follow, until it closes or On Air quits.
func (a *App) WindowPicked(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.canChoose() {
		return
	}
	if name == "" {
		name = "a window with no name"
	}
	a.window, a.note = name, ""
	a.show()
}

// canChoose says whether what is recorded may change now, and when it
// can't, says why. The lock is held.
func (a *App) canChoose() bool {
	if a.state == idle {
		return true
	}
	a.note = "What is recorded can't change while a recording runs."
	a.show()
	return false
}

// remember keeps a choice of display and area, in memory and in the
// settings file. The lock is held.
func (a *App) remember(p picked) {
	a.picked, a.note = p, ""
	if a.settings != "" {
		if err := settings.Save(a.settings, p); err != nil {
			a.note = "On Air records this until it quits, but couldn't remember it: " + err.Error()
		}
	}
	a.show()
}

// target is what Start records, and why that isn't what was picked, if it
// isn't. The lock is held.
func (a *App) target() (Target, fallback) {
	if a.window != "" {
		return Target{Window: true}, asPicked
	}
	return a.displayTarget()
}

// displayTarget is the display Start records when no window is picked.
func (a *App) displayTarget() (Target, fallback) {
	if a.picked.Display == "" {
		return Target{}, asPicked
	}
	d, ok := a.display(a.picked.Display)
	switch {
	case !ok:
		return Target{}, displayGone
	case a.picked.Area == nil:
		return Target{Display: d.ID}, asPicked
	case !fits(*a.picked.Area, d):
		return Target{Display: d.ID}, areaOff
	}
	return Target{Display: d.ID, Area: *a.picked.Area}, asPicked
}

// withoutTarget starts capture again on what is left when the edge can't
// find what it was asked for: the whole display, for a window that is gone
// or an area off its display, and the main display for a display that
// isn't connected. A remembered choice never keeps a recording from
// starting. The lock is held.
func (a *App) withoutTarget() bool {
	if a.state != starting || a.quitting {
		return false
	}
	c := a.asked
	switch {
	case c.Target.Window:
		a.window = ""
		t, _ := a.displayTarget()
		c.Target, a.fallback = Target{Display: t.Display}, windowGone
	case c.Target.Area != (Area{}):
		c.Target, a.fallback = Target{Display: c.Target.Display}, areaOff
	case c.Target.Display != "":
		c.Target, a.fallback = Target{}, displayGone
	default:
		return false
	}
	a.asked, a.note = c, "Starting…"
	a.show()
	a.edge.Start(c)
	return true
}

// areaDisplay is the display an area is drawn on: the one Start records.
func (a *App) areaDisplay() string {
	t, _ := a.displayTarget()
	if t.Display != "" {
		return t.Display
	}
	for _, d := range a.displays {
		if d.Main {
			return d.ID
		}
	}
	return ""
}

func (a *App) display(id string) (Display, bool) {
	for _, d := range a.displays {
		if d.ID == id {
			return d, true
		}
	}
	return Display{}, false
}

// fits says whether an area lies on a display as it is now, with half a
// point to spare for rounding.
func fits(r Area, d Display) bool {
	const slack = 0.5
	return r.Width >= smallest && r.Height >= smallest && r.X >= -slack && r.Y >= -slack &&
		r.X+r.Width <= d.Width+slack && r.Y+r.Height <= d.Height+slack
}

// choices is what the menu offers to record. The lock is held.
func (a *App) choices() Choices {
	c := Choices{Open: a.state == idle}
	t, _ := a.displayTarget()
	for _, d := range a.displays {
		on := a.window == "" && (t.Display == d.ID || t.Display == "" && d.Main)
		c.Displays = append(c.Displays, Choice{ID: d.ID, Title: d.title(), Picked: on})
	}
	switch {
	case a.window != "":
		c.Window = "Window: " + a.window
	case a.picked.Area != nil && t.Display == a.picked.Display:
		c.Area = "Area " + size(a.picked.Area.Width, a.picked.Area.Height)
	}
	return c
}

func (d Display) title() string {
	name := d.Name
	if name == "" {
		name = "Display"
	}
	return name + ", " + size(d.Width, d.Height)
}

func size(w, h float64) string {
	return fmt.Sprintf("%d × %d", int(math.Round(w)), int(math.Round(h)))
}

// join puts notes together, leaving out empty ones.
func join(notes ...string) string {
	var kept []string
	for _, n := range notes {
		if n != "" {
			kept = append(kept, n)
		}
	}
	return strings.Join(kept, " ")
}
