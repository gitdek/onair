//go:build darwin

// Package mac is the part of On Air that touches macOS: the menu-bar item,
// the screen and sound capture, and the writer that cuts them into
// segments. It decides nothing. What happens there reaches Go through a
// Handler, and Go tells it what to show.
package mac

/*
#cgo CFLAGS: -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Foundation -framework AppKit -framework AVFoundation -framework CoreMedia -framework CoreVideo -framework CoreGraphics -framework CoreAudio -framework AudioToolbox -framework ScreenCaptureKit -framework UniformTypeIdentifiers -framework ColorSync
#include <stdlib.h>
#include "recorder.h"
*/
import "C"

import (
	"errors"
	"sync"
	"unsafe"
)

// Handler receives what the edge reports. Segment and Event arrive on the
// recorder's threads, and the rest on the main one.
type Handler interface {
	// Segment is the opening segment when opening is true, and a media
	// segment otherwise. The bytes are the handler's to keep.
	Segment(opening bool, data []byte)
	Event(code int, message string)
	Menu(action int)
	// Displays are the connected displays, when the app starts and
	// whenever they change.
	Displays([]Display)
	// Pick is a display chosen from the menu.
	Pick(id string)
	// Area is an area drawn on a display, in its points from its top left.
	Area(display string, area Area)
	// Window names the window a person picked, which the edge keeps.
	Window(name string)
}

// Display is a connected display.
type Display struct {
	ID            string // stays the same across launches and reconnections
	Name          string
	Width, Height float64 // in points
	Main          bool    // the one with the menu bar
}

// Area is a part of a display, in its points from its top left.
type Area struct{ X, Y, Width, Height float64 }

// What Event reports.
const (
	Started  = int(C.ONAIR_STARTED)
	Finished = int(C.ONAIR_FINISHED)
	Failed   = int(C.ONAIR_FAILED)
	// MicrophoneFailed says capture didn't start because of the microphone,
	// and isn't running.
	MicrophoneFailed = int(C.ONAIR_MICROPHONE_FAILED)
	// TargetGone says capture didn't start because what it was asked to
	// record isn't there, and isn't running.
	TargetGone   = int(C.ONAIR_TARGET_GONE)
	WindowClosed = int(C.ONAIR_WINDOW_CLOSED)
	PickerFailed = int(C.ONAIR_PICKER_FAILED)
)

// What Menu reports.
const (
	MenuStart  = int(C.ONAIR_MENU_START)
	MenuStop   = int(C.ONAIR_MENU_STOP)
	MenuLast   = int(C.ONAIR_MENU_LAST)
	MenuFolder = int(C.ONAIR_MENU_FOLDER)
	MenuQuit   = int(C.ONAIR_MENU_QUIT)
	MenuArea   = int(C.ONAIR_MENU_AREA)
	MenuWhole  = int(C.ONAIR_MENU_WHOLE)
	MenuWindow = int(C.ONAIR_MENU_WINDOW)
)

// Config is what a recording is asked to be.
type Config struct {
	Rows         int     // the picture's height at most
	FPS          int     // frames a second at most
	PieceSeconds float64 // how long a piece is
	VideoBitrate int     // bits a second
	Microphone   bool    // mix the microphone into the display's sound
	Window       bool    // record the picked window
	Display      string  // otherwise this display, or the main one when empty
	Area         Area    // an area of it, or all of it when it has no width
}

var (
	mu      sync.Mutex
	handler Handler
)

// Handle sets who hears from the edge.
func Handle(h Handler) {
	mu.Lock()
	handler = h
	mu.Unlock()
}

func current() Handler {
	mu.Lock()
	defer mu.Unlock()
	return handler
}

//export onairPiece
func onairPiece(kind C.int, data unsafe.Pointer, length C.long) {
	if h := current(); h != nil {
		h.Segment(kind == 0, C.GoBytes(data, C.int(length)))
	}
}

