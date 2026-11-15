// Package localmodel asks a model on this Mac typed questions about a
// call: Liquid's d1-2B, which a helper process loads once and asks through
// mlx-vlm. The helper speaks JSON lines over its standard input and output,
// and has no network port. It runs from the Python environment that
// tools/setup-models makes, uses the Hugging Face cache offline, and never
// downloads anything. On Air starts it when it is first asked, starts it
// again if it dies, and gives up after three failures, answers in between
// or not, until it is closed and chosen again. One left unasked for half
// an hour is stopped. It decides nothing: which questions to ask is here,
// and what to make of the answers is internal/live's.
package localmodel

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gitdek/onair/internal/live"
)

//go:embed helper.py
var helper string

// Limits on the helper.
const (
	startsAllowed = 3                // helpers that may fail to start, load or keep running before it gives up
	loadTimeout   = 2 * time.Minute  // for the model to load
	maxLine       = 1 << 20          // bytes of one line from the helper
	keptErrors    = 2048             // bytes of its standard error kept, to say why it stopped
	idleQuit      = 30 * time.Minute // a helper that isn't asked anything for this long is stopped
)

// Model is the local model, behind its helper. Several goroutines may use
// it at once: one question goes to the helper at a time.
type Model struct {
	argv []string // the helper's command
	env  []string

	turn chan struct{} // held while a question is with the helper

	mu      sync.Mutex
	proc    *helperProcess
	failed  int    // helpers that failed since Close
	why     string // why the last start failed
	gaveUp  bool
	asked   int64
	lastUse time.Time
	quiet   time.Duration // how long it may go unasked: idleQuit, or less in a test
	idle    *time.Timer   // stops a helper left unasked for quiet
}

// New returns the model behind a helper run by python, the Python of the
// environment tools/setup-models makes.
func New(python string) *Model {
	env := []string{"PATH=/usr/bin:/bin", "HF_HUB_OFFLINE=1", "TRANSFORMERS_OFFLINE=1", "HF_HUB_DISABLE_TELEMETRY=1", "TOKENIZERS_PARALLELISM=false"}
	if home, err := os.UserHomeDir(); err == nil {
		env = append(env, "HOME="+home)
	}
	return &Model{argv: []string{python, "-I", "-c", helper}, env: env, turn: make(chan struct{}, 1), quiet: idleQuit}
}

// helperProcess is one run of the helper.
type helperProcess struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	lines  chan []byte   // what it writes, a line at a time; closed when it ends
	ready  chan struct{} // closed once the model is loaded
	failed error         // why it never got ready, once ready is closed with it set
	stderr *tail
}

// tail keeps the end of what was written to it.
type tail struct {
	mu sync.Mutex
	b  []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > keptErrors {
		t.b = t.b[len(t.b)-keptErrors:]
	}
	return len(p), nil
}

