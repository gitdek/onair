//go:build darwin

// Command onair is On Air, a menu-bar recorder for macOS.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/gitdek/onair/internal/mac"
	"github.com/gitdek/onair/internal/recorder"
	"github.com/gitdek/onair/internal/spool"
)

func init() {
	// AppKit belongs to the thread the process started on.
	runtime.LockOSThread()
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "selftest" {
		tracks := 2
		if len(os.Args) > 2 {
			fmt.Sscan(os.Args[2], &tracks)
		}
		if err := selftest(tracks); err != nil {
			fmt.Fprintln(os.Stderr, "selftest failed:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 {
		fmt.Fprintln(os.Stderr, "usage: onair [selftest]")
		os.Exit(2)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "On Air can't find your home directory:", err)
		os.Exit(1)
	}
	// Pieces are On Air's own. Joined recordings go to a folder of its own
	// too, until the hand-over to another folder is wired in.
	app := recorder.New(edge{}, recorder.Config{Rows: 1080, FPS: 15, PieceSeconds: 4, VideoBitrate: 1_800_000},
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

// selftest writes ten seconds of made-up picture and sound as pieces, then
// checks that one piece plays on its own and that the join plays whole. It
// records nothing, so it needs no permission.
func selftest(tracks int) error {
	dir, err := os.MkdirTemp("", "onair-selftest-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	const seconds, pieceSeconds = 10.0, 2.0
	r, err := spool.Create(filepath.Join(dir, "spool"), "selftest")
	if err != nil {
		return err
	}
	c := &collector{r: r}
	mac.Handle(c)
	if err := mac.SelfTest(seconds, tracks, pieceSeconds); err != nil {
		return err
	}
	if c.err != nil {
		return c.err
	}
	pieces, _, err := r.Pieces()
	if err != nil {
		return err
	}
	if len(pieces) < 4 {
		return fmt.Errorf("%v seconds in %v-second pieces gave %d pieces", seconds, pieceSeconds, len(pieces))
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
	fmt.Printf("joined: %.2f s, %d video and %d sound tracks\n", whole.Seconds, whole.VideoTracks, whole.AudioTracks)
	if whole.VideoTracks != 1 || whole.AudioTracks != tracks || whole.Seconds < seconds-0.5 || whole.Seconds > seconds+0.5 {
		return fmt.Errorf("the joined file isn't the %v seconds with one picture and %d sound tracks that were written", seconds, tracks)
	}
	if one.VideoTracks != 1 || one.AudioTracks != tracks {
		return fmt.Errorf("piece 2 doesn't hold one picture and %d sound tracks", tracks)
	}
	return nil
}