//export onairEvent
func onairEvent(code C.int, message *C.char) {
	if h := current(); h != nil {
		h.Event(int(code), C.GoString(message))
	}
}

//export onairMenu
func onairMenu(action C.int) {
	if h := current(); h != nil {
		h.Menu(int(action))
	}
}

//export onairDisplays
func onairDisplays(list *C.onair_display, count C.int) {
	h := current()
	if h == nil {
		return
	}
	var displays []Display
	if count > 0 {
		for _, d := range unsafe.Slice(list, int(count)) {
			displays = append(displays, Display{ID: C.GoString(d.id), Name: C.GoString(d.name),
				Width: float64(d.width), Height: float64(d.height), Main: d.main != 0})
		}
	}
	h.Displays(displays)
}

//export onairPick
func onairPick(id *C.char) {
	if h := current(); h != nil {
		h.Pick(C.GoString(id))
	}
}

//export onairArea
func onairArea(display *C.char, x, y, width, height C.double) {
	if h := current(); h != nil {
		h.Area(C.GoString(display), Area{X: float64(x), Y: float64(y), Width: float64(width), Height: float64(height)})
	}
}

//export onairWindow
func onairWindow(name *C.char) {
	if h := current(); h != nil {
		h.Window(C.GoString(name))
	}
}

// Start asks for capture and returns at once: Started, Failed,
// MicrophoneFailed or TargetGone follows.
func Start(c Config) {
	display := C.CString(c.Display)
	defer C.free(unsafe.Pointer(display))
	C.onair_start(C.onair_config{rows: C.int(c.Rows), fps: C.int(c.FPS), pieceSeconds: C.double(c.PieceSeconds),
		videoBitrate: C.int(c.VideoBitrate), microphone: flag(c.Microphone), window: flag(c.Window), display: display,
		areaX: C.double(c.Area.X), areaY: C.double(c.Area.Y), areaWidth: C.double(c.Area.Width), areaHeight: C.double(c.Area.Height)})
}

