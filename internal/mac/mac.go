//go:build darwin

// Package mac is the part of On Air that touches macOS: the menu-bar item,
// the screen and sound capture, the writer that cuts them into segments,
// and the live side's speech recognition and panel. It decides nothing.
// What happens there reaches Go through a Handler, and Go tells it what to
// show.
package mac

/*
#cgo CFLAGS: -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Foundation -framework AppKit -framework AVFoundation -framework CoreMedia -framework CoreVideo -framework CoreGraphics -framework CoreAudio -framework AudioToolbox -framework ScreenCaptureKit -framework UniformTypeIdentifiers -framework ColorSync -framework Speech
#include <stdlib.h>
#include "recorder.h"
#include "speechmix.h"
*/
import "C"

import (
	"errors"
	"strings"
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
	// Folder is the folder a person picked for recordings to go to.
	Folder(path string)
	// Heard is a final result of the live side's speech recognition, which
	// hears the call's sound and the microphone added into one. It arrives
	// on the live side's own thread.
	Heard(Said)
	// Live is what the live side's speech recognition reports, one of the
	// Live codes. It arrives on the live side's own threads.
	Live(code int, message string)
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
	// MicrophoneAllowed and MicrophoneRefused answer AskMicrophone.
	MicrophoneAllowed = int(C.ONAIR_MICROPHONE_ALLOWED)
	MicrophoneRefused = int(C.ONAIR_MICROPHONE_REFUSED)
	// MicrophoneQuiet says none of the microphone's sound came in
	// Config.QuietSeconds, and the message says what came instead.
	// MicrophoneHeard says its sound came after that.
	MicrophoneQuiet = int(C.ONAIR_MICROPHONE_QUIET)
	MicrophoneHeard = int(C.ONAIR_MICROPHONE_HEARD)
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
	MenuChoose = int(C.ONAIR_MENU_CHOOSE)
	MenuLive   = int(C.ONAIR_MENU_LIVE)
	MenuLocal  = int(C.ONAIR_MENU_LOCAL)
)

// What Live reports.
const (
	LiveAllowed     = int(C.ONAIR_LIVE_ALLOWED)
	LiveRefused     = int(C.ONAIR_LIVE_REFUSED)
	LiveUnsupported = int(C.ONAIR_LIVE_UNSUPPORTED)
	LiveFailed      = int(C.ONAIR_LIVE_FAILED)
	LiveDropped     = int(C.ONAIR_LIVE_DROPPED)
	// LiveDiag is a line for the diagnostic log: counts, kinds and codes,
	// never words that were said.
	LiveDiag = int(C.ONAIR_LIVE_DIAG)
)

// Said is a final result of speech recognition: its words, and how loud
// each of the two sounds was while they were said, which the live side
// decides whose they were by.
type Said struct {
	// Began is when the result's sound began, in seconds since 1970.
	Began    float64
	Segments []Segment
	// Step is how long each step of loudness lasts, in seconds. The first
	// starts at Began.
	Step float64
	// Mic and Call are each sound's loudness in each step: its RMS, from 0
	// to 1 of full scale, before the two were added.
	Mic, Call []float64
}

// Segment is a word or so of a result, and when it was said, in seconds
// from the start of the result's sound.
type Segment struct {
	Text       string
	At, Length float64
}

