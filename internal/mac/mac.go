//go:build darwin

// Package mac is the part of On Air that touches macOS: the menu-bar item,
// the screen and sound capture, and the writer that cuts them into
// segments. It decides nothing. What happens there reaches Go through a
// Handler, and Go tells it what to show.
package mac

/*
#cgo CFLAGS: -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Foundation -framework AppKit -framework AVFoundation -framework CoreMedia -framework CoreVideo -framework CoreGraphics -framework CoreAudio -framework AudioToolbox -framework ScreenCaptureKit -framework UniformTypeIdentifiers
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
// recorder's threads, and Menu on the main one.
type Handler interface {
	// Segment is the opening segment when opening is true, and a media
	// segment otherwise. The bytes are the handler's to keep.
	Segment(opening bool, data []byte)
	Event(code int, message string)
	Menu(action int)
}

// What Event reports.
const (
	Started  = int(C.ONAIR_STARTED)
	Finished = int(C.ONAIR_FINISHED)
	Failed   = int(C.ONAIR_FAILED)
	// MicrophoneFailed says capture didn't start because of the microphone,
	// and isn't running.
	MicrophoneFailed = int(C.ONAIR_MICROPHONE_FAILED)
)

// What Menu reports.
const (
	MenuStart  = int(C.ONAIR_MENU_START)
	MenuStop   = int(C.ONAIR_MENU_STOP)
	MenuLast   = int(C.ONAIR_MENU_LAST)
	MenuFolder = int(C.ONAIR_MENU_FOLDER)
	MenuQuit   = int(C.ONAIR_MENU_QUIT)
)

// Config is what a recording is asked to be.
type Config struct {
	Rows         int     // the picture's height at most
	FPS          int     // frames a second at most
	PieceSeconds float64 // how long a piece is
	VideoBitrate int     // bits a second
	Microphone   bool    // mix the microphone into the display's sound
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

// Start asks for capture and returns at once: Started, Failed or
// MicrophoneFailed follows.
func Start(c Config) {
	microphone := C.int(0)
	if c.Microphone {
		microphone = 1
	}
	C.onair_start(C.onair_config{rows: C.int(c.Rows), fps: C.int(c.FPS), pieceSeconds: C.double(c.PieceSeconds),
		videoBitrate: C.int(c.VideoBitrate), microphone: microphone})
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