func flag(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// Stop ends capture: Finished follows the last segment.
func Stop() { C.onair_stop() }

// SelfTest writes segments from made-up frames and sound through the same
// writer capture uses, each sound track straight to the writer, and returns
// once the last one was handed over.
func SelfTest(seconds float64, audioTracks int, pieceSeconds float64) error {
	var message *C.char
	if C.onair_selftest(C.double(seconds), C.int(audioTracks), C.double(pieceSeconds), &message) != 0 {
		defer C.free(unsafe.Pointer(message))
		return errors.New(C.GoString(message))
	}
	return nil
}

// Mix is what SelfTestMix plays: the display's sound and the microphone,
// made up and shaped like capture's.
type Mix struct {
	Seconds      float64
	PieceSeconds float64
	QuietFrom    float64 // when the microphone stops sending
	QuietTo      float64 // and when it starts again
	SwitchAt     float64 // when the microphone goes from 44.1 kHz to 48 kHz
}

// MixReport is what the mixer did in SelfTestMix.
type MixReport struct {
	Late, Early int // sound frames it left out
	Handed      int // mixed buffers it handed to the writer
	Dropped     int // of those, ones the writer refused
}

// SelfTestMix writes segments as SelfTest does, with the two sources mixed
// into one sound track through the mixer capture uses.
func SelfTestMix(m Mix) (MixReport, error) {
	var report C.onair_mix_report
	var message *C.char
	plan := C.onair_mix{seconds: C.double(m.Seconds), pieceSeconds: C.double(m.PieceSeconds),
		quietFrom: C.double(m.QuietFrom), quietTo: C.double(m.QuietTo), switchAt: C.double(m.SwitchAt)}
	failed := C.onair_selftest_mix(plan, &report, &message) != 0
	r := MixReport{Late: int(report.late), Early: int(report.early), Handed: int(report.handed), Dropped: int(report.dropped)}
	if failed {
		defer C.free(unsafe.Pointer(message))
		return r, errors.New(C.GoString(message))
	}
	return r, nil
}

// Movie is what Probe read from a file.
type Movie struct {
	Seconds     float64
	VideoTracks int
	AudioTracks int
	// Where the first picture and sound tracks start, in seconds.
	VideoStart, AudioStart float64
}

// Probe reads a movie file's length and tracks, as a player would.
func Probe(path string) (Movie, error) {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	var movie C.onair_movie
	var message *C.char
	if C.onair_probe(p, &movie, &message) != 0 {
		defer C.free(unsafe.Pointer(message))
		return Movie{}, errors.New(C.GoString(message))
	}
	return Movie{Seconds: float64(movie.seconds), VideoTracks: int(movie.videoTracks), AudioTracks: int(movie.audioTracks),
		VideoStart: float64(movie.videoStart), AudioStart: float64(movie.audioStart)}, nil
}

// Sound is a movie's sound track, decoded as a player would.
type Sound struct {
	Samples []float32 // 48 kHz stereo, interleaved
	// Start is when the first sample is played, in seconds from the first
	// frame.
	Start float64
}

// DecodeSound decodes a movie file's one sound track.
func DecodeSound(path string) (Sound, error) {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	var samples *C.float
	var frames C.long
	var start C.double
	var message *C.char
	if C.onair_sound(p, &samples, &frames, &start, &message) != 0 {
		defer C.free(unsafe.Pointer(message))
		return Sound{}, errors.New(C.GoString(message))
	}
	defer C.free(unsafe.Pointer(samples))
	out := make([]float32, 2*int(frames))
	copy(out, unsafe.Slice((*float32)(unsafe.Pointer(samples)), len(out)))
	return Sound{Samples: out, Start: float64(start)}, nil
}

// Run shows the menu-bar item and runs the app on the calling thread, which
// must be the main one. It doesn't return.
func Run() { C.onair_run() }

// Show sets what the menu-bar item says.
func Show(recording bool, last, note string) {
	l, n := C.CString(last), C.CString(note)
	defer C.free(unsafe.Pointer(l))
	defer C.free(unsafe.Pointer(n))
	r := C.int(0)
	if recording {
		r = 1
	}
	C.onair_show(r, l, n)
}

// Reveal shows a file or a folder in the Finder.
func Reveal(path string) {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	C.onair_reveal(p)
}

// Quit ends the app.
func Quit() { C.onair_quit() }

// Choice is a display, as the menu offers it.
type Choice struct {
	ID, Title string
	Picked    bool
}

// Choices sets the part of the menu that picks what to record.
func Choices(open bool, displays []Choice, area, window string) {
	n := len(displays)
	list := (*C.onair_choice)(C.malloc(C.size_t(max(n, 1)) * C.size_t(unsafe.Sizeof(C.onair_choice{}))))
	defer C.free(unsafe.Pointer(list))
	items := unsafe.Slice(list, max(n, 1))
	for i, d := range displays {
		items[i] = C.onair_choice{id: C.CString(d.ID), title: C.CString(d.Title), picked: flag(d.Picked)}
	}
	defer func() {
		for i := range displays {
			C.free(unsafe.Pointer(items[i].id))
			C.free(unsafe.Pointer(items[i].title))
		}
	}()
	a, w := C.CString(area), C.CString(window)
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(w))
	C.onair_choices(flag(open), list, C.int(n), a, w)
}

// SelectArea lets a person drag out an area of a display: Area follows,
// unless they cancel.
func SelectArea(display string) {
	d := C.CString(display)
	defer C.free(unsafe.Pointer(d))
	C.onair_select_area(d)
}

// SelectWindow shows the system's picker for one window: Window follows,
// unless they cancel.
func SelectWindow() { C.onair_select_window() }

// frameSize is the picture's size in pixels that capture takes for content
// of a size in points at a scale.
func frameSize(width, height, scale float64, rows int) (int, int) {
	var w, h C.int
	C.onair_frame_size(C.double(width), C.double(height), C.double(scale), C.int(rows), &w, &h)
	return int(w), int(h)
}