// Config is what a recording is asked to be.
type Config struct {
	Rows         int     // the picture's height at most
	FPS          int     // frames a second at most
	PieceSeconds float64 // how long a piece is
	VideoBitrate int     // bits a second
	Microphone   bool    // mix the microphone into the display's sound
	QuietSeconds float64 // how long to wait for the microphone's sound before saying none came
	Window       bool    // record the picked window
	Display      string  // otherwise this display, or the main one when empty
	Area         Area    // an area of it, or all of it when it has no width
	// LeaveOutOwnWindows leaves On Air's own windows, such as the live
	// panel, out of a display's picture.
	LeaveOutOwnWindows bool
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

//export onairFolder
func onairFolder(path *C.char) {
	if h := current(); h != nil {
		h.Folder(C.GoString(path))
	}
}

//export onairHeard
func onairHeard(began C.double, segments *C.onair_segment, count C.int, step C.double, mic, call *C.double, steps C.int) {
	h := current()
	if h == nil {
		return
	}
	s := Said{Began: float64(began), Step: float64(step)}
	if segments != nil && count > 0 {
		for _, seg := range unsafe.Slice(segments, int(count)) {
			s.Segments = append(s.Segments, Segment{Text: C.GoString(seg.text), At: float64(seg.at), Length: float64(seg.length)})
		}
	}
	if mic != nil && call != nil && steps > 0 {
		for _, v := range unsafe.Slice(mic, int(steps)) {
			s.Mic = append(s.Mic, float64(v))
		}
		for _, v := range unsafe.Slice(call, int(steps)) {
			s.Call = append(s.Call, float64(v))
		}
	}
	h.Heard(s)
}

//export onairLive
func onairLive(code C.int, message *C.char) {
	if h := current(); h != nil {
		h.Live(int(code), C.GoString(message))
	}
}

// Start asks for capture and returns at once: Started, Failed,
// MicrophoneFailed or TargetGone follows.
func Start(c Config) {
	display := C.CString(c.Display)
	defer C.free(unsafe.Pointer(display))
	C.onair_start(C.onair_config{rows: C.int(c.Rows), fps: C.int(c.FPS), pieceSeconds: C.double(c.PieceSeconds),
		videoBitrate: C.int(c.VideoBitrate), microphone: flag(c.Microphone), quietSeconds: C.double(c.QuietSeconds), window: flag(c.Window), display: display,
		areaX: C.double(c.Area.X), areaY: C.double(c.Area.Y), areaWidth: C.double(c.Area.Width), areaHeight: C.double(c.Area.Height),
		leaveOutOwn: flag(c.LeaveOutOwnWindows)})
}

func flag(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// Stop ends capture: Finished follows the last segment.
func Stop() { C.onair_stop() }

// AskMicrophone asks whether On Air may use the microphone, and returns at
// once: MicrophoneAllowed or MicrophoneRefused follows, once there is an
// answer. When macOS hasn't asked the person yet, it asks them now.
func AskMicrophone() { C.onair_ask_microphone() }

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

// Choices sets the part of the menu that picks what to record, names the
// folder recordings go to, says whether Live Context is on, and names the
// model asked about the call, if any.
func Choices(open bool, displays []Choice, area, window, folder string, live bool, judge string) {
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
	a, w, f, j := C.CString(area), C.CString(window), C.CString(folder), C.CString(judge)
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(w))
	defer C.free(unsafe.Pointer(f))
	defer C.free(unsafe.Pointer(j))
	C.onair_choices(flag(open), list, C.int(n), a, w, f, flag(live), j)
}

// LiveAsk asks whether On Air may recognize speech, which macOS asks the
// person the first time, and returns at once: LiveAllowed or LiveRefused
// follows.
func LiveAsk() { C.onair_live_ask() }

// Listen starts or stops recognizing speech in capture's two sounds,
// added into one, on the Mac only, and returns at once. While on, Heard
// reports the finished words of about every chunk seconds of sound.
func Listen(on bool, chunk float64) { C.onair_live_listen(flag(on), C.double(chunk)) }

// Link is a link the live panel shows.
type Link struct{ Kind, Title, Date, Why, URL string }

// Answer is an answer card the live panel shows: a question as heard, an
// answer written for it and its sources, or the lines that answer it, and
// a word on it.
type Answer struct {
	Question string
	Text     string
	Sources  []AnswerLine
	Lines    []AnswerLine
	Note     string
}

// AnswerLine is a line of an answer, where it was said, and the moment a
// click opens.
type AnswerLine struct{ Text, Where, URL string }

// ShowPanel sets what the live panel shows, beside a display named as
// Display.ID names it, or the main one when empty: a line under its title,
// a status line, what a model said, a line each, what was said that
// differs from the record, when its Text is set, answer cards and links.
// It hides the panel when visible is false, and returns at once.
func ShowPanel(visible bool, display, headline, status string, calls []string, differs AnswerLine, answers []Answer, links []Link) {
	n := len(links)
	list := (*C.onair_link)(C.malloc(C.size_t(max(n, 1)) * C.size_t(unsafe.Sizeof(C.onair_link{}))))
	defer C.free(unsafe.Pointer(list))
	items := unsafe.Slice(list, max(n, 1))
	var strs []*C.char
	str := func(s string) *C.char {
		c := C.CString(s)
		strs = append(strs, c)
		return c
	}
	defer func() {
		for _, s := range strs {
			C.free(unsafe.Pointer(s))
		}
	}()
	for i, l := range links {
		items[i] = C.onair_link{kind: str(l.Kind), title: str(l.Title), date: str(l.Date), why: str(l.Why), link: str(l.URL)}
	}
	// A difference from the record goes first, as a row of its own. Each
	// card goes as rows: its question, its answer and sources, or its
	// lines, and its word.
	var rows []C.onair_answer
	if differs.Text != "" {
		rows = append(rows, C.onair_answer{kind: str("differs"), text: str(differs.Text), where: str(differs.Where), link: str(differs.URL)})
	}
	for _, a := range answers {
		rows = append(rows, C.onair_answer{kind: str("question"), text: str(a.Question), where: str(""), link: str("")})
		if a.Text != "" {
			rows = append(rows, C.onair_answer{kind: str("answer"), text: str(a.Text), where: str(""), link: str("")})
		}
		for _, l := range a.Sources {
			rows = append(rows, C.onair_answer{kind: str("source"), text: str(l.Text), where: str(""), link: str(l.URL)})
		}
		for _, l := range a.Lines {
			rows = append(rows, C.onair_answer{kind: str("line"), text: str(l.Text), where: str(l.Where), link: str(l.URL)})
		}
		if a.Note != "" {
			rows = append(rows, C.onair_answer{kind: str("note"), text: str(a.Note), where: str(""), link: str("")})
		}
	}
	answerList := (*C.onair_answer)(C.malloc(C.size_t(max(len(rows), 1)) * C.size_t(unsafe.Sizeof(C.onair_answer{}))))
	defer C.free(unsafe.Pointer(answerList))
	copy(unsafe.Slice(answerList, max(len(rows), 1)), rows)
	C.onair_live_show(flag(visible), str(display), str(headline), str(status), str(strings.Join(calls, "\n")), answerList, C.int(len(rows)), list, C.int(n))
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

// ChooseFolder shows the system's folder picker, opened on current: Folder
// follows, unless they cancel.
func ChooseFolder(current string) {
	c := C.CString(current)
	defer C.free(unsafe.Pointer(c))
	C.onair_choose_folder(c)
}

// frameSize is the picture's size in pixels that capture takes for content
// of a size in points at a scale.
func frameSize(width, height, scale float64, rows int) (int, int) {
	var w, h C.int
	C.onair_frame_size(C.double(width), C.double(height), C.double(scale), C.int(rows), &w, &h)
	return int(w), int(h)
}

// A buffer of one of the two sounds, given to speechMix: the sound, 0 the
// call's and 1 the microphone's, the frame its stamp puts it at, and its
// frames.
type mixBuffer struct {
	sound  int
	at     int
	frames []float32
}

// speechMix runs the live side's mix of the two sounds at rate over
// buffers, given in order, handing on what it can after each, and then up
// to now, a frame on the timeline, unless now is negative. It returns what
// it handed on, each sound's loudness in each step, and the frames it left
// out as late and as early.
func speechMix(rate int, buffers []mixBuffer, now int) (mix []float32, loud [][2]float32, late, early int) {
	var m C.oa_speechmix
	if C.oa_speechmix_init(&m, C.long(rate), 4) != 0 {
		return nil, nil, 0, 0
	}
	defer C.oa_speechmix_free(&m)
	out := make([]float32, int(m.step))
	var l [2]C.float
	take := func(now int) {
		ready := C.oa_speechmix_ready(&m, C.long(now))
		for C.oa_speechmix_take(&m, ready, (*C.float)(&out[0]), &l[0]) != 0 {
			mix = append(mix, out...)
			loud = append(loud, [2]float32{float32(l[0]), float32(l[1])})
		}
	}
	for _, b := range buffers {
		if len(b.frames) > 0 {
			C.oa_speechmix_place(&m, C.int(b.sound), (*C.float)(&b.frames[0]), C.long(len(b.frames)), C.long(b.at))
		}
		take(-1)
	}
	if now >= 0 {
		take(now)
	}
	return mix, loud, int(m.late), int(m.early)
}
