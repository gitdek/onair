//go:build darwin

// Command onair is On Air, a menu-bar recorder for macOS.
package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/gitdek/onair/internal/mac"
	"github.com/gitdek/onair/internal/mp4"
	"github.com/gitdek/onair/internal/recorder"
	"github.com/gitdek/onair/internal/spool"
	"github.com/gitdek/onair/internal/tone"
)

func init() {
	// AppKit belongs to the thread the process started on.
	runtime.LockOSThread()
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "selftest" {
		if err := selftest(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "selftest failed:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 {
		fmt.Fprintln(os.Stderr, "usage: onair [selftest [-keep] [mix | TRACKS]]")
		os.Exit(2)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "On Air can't find your home directory:", err)
		os.Exit(1)
	}
	// Pieces are On Air's own. Joined recordings go to a folder of its own
	// too, until the hand-over to another folder is wired in.
	app := recorder.New(edge{}, recorder.Config{Rows: 1080, FPS: 15, PieceSeconds: 4, VideoBitrate: 1_800_000, Microphone: true},
		filepath.Join(home, "Library", "Application Support", "On Air", "spool"),
		filepath.Join(home, "Movies", "On Air"), time.Now)
	mac.Handle(bridge{app})
	mac.Run()
}

// edge is the Mac, as the recorder asks things of it.
type edge struct{}

func (edge) Start(c recorder.Config) {
	mac.Start(mac.Config{Rows: c.Rows, FPS: c.FPS, PieceSeconds: c.PieceSeconds, VideoBitrate: c.VideoBitrate, Microphone: c.Microphone})
}
func (edge) Stop()                                  { mac.Stop() }
func (edge) Show(recording bool, last, note string) { mac.Show(recording, last, note) }
func (edge) Reveal(path string)                     { mac.Reveal(path) }
func (edge) Quit()                                  { mac.Quit() }

// bridge is the recorder, as the Mac reports things to it.
type bridge struct{ app *recorder.App }

func (b bridge) Segment(opening bool, data []byte) { b.app.Segment(opening, data) }

func (b bridge) Event(code int, message string) {
	switch code {
	case mac.Started:
		b.app.Event(recorder.Started, message)
	case mac.Finished:
		b.app.Event(recorder.Finished, message)
	case mac.Failed:
		b.app.Event(recorder.Failed, message)
	case mac.MicrophoneFailed:
		b.app.Event(recorder.MicrophoneFailed, message)
	}
}

func (b bridge) Menu(action int) {
	switch action {
	case 0:
		b.app.Launched()
	case mac.MenuStart:
		b.app.Menu(recorder.Start)
	case mac.MenuStop:
		b.app.Menu(recorder.Stop)
	case mac.MenuLast:
		b.app.Menu(recorder.ShowLast)
	case mac.MenuFolder:
		b.app.Menu(recorder.OpenFolder)
	case mac.MenuQuit:
		b.app.Menu(recorder.Quit)
	}
}

// collector keeps the segments a self-test's writer hands over.
type collector struct {
	mu  sync.Mutex
	r   *spool.Recording
	n   int
	err error
}

func (c *collector) Segment(opening bool, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	if opening {
		c.err = c.r.PutOpening(data)
		return
	}
	c.n++
	c.err = c.r.PutPiece(c.n, data)
}

func (c *collector) Event(int, string) {}
func (c *collector) Menu(int)          {}

func (c *collector) kept() (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n, c.err
}

// What the mixed self-test plays: the display's sound at 440 Hz, louder on
// the left, and the microphone's at 880 Hz in both ears, quiet for a second
// and switching from 44.1 kHz to 48 kHz later on. No source plays 660 Hz.
var (
	plan             = mac.Mix{Seconds: 10, PieceSeconds: 2, QuietFrom: 4, QuietTo: 5, SwitchAt: 7}
	displayLoudness  = [2]float64{0.2, 0.1}
	microphoneLevel  = 0.15
	frame            = 1.0 / 15
	displayHz, micHz = 440.0, 880.0
	controlHz        = 660.0
)

