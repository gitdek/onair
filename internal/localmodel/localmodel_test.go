package localmodel

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gitdek/onair/internal/live"
)

// The test binary is also a fake helper, run with ONAIR_FAKE_HELPER set to
// how it behaves. It loads nothing.
func TestMain(m *testing.M) {
	if mode := os.Getenv("ONAIR_FAKE_HELPER"); mode != "" {
		os.Exit(fake(mode))
	}
	os.Exit(m.Run())
}

func fake(mode string) int {
	say := func(v any) {
		b, _ := json.Marshal(v)
		os.Stdout.Write(append(b, '\n'))
	}
	switch mode {
	case "fails to load":
		say(map[string]any{"error": "OSError: no snapshot of the model in the cache"})
		return 1
	case "dies loading":
		fmt.Fprintln(os.Stderr, "Segmentation fault")
		return 139
	case "never loads":
		time.Sleep(time.Hour)
	}
	fmt.Println("Fetching 9 files: 100%") // a library, printing before the model loads
	say(map[string]any{"ready": true, "load_ms": 5})
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 64<<10), 1<<20)
	for asked := 1; in.Scan(); asked++ {
		var req struct {
			ID        int64          `json:"id"`
			State     string         `json:"state"`
			Questions map[string]any `json:"questions"`
		}
		json.Unmarshal(in.Bytes(), &req)
		switch mode {
		case "slow":
			time.Sleep(300 * time.Millisecond)
		case "dies":
			fmt.Fprintln(os.Stderr, "Killed")
			return 9
		case "answers once":
			if asked > 1 {
				fmt.Fprintln(os.Stderr, "Killed")
				return 9
			}
		case "fails":
			say(map[string]any{"id": req.ID, "error": "ValueError: the state is too long"})
			continue
		case "nonsense":
			say(map[string]any{"id": req.ID, "answers": map[string]any{"decision": "yes"}})
			continue
		}
		answers := map[string]any{
			"decision":      map[string]any{"type": "noul", "noul": 0.9},
			"stance":        map[string]any{"type": "choice", "choice": "agreed", "probabilities": map[string]float64{"proposed": 0.1, "agreed": 0.8, "taken back": 0.05, "none": 0.05}},
			"topic_changed": map[string]any{"type": "noul", "noul": 0.1},
		}
		if _, ok := req.Questions["differs"]; ok {
			answers["differs"] = map[string]any{"type": "score", "score": 1.6}
		}
		say(map[string]any{"id": req.ID - 1, "answers": map[string]any{}}) // an answer to an earlier question
		say(map[string]any{"id": req.ID, "answers": answers, "ms": 3})
	}
	return 0
}

// faked is a model whose helper is the fake, behaving as mode says.
func faked(mode string) *Model {
	m := New("unused")
	m.argv = []string{os.Args[0], "-test.run=^$"}
	m.env = append(m.env, "ONAIR_FAKE_HELPER="+mode)
	return m
}

var round = live.Round{Text: "Let's lock it in: Thursday at ten.", Records: []live.Link{{Kind: live.MeetingLink, Title: "Demo planning", Date: "2026-11-03", Why: "12:31 “Tuesday works”"}}}

func judge(m *Model, timeout time.Duration) (live.Verdict, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return m.Judge(ctx, round)
}

