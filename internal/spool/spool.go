// Package spool keeps a recording's pieces on disk and joins them.
//
// A recording is a directory of its own. The recorder hands it one opening
// segment, which describes the tracks, and then media segments a few seconds
// long. Each media segment is stored as a piece: the opening segment followed
// by the media, so every piece plays on its own. A piece gets its name only
// once its bytes are on disk, so a name that exists is a whole piece, whatever
// happened to the process that wrote it (docs/PRD.md, rules 3 and 4).
package spool

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	openingName = "init.mp4"
	doneName    = "done"
	piecePrefix = "piece-"
	pieceSuffix = ".mp4"
)

// Recording is one recording's directory in the spool.
type Recording struct {
	Dir string
}

// Piece is one whole piece, in the order it was written.
type Piece struct {
	Number int
	Path   string
	Bytes  int64
}

// Create makes a new recording's directory under root. It refuses a name
// that exists, so two recordings never share pieces.
func Create(root, id string) (*Recording, error) {
	if id == "" || id != filepath.Base(id) || strings.HasPrefix(id, ".") {
		return nil, fmt.Errorf("a recording's name can't be %q", id)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	dir := filepath.Join(root, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return nil, err
	}
	return &Recording{Dir: dir}, syncDir(root)
}

// Open returns the recording kept in dir.
func Open(dir string) *Recording { return &Recording{Dir: dir} }

// PutOpening stores the opening segment. It is written once.
func (r *Recording) PutOpening(opening []byte) error {
	if len(opening) == 0 {
		return errors.New("the opening segment is empty")
	}
	if _, err := os.Lstat(filepath.Join(r.Dir, openingName)); err == nil {
		return errors.New("this recording has its opening segment already")
	}
	return r.put(openingName, opening)
}

// PutPiece stores media segment number n, counted from 1, as a piece that
// plays on its own. Pieces arrive in order: n must follow the last one.
func (r *Recording) PutPiece(n int, media []byte) error {
	if len(media) == 0 {
		return fmt.Errorf("piece %d is empty", n)
	}
	opening, err := os.ReadFile(filepath.Join(r.Dir, openingName))
	if err != nil {
		return fmt.Errorf("piece %d came before the opening segment: %w", n, err)
	}
	pieces, _, err := r.Pieces()
	if err != nil {
		return err
	}
	if want := len(pieces) + 1; n != want {
		return fmt.Errorf("piece %d came where piece %d belongs", n, want)
	}
	return r.put(pieceName(n), opening, media)
}

// Pieces lists the whole pieces from the first, in order, up to the first
// one that is missing or isn't this recording's. after counts the files
// named as later pieces that lie beyond that point, which a join leaves out.
func (r *Recording) Pieces() (pieces []Piece, after int, err error) {
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		return nil, 0, err
	}
	found := map[int]Piece{}
	for _, e := range entries {
		n, ok := pieceNumber(e.Name())
		if !ok || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, 0, err
		}
		found[n] = Piece{Number: n, Path: filepath.Join(r.Dir, e.Name()), Bytes: info.Size()}
	}
	for n := 1; ; n++ {
		p, ok := found[n]
		if !ok {
			break
		}
		pieces = append(pieces, p)
	}
	return pieces, len(found) - len(pieces), nil
}

// Joined says what a join wrote.
type Joined struct {
	Path   string
	Pieces int
	Bytes  int64
	// After counts pieces left out because an earlier one is missing.
	After int
}

// Join writes the recording's whole pieces to dst as one file: the opening
// segment once, then each piece's media in order. dst gets its name only
// when it is whole, and a file already at dst is never replaced. The pieces
// are left as they are.
func (r *Recording) Join(dst string) (Joined, error) {
	opening, err := os.ReadFile(filepath.Join(r.Dir, openingName))
	if err != nil {
		return Joined{}, fmt.Errorf("nothing to join: %w", err)
	}
	pieces, after, err := r.Pieces()
	if err != nil {
		return Joined{}, err
	}
	if len(pieces) == 0 {
		return Joined{}, errors.New("nothing to join: the recording has no whole piece")
	}
	if _, err := os.Lstat(dst); err == nil {
		return Joined{}, fmt.Errorf("%s exists already", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return Joined{}, err
	}
	tmp := filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+".part")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return Joined{}, err
	}
	fail := func(err error) (Joined, error) {
		f.Close()
		os.Remove(tmp)
		return Joined{}, err
	}
	written, err := f.Write(opening)
	if err != nil {
		return fail(err)
	}
	total := int64(written)
	for _, p := range pieces {
		n, err := appendMedia(f, p.Path, opening)
		if err != nil {
			return fail(fmt.Errorf("piece %d: %w", p.Number, err))
		}
		total += n
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return Joined{}, err
	}
	// Link, not rename: a rename would replace a file that appeared at dst
	// since the check above.
	if err := os.Link(tmp, dst); err != nil {
		os.Remove(tmp)
		return Joined{}, err
	}
	os.Remove(tmp)
	return Joined{Path: dst, Pieces: len(pieces), Bytes: total, After: after}, syncDir(filepath.Dir(dst))
}

// appendMedia copies a piece's media, the part after its opening segment.
func appendMedia(dst io.Writer, path string, opening []byte) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	head := make([]byte, len(opening))
	if _, err := io.ReadFull(f, head); err != nil || !bytes.Equal(head, opening) {
		return 0, errors.New("it doesn't start with this recording's opening segment")
	}
	return io.Copy(dst, f)
}

// MarkDone records that the recording was joined into the file at path, so
// it isn't taken for one that was cut short.
func (r *Recording) MarkDone(path string) error {
	return r.put(doneName, []byte(path+"\n"))
}

// Done reports whether the recording was joined, and into which file.
func (r *Recording) Done() (path string, done bool) {
	b, err := os.ReadFile(filepath.Join(r.Dir, doneName))
	if err != nil {
		return "", false
	}
	return strings.TrimSuffix(string(b), "\n"), true
}

// Unfinished lists the recordings under root that have at least one whole
// piece and were never joined, oldest name first: what a crash left behind.
func Unfinished(root string) ([]*Recording, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Recording
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		r := Open(filepath.Join(root, e.Name()))
		if _, done := r.Done(); done {
			continue
		}
		if pieces, _, err := r.Pieces(); err != nil || len(pieces) == 0 {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out, nil
}

// put writes name in the recording's directory from parts. The bytes go to a
// hidden name first and are flushed to disk, and only then does the file get
// its name, so a reader never sees a partial file under it.
func (r *Recording) put(name string, parts ...[]byte) error {
	tmp := filepath.Join(r.Dir, "."+name+".part")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	for _, p := range parts {
		if _, err := f.Write(p); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, filepath.Join(r.Dir, name)); err != nil {
		os.Remove(tmp)
		return err
	}
	return syncDir(r.Dir)
}

func pieceName(n int) string { return fmt.Sprintf("%s%06d%s", piecePrefix, n, pieceSuffix) }

func pieceNumber(name string) (int, bool) {
	digits, ok := strings.CutPrefix(name, piecePrefix)
	if !ok {
		return 0, false
	}
	digits, ok = strings.CutSuffix(digits, pieceSuffix)
	if !ok || len(digits) != 6 {
		return 0, false
	}
	n := 0
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, n > 0
}

// syncDir flushes a directory, so a name given in it survives a power loss.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
