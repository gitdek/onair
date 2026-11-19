package replay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gitdek/onair/internal/live"
)

// Store keeps what the archive and the models answered in a replay, by
// what they were asked, a file each in a folder of its own that only its
// owner may read.
type Store struct {
	Dir string
	// Asked and Kept count the questions asked for real, and those
	// answered from what was kept.
	Asked, Kept atomic.Int64
}

// kept is an answer as the store keeps it: how long it took, and what
// came back.
type kept struct {
	Took   time.Duration   `json:"took"`
	Err    string          `json:"err,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

func (s *Store) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.Dir, hex.EncodeToString(sum[:16])+".json")
}

func (s *Store) get(key string) (kept, bool) {
	var k kept
	b, err := os.ReadFile(s.path(key))
	if err != nil || json.Unmarshal(b, &k) != nil {
		return kept{}, false
	}
	return k, true
}

func (s *Store) put(key string, k kept) error {
	b, err := json.Marshal(k)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, ".kept-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path(key))
}

// ask answers a question the live side asks of another part. The first
// time, it asks real, with the clock held, and keeps what came back and
// how long it took. Once kept, it answers that once the clock has gone on
// that long since it was asked, as if it had been asked then. What the
// live side allows the question, by ctx's deadline, it allows by the
// clock: an answer that took longer fails with late when the clock gets
// there. The live side giving up on it, by cancelling ctx, ends it at once.
func ask[T any](ctx context.Context, clock *Clock, store *Store, key string, late error, ready func(), real func(context.Context) (T, error)) (T, error) {
	var zero T
	asked := clock.Now()
	allowed := time.Duration(1<<62 - 1)
	if d, ok := ctx.Deadline(); ok {
		allowed = time.Until(d)
	}
	k, ok := store.get(key)
	if ok {
		store.Kept.Add(1)
	} else {
		release := clock.Hold()
		if ready != nil {
			ready()
		}
		began := time.Now()
		v, err := real(ctx)
		took := time.Since(began)
		release()
		if errors.Is(ctx.Err(), context.Canceled) {
			return zero, ctx.Err()
		}
		store.Asked.Add(1)
		k = kept{Took: took}
		if err != nil {
			k.Err = err.Error()
		} else if b, merr := json.Marshal(v); merr == nil {
			k.Result = b
		}
		if perr := store.put(key, k); perr != nil {
			return zero, fmt.Errorf("the replay couldn't keep an answer: %w", perr)
		}
	}
	stop := make(chan struct{})
	cancelled := context.AfterFunc(ctx, func() {
		if errors.Is(ctx.Err(), context.Canceled) {
			close(stop)
		}
	})
	defer cancelled()
	if k.Took > allowed {
		if !clock.Until(asked.Add(allowed), stop) {
			return zero, ctx.Err()
		}
		return zero, late
	}
	if !clock.Until(asked.Add(k.Took), stop) {
		return zero, ctx.Err()
	}
	if k.Err != "" {
		if k.Err == live.ErrLoading.Error() {
			return zero, live.ErrLoading
		}
		return zero, errors.New(k.Err)
	}
	var v T
	if err := json.Unmarshal(k.Result, &v); err != nil {
		return zero, errors.New("the replay couldn't read an answer it kept")
	}
	return v, nil
}

// Archive is On the Record, asked through a replay's store.
type Archive struct {
	Real  live.Archive
	Clock *Clock
	Store *Store
}

var errArchiveLate = errors.New("On the Record didn't answer in time")

func (a Archive) Connections(ctx context.Context, text string) (live.Found, error) {
	return ask(ctx, a.Clock, a.Store, "find_connections\x00"+text, errArchiveLate, nil, func(ctx context.Context) (live.Found, error) {
		return a.Real.Connections(ctx, text)
	})
}

func (a Archive) Answer(ctx context.Context, question string, transcript bool) (live.Brief, error) {
	return ask(ctx, a.Clock, a.Store, fmt.Sprintf("gather_context\x00%t\x00%s", transcript, question), errArchiveLate, nil, func(ctx context.Context) (live.Brief, error) {
		return a.Real.Answer(ctx, question, transcript)
	})
}

// Library asks for the archive's lists through the store, when the real
// archive gives them.
func (a Archive) Library(ctx context.Context) (live.Library, error) {
	l, ok := a.Real.(live.Librarian)
	if !ok {
		return live.Library{}, errors.New("On the Record's lists can't be asked for")
	}
	return ask(ctx, a.Clock, a.Store, "lists", errArchiveLate, nil, func(ctx context.Context) (live.Library, error) {
		return l.Library(ctx)
	})
}

// Writer is the writing model, asked through a replay's store. Its model
// is loaded, with the clock held, before it is first asked for real.
type Writer struct {
	Real  live.Writer
	Clock *Clock
	Store *Store
	once  sync.Once
}

var errModelLate = errors.New("the local model didn't answer in time")

func (w *Writer) Write(ctx context.Context, p live.Prompt) (string, error) {
	key := fmt.Sprintf("write\x00%d\x00%t\x00%s\x00%s", p.Most, p.Quick, p.System, p.User)
	return ask(ctx, w.Clock, w.Store, key, errModelLate, w.load, func(ctx context.Context) (string, error) {
		return w.Real.Write(ctx, p)
	})
}

// load waits for the real model to load, once.
func (w *Writer) load() {
	w.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		w.Real.Write(ctx, live.Prompt{System: "Reply with one word.", User: "Ready?", Most: 1})
	})
}

// Close leaves the real model to whoever made it.
func (w *Writer) Close() {}

// Judge is the decision model, asked through a replay's store, and loaded
// as the writer is.
type Judge struct {
	Real  live.Judge
	Clock *Clock
	Store *Store
	once  sync.Once
}

func (j *Judge) Judge(ctx context.Context, r live.Round) (live.Verdict, error) {
	return ask(ctx, j.Clock, j.Store, "judge\x00"+r.Text, errModelLate, j.load, func(ctx context.Context) (live.Verdict, error) {
		return j.Real.Judge(ctx, r)
	})
}

func (j *Judge) load() {
	j.once.Do(func() {
		deadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(deadline) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, err := j.Real.Judge(ctx, live.Round{Text: "Ready?"})
			cancel()
			if !errors.Is(err, live.ErrLoading) {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	})
}

// Close leaves the real model to whoever made it.
func (j *Judge) Close() {}