func (t *tail) last() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(string(t.b)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// ErrLoading says the model is still loading, which takes a few seconds
// after the helper starts.
var ErrLoading = errors.New("the local model is still loading")

// Judge asks the helper the typed questions for a round, and reads its
// answers as a verdict.
func (m *Model) Judge(ctx context.Context, r live.Round) (live.Verdict, error) {
	select {
	case m.turn <- struct{}{}:
	case <-ctx.Done():
		return live.Verdict{}, errors.New("the local model was busy with the last question")
	}
	defer func() { <-m.turn }()
	p, err := m.running()
	if err != nil {
		return live.Verdict{}, err
	}
	select {
	case <-p.ready:
		if p.failed != nil {
			return live.Verdict{}, m.lost(p, p.failed)
		}
	default:
		return live.Verdict{}, ErrLoading
	}
	m.mu.Lock()
	m.asked++
	id := m.asked
	m.lastUse = time.Now()
	m.watch()
	m.mu.Unlock()
	request, err := json.Marshal(map[string]any{"id": id, "state": r.Text, "questions": questions(r)})
	if err != nil {
		return live.Verdict{}, err
	}
	if _, err := p.in.Write(append(request, '\n')); err != nil {
		return live.Verdict{}, m.lost(p, fmt.Errorf("the local model stopped: %w", err))
	}
	for {
		select {
		case <-ctx.Done():
			return live.Verdict{}, errors.New("the local model didn't answer in time")
		case line, open := <-p.lines:
			if !open {
				return live.Verdict{}, m.lost(p, fmt.Errorf("the local model stopped%s", p.why()))
			}
			var a struct {
				ID      int64                      `json:"id"`
				Answers map[string]json.RawMessage `json:"answers"`
				Error   string                     `json:"error"`
			}
			if json.Unmarshal(line, &a) != nil || a.ID != id {
				// An answer to a question given up on, or something else.
				continue
			}
			if a.Error != "" {
				return live.Verdict{}, fmt.Errorf("the local model couldn't answer: %s", a.Error)
			}
			// An answer doesn't start the count of failures over: a
			// helper that answers and then dies, again and again, would
			// otherwise load the model afresh every round.
			return verdict(a.Answers, len(r.Records) > 0)
		}
	}
}

// running is the helper, started if it isn't running. It starts it once
// more each time one dies, and not after startsAllowed have failed since
// Close.
func (m *Model) running() (*helperProcess, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.proc != nil {
		if !m.lastUse.IsZero() && time.Since(m.lastUse) > m.quiet {
			m.proc.stop()
			m.proc = nil
		} else {
			return m.proc, nil
		}
	}
	if m.gaveUp {
		return nil, fmt.Errorf("the local model couldn't be started, so On Air stopped trying: %s", m.why)
	}
	p, err := start(m.argv, m.env)
	if err != nil {
		m.fail(err)
		return nil, fmt.Errorf("the local model couldn't be started: %w", err)
	}
	m.proc, m.lastUse = p, time.Now()
	m.watch()
	return p, nil
}

// watch sets the timer that stops a helper once it has gone unasked for
// m.quiet, so it goes by itself after a call, with no question to come.
// The lock is held.
func (m *Model) watch() {
	if m.idle == nil {
		m.idle = time.AfterFunc(m.quiet, m.stopIdle)
		return
	}
	m.idle.Reset(m.quiet)
}

// stopIdle stops the helper if it has gone unasked for m.quiet, or looks
// again when it would have. Stopping it isn't a failure: the next question
// starts another.
func (m *Model) stopIdle() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.proc == nil {
		return
	}
	if left := m.quiet - time.Since(m.lastUse); left > 0 {
		m.idle.Reset(left)
		return
	}
	m.proc.stop()
	m.proc = nil
}

// fail counts a helper that couldn't start, didn't load or stopped, until
// one answers. The lock is held.
func (m *Model) fail(err error) {
	m.failed++
	m.why = err.Error()
	m.gaveUp = m.failed >= startsAllowed
}

// lost lets go of a helper that stopped or never got ready, so the next
// question starts another, and says why.
func (m *Model) lost(p *helperProcess, err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.proc == p {
		m.proc = nil
		p.stop()
		m.fail(err)
	}
	return err
}

// Close stops the helper, and lets the next question start it again.
func (m *Model) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.proc != nil {
		m.proc.stop()
		m.proc = nil
	}
	if m.idle != nil {
		m.idle.Stop()
	}
	m.failed, m.why, m.gaveUp = 0, "", false
}

// start starts the helper, and waits for it to load the model in the
// background.
func start(argv, env []string) (*helperProcess, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Dir = os.TempDir()
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p := &helperProcess{cmd: cmd, in: in, lines: make(chan []byte, 4), ready: make(chan struct{}), stderr: &tail{}}
	cmd.Stderr = p.stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		defer close(p.lines)
		reader := bufio.NewReaderSize(out, 64<<10)
		readyDone := false
		timer := time.AfterFunc(loadTimeout, func() { p.stop() })
		defer timer.Stop()
		for {
			line, err := readLine(reader)
			if err != nil {
				if !readyDone {
					p.failed = fmt.Errorf("the local model stopped while loading%s", p.why())
					close(p.ready)
				}
				cmd.Wait()
				return
			}
			if !readyDone {
				var hello struct {
					Ready bool   `json:"ready"`
					Error string `json:"error"`
				}
				if json.Unmarshal(line, &hello) != nil || !hello.Ready && hello.Error == "" {
					// Something a library printed before the model loaded.
					continue
				}
				timer.Stop()
				readyDone = true
				if !hello.Ready {
					p.failed = fmt.Errorf("the local model didn't load: %s", hello.Error)
				}
				close(p.ready)
				continue
			}
			select {
			case p.lines <- line:
			default:
				// Nothing waits for it: an answer to a question given up on.
			}
		}
	}()
	return p, nil
}

// readLine reads a line of at most maxLine bytes, and skips the rest of a
// longer one.
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, more, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		if len(line)+len(part) <= maxLine {
			line = append(line, part...)
		}
		if !more {
			return line, nil
		}
	}
}

// why is what the helper last wrote to its standard error, if anything.
func (p *helperProcess) why() string {
	if last := p.stderr.last(); last != "" {
		if len(last) > 300 {
			last = last[:300]
		}
		return ": " + last
	}
	return ""
}

func (p *helperProcess) stop() {
	p.in.Close()
	if p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
}