// selftest writes ten seconds of made-up picture and sound as pieces, then
// checks that one piece plays on its own, that the join plays whole, and
// that both start at zero. By default the sound is the display's and the
// microphone's, mixed as capture mixes them, and the check listens to what
// was written; given a number, that many sound tracks go to the writer
// directly. It records nothing, so it needs no permission.
func selftest(args []string) error {
	flags := flag.NewFlagSet("selftest", flag.ContinueOnError)
	keep := flags.Bool("keep", false, "keep the pieces and the joined file, and say where")
	if err := flags.Parse(args); err != nil {
		return err
	}
	mixed, tracks := true, 1
	if mode := flags.Arg(0); mode != "" && mode != "mix" {
		n, err := strconv.Atoi(mode)
		if err != nil || n < 0 || flags.NArg() > 1 {
			return fmt.Errorf("selftest takes mix or a number of sound tracks, not %q", flags.Args())
		}
		mixed, tracks = false, n
	}
	dir, err := os.MkdirTemp("", "onair-selftest-")
	if err != nil {
		return err
	}
	if *keep {
		fmt.Println("kept in", dir)
	} else {
		defer os.RemoveAll(dir)
	}
	r, err := spool.Create(filepath.Join(dir, "spool"), "selftest")
	if err != nil {
		return err
	}
	c := &collector{r: r}
	mac.Handle(c)
	if mixed {
		report, err := mac.SelfTestMix(plan)
		fmt.Printf("mixed: %d buffers handed to the writer, %d refused; %d frames of sound late, %d early\n", report.Handed, report.Dropped, report.Late, report.Early)
		if err != nil {
			return err
		}
		if report.Late+report.Early+report.Dropped > 0 {
			return errors.New("the mixer left sound out")
		}
	} else if err := mac.SelfTest(plan.Seconds, tracks, plan.PieceSeconds); err != nil {
		return err
	}
	// Stop counts on the last segment coming before the writer says it
	// finished.
	written, _ := c.kept()
	time.Sleep(300 * time.Millisecond)
	if n, err := c.kept(); err != nil {
		return err
	} else if n != written {
		return errors.New("a segment came after the writer said it had finished")
	}
	pieces, _, err := r.Pieces()
	if err != nil {
		return err
	}
	if len(pieces) < 4 {
		return fmt.Errorf("%v seconds in %v-second pieces gave %d pieces", plan.Seconds, plan.PieceSeconds, len(pieces))
	}
	one, err := mac.Probe(pieces[1].Path)
	if err != nil {
		return fmt.Errorf("piece 2 doesn't play on its own: %w", err)
	}
	joined, err := r.Join(filepath.Join(dir, "joined.mp4"))
	if err != nil {
		return err
	}
	whole, err := mac.Probe(joined.Path)
	if err != nil {
		return fmt.Errorf("the joined file doesn't play: %w", err)
	}
	fmt.Printf("%d pieces, %d bytes joined\n", joined.Pieces, joined.Bytes)
	fmt.Printf("piece 2 alone: %.2f s, %d video and %d sound tracks\n", one.Seconds, one.VideoTracks, one.AudioTracks)
	fmt.Printf("joined: %.3f s, %d video and %d sound tracks, the picture from %.4f s and the sound from %.4f s\n",
		whole.Seconds, whole.VideoTracks, whole.AudioTracks, whole.VideoStart, whole.AudioStart)
	if whole.VideoTracks != 1 || whole.AudioTracks != tracks || math.Abs(whole.Seconds-plan.Seconds) > 0.1 {
		return fmt.Errorf("the joined file isn't the %v seconds with one picture and %d sound tracks that were written", plan.Seconds, tracks)
	}
	if one.VideoTracks != 1 || one.AudioTracks != tracks {
		return fmt.Errorf("piece 2 doesn't hold one picture and %d sound tracks", tracks)
	}
	if math.Abs(whole.VideoStart) > frame || tracks > 0 && math.Abs(whole.AudioStart) > frame {
		return errors.New("the joined file doesn't start at zero")
	}
	if err := startsAtZero(joined.Path, pieces[1].Path); err != nil {
		return err
	}
	if mixed {
		return listen(joined.Path)
	}
	return nil
}