// loaded asks until the model has loaded, and returns the first answer
// that isn't ErrLoading.
func loaded(t *testing.T, m *Model) (live.Verdict, error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		v, err := judge(m, time.Second)
		if !errors.Is(err, ErrLoading) {
			return v, err
		}
		if time.Now().After(deadline) {
			t.Fatal("the helper never loaded")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestItAsksTheHelperAndReadsItsVerdict(t *testing.T) {
	m := faked("answers")
	defer m.Close()
	v, err := loaded(t, m)
	if err != nil {
		t.Fatal(err)
	}
	if v.Decision != 0.9 || v.Topic != 0.1 || v.Stance["agreed"] != 0.8 || !v.Compared || v.Differs != 0.8 {
		t.Fatalf("read %+v", v)
	}
	// Without a record, nothing is compared.
	v, err = m.Judge(context.Background(), live.Round{Text: "words"})
	if err != nil || v.Compared {
		t.Fatalf("without a record: %+v, %v", v, err)
	}
}

func TestASlowAnswerIsGivenUpOnAndTheNextIsRead(t *testing.T) {
	m := faked("slow")
	defer m.Close()
	loaded(t, m)
	if _, err := judge(m, 50*time.Millisecond); err == nil || err.Error() != "the local model didn't answer in time" {
		t.Fatalf("a slow answer gave %v", err)
	}
	if v, err := judge(m, 2*time.Second); err != nil || v.Decision != 0.9 {
		t.Fatalf("the next answer was %+v, %v", v, err)
	}
}

func TestAHelperThatDiesIsStartedAgainThenGivenUpOn(t *testing.T) {
	m := faked("dies")
	defer m.Close()
	for i := 1; i <= startsAllowed; i++ {
		if _, err := loaded(t, m); err == nil || !strings.Contains(err.Error(), "the local model stopped") || !strings.Contains(err.Error(), "Killed") {
			t.Fatalf("death %d gave %v", i, err)
		}
	}
	if _, err := judge(m, time.Second); err == nil || !strings.Contains(err.Error(), "On Air stopped trying") {
		t.Fatalf("after %d deaths: %v", startsAllowed, err)
	}
	// Chosen again, it is tried again.
	m.Close()
	if _, err := loaded(t, m); err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatalf("after Close: %v", err)
	}
}

// TestAHelperThatKeepsDyingAfterAnsweringIsGivenUpOn checks that the
// starts are bounded however the helper dies: one that answers and then
// dies, again and again, would otherwise load the model afresh every round
// for the rest of a call.
func TestAHelperThatKeepsDyingAfterAnsweringIsGivenUpOn(t *testing.T) {
	m := faked("answers once")
	defer m.Close()
	for round := 1; round <= 4*startsAllowed; round++ {
		if _, err := loaded(t, m); err != nil && strings.Contains(err.Error(), "On Air stopped trying") {
			return
		}
	}
	t.Fatalf("a helper that died after each answer was still started again after %d rounds", 4*startsAllowed)
}

// TestAHelperLeftUnaskedIsStopped checks that a helper nobody asks is
// stopped by itself, not at the next question, which may never come: after
// a call, with On Air running on, the model would otherwise stay in memory.
// Stopping it isn't a failure, so the next question starts another.
func TestAHelperLeftUnaskedIsStopped(t *testing.T) {
	m := faked("answers")
	defer m.Close()
	if _, err := loaded(t, m); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.quiet = 200 * time.Millisecond
	p := m.proc
	m.mu.Unlock()
	if _, err := judge(m, time.Second); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for stopped := false; !stopped; {
		select {
		case _, open := <-p.lines:
			stopped = !open
		case <-deadline:
			t.Fatal("a helper left unasked was still running")
		}
	}
	m.mu.Lock()
	m.quiet = idleQuit
	m.mu.Unlock()
	if v, err := loaded(t, m); err != nil || v.Decision != 0.9 {
		t.Fatalf("after it was stopped for going unasked, the next question gave %+v, %v", v, err)
	}
}

func TestAHelperThatCantLoadSaysWhy(t *testing.T) {
	for mode, says := range map[string]string{
		"fails to load": "the local model didn't load: OSError: no snapshot of the model in the cache",
		"dies loading":  "the local model stopped while loading: Segmentation fault",
	} {
		t.Run(mode, func(t *testing.T) {
			m := faked(mode)
			defer m.Close()
			for i := 0; i < startsAllowed; i++ {
				if _, err := loaded(t, m); err == nil || err.Error() != says {
					t.Fatalf("got %v, not %q", err, says)
				}
			}
			if _, err := judge(m, time.Second); err == nil || !strings.HasPrefix(err.Error(), "the local model couldn't be started, so On Air stopped trying: ") {
				t.Fatalf("after three: %v", err)
			}
		})
	}
	m := New("/nonexistent/python")
	if _, err := judge(m, time.Second); err == nil || !strings.HasPrefix(err.Error(), "the local model couldn't be started: ") {
		t.Fatalf("a missing environment gave %v", err)
	}
}

func TestAnAnswerThatCantBeReadSaysSo(t *testing.T) {
	for mode, says := range map[string]string{
		"fails":    "the local model couldn't answer: ValueError: the state is too long",
		"nonsense": "the local model's answer wasn't in the shape On Air reads",
	} {
		m := faked(mode)
		if _, err := loaded(t, m); err == nil || err.Error() != says {
			t.Fatalf("%s: got %v, not %q", mode, err, says)
		}
		m.Close()
	}
}

func TestStillLoadingSaysSo(t *testing.T) {
	m := faked("never loads")
	defer m.Close()
	if _, err := judge(m, time.Second); !errors.Is(err, ErrLoading) {
		t.Fatalf("a helper still loading gave %v", err)
	}
}

func TestCloseStopsTheHelper(t *testing.T) {
	m := faked("answers")
	loaded(t, m)
	m.mu.Lock()
	p := m.proc
	m.mu.Unlock()
	m.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, open := <-p.lines; !open {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the helper didn't stop")
		}
	}
}

func TestTheQuestionsARoundAsks(t *testing.T) {
	q := questions(round)
	if len(q) != 4 || !strings.Contains(fmt.Sprint(q["differs"]), "Demo planning, 2026-11-03. 12:31 “Tuesday works”") {
		t.Fatalf("asked %v", q)
	}
	if q := questions(live.Round{Text: "words"}); len(q) != 3 || q["differs"] != nil {
		t.Fatalf("without a record asked %v", q)
	}
	if _, err := verdict(map[string]json.RawMessage{"decision": json.RawMessage(`{"noul": 0.5}`)}, false); err == nil {
		t.Fatal("a verdict missing answers was read")
	}
	v, err := verdict(map[string]json.RawMessage{
		"decision":      json.RawMessage(`{"noul": 1.4}`),
		"topic_changed": json.RawMessage(`{"noul": -0.2}`),
		"stance":        json.RawMessage(`{"probabilities": {"agreed": 0.6}}`),
		"differs":       json.RawMessage(`{"score": 2}`),
	}, true)
	if err != nil || v.Decision != 1 || v.Topic != 0 || v.Differs != 1 || !v.Compared {
		t.Fatalf("read %+v, %v", v, err)
	}
}

func TestTheHelperRunsOfflineAndRefusesCode(t *testing.T) {
	for _, want := range []string{`"snapshots", REVISION`, `os.environ["HF_HUB_OFFLINE"] = "1"`, `endswith(".py")`, REVISIONLine} {
		if !strings.Contains(helper, want) {
			t.Fatalf("the helper doesn't hold %q", want)
		}
	}
	if m := New("/x/python"); m.argv[1] != "-I" || !strings.Contains(strings.Join(m.env, " "), "HF_HUB_OFFLINE=1") {
		t.Fatalf("the helper runs as %v with %v", m.argv[:2], m.env)
	}
}

// REVISIONLine pins the model's revision.
const REVISIONLine = `REVISION = "3247d0483b11e5da31fa10e7979c23252fbe3580"`

// TestTheRealHelper runs the real helper, from the environment
// tools/setup-models makes, on made-up words, when ONAIR_LOCAL_MODEL_PYTHON
// names its Python. It is skipped otherwise, as in CI.
func TestTheRealHelper(t *testing.T) {
	python := os.Getenv("ONAIR_LOCAL_MODEL_PYTHON")
	if python == "" {
		t.Skip("ONAIR_LOCAL_MODEL_PYTHON isn't set")
	}
	m := New(python)
	defer m.Close()
	deadline := time.Now().Add(3 * time.Minute)
	for {
		start := time.Now()
		v, err := m.Judge(context.Background(), round)
		if errors.Is(err, ErrLoading) && time.Now().Before(deadline) {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("answered in %v: %+v", time.Since(start), v)
		break
	}
	for i := 0; i < 3; i++ {
		start := time.Now()
		v, err := m.Judge(context.Background(), live.Round{Text: "Shall we move on to hiring? Yes, the contractor budget still isn't approved."})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("answered in %v: %+v", time.Since(start), v)
	}
}