// startsAtZero reads the files' own bytes, as any reader of them would, to
// see that the joined file's tracks start at zero and that piece 2's start
// together, later on the same timeline.
func startsAtZero(joined, piece string) error {
	whole, err := mp4.File(joined)
	if err != nil {
		return fmt.Errorf("the joined file's fragments: %w", err)
	}
	two, err := mp4.File(piece)
	if err != nil {
		return fmt.Errorf("piece 2's fragments: %w", err)
	}
	fmt.Printf("first fragments: joined %s; piece 2 %s\n", decodeTimes(whole), decodeTimes(two))
	for _, t := range whole {
		if t.Seconds() > frame {
			return fmt.Errorf("the joined file's %s track starts at %.3f s, not zero", t.Handler, t.Seconds())
		}
	}
	for _, t := range two {
		if t.Seconds() <= 0 || t.Seconds() >= plan.Seconds || math.Abs(t.Seconds()-two[0].Seconds()) > frame {
			return fmt.Errorf("piece 2's %s track starts at %.3f s, not with the rest of piece 2 within the recording", t.Handler, t.Seconds())
		}
	}
	return nil
}

func decodeTimes(tracks []mp4.Track) string {
	s := ""
	for i, t := range tracks {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("%s at %d/%d = %.4f s", t.Handler, t.First, t.Timescale, t.Seconds())
	}
	return s
}

// listen decodes the joined file's sound, as a player would, and measures
// its tones in 50-millisecond blocks, each of which holds whole cycles of
// all three: the display's in both channels at their two loudnesses, the
// microphone's in both but for its quiet second, which must be where it was
// played, and nothing at the frequency no source played. A block is long
// enough to hold most of a gap at a seam, so a dropout shows as a quiet one.
func listen(path string) error {
	decoded, err := mac.DecodeSound(path)
	if err != nil {
		return fmt.Errorf("the joined file's sound: %w", err)
	}
	s := tone.Sound{Samples: decoded.Samples, Channels: 2, Rate: 48000, Start: decoded.Start}
	fmt.Printf("sound: %.3f s decoded, from %.4f s after the first frame\n", s.Seconds(), s.Start)
	if math.Abs(s.Start) > frame || math.Abs(s.Seconds()-plan.Seconds) > 0.1 {
		return errors.New("the sound doesn't cover the recording")
	}
	const block = 0.05
	from, to := 0.25, plan.Seconds-0.25
	near := func(least, most, want float64) bool { return least >= 0.8*want && most <= 1.2*want }
	for ch, side := range []string{"left", "right"} {
		least, most := s.Levels(ch, displayHz, from, to, block)
		fmt.Printf("%s: the display's %v Hz from %.4f to %.4f, made at %v\n", side, displayHz, least, most, displayLoudness[ch])
		if !near(least, most, displayLoudness[ch]) {
			return fmt.Errorf("the display's sound isn't all there on the %s", side)
		}
		least, most = s.Levels(ch, micHz, from, plan.QuietFrom-0.1, block)
		least2, most2 := s.Levels(ch, micHz, plan.QuietTo+0.1, to, block)
		least, most = math.Min(least, least2), math.Max(most, most2)
		_, quiet := s.Levels(ch, micHz, plan.QuietFrom+0.1, plan.QuietTo-0.1, block)
		fmt.Printf("%s: the microphone's %v Hz from %.4f to %.4f, made at %v, and at most %.4f in its quiet second\n", side, micHz, least, most, microphoneLevel, quiet)
		if !near(least, most, microphoneLevel) {
			return fmt.Errorf("the microphone's sound isn't all there on the %s", side)
		}
		if quiet > 0.005 {
			return fmt.Errorf("the microphone's quiet second isn't quiet on the %s", side)
		}
		_, control := s.Levels(ch, controlHz, from, to, block)
		fmt.Printf("%s: %v Hz, which nothing played, at most %.4f\n", side, controlHz, control)
		if control > 0.005 {
			return fmt.Errorf("%v Hz is on the %s, and nothing played it", controlHz, side)
		}
	}
	gaps := s.Quiet(0, micHz, microphoneLevel/2, from, to, 0.025)
	if len(gaps) != 1 {
		return fmt.Errorf("the microphone went quiet %d times, not once: %v", len(gaps), gaps)
	}
	fmt.Printf("the microphone's quiet second: %.3f to %.3f s, played from %v to %v s\n", gaps[0].From, gaps[0].To, plan.QuietFrom, plan.QuietTo)
	if math.Abs(gaps[0].From-plan.QuietFrom) > 0.05 || math.Abs(gaps[0].To-plan.QuietTo) > 0.05 {
		return errors.New("the microphone's sound isn't where it was played")
	}
	return nil
}
