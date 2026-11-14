package spool

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newRecording(t *testing.T) (*Recording, string) {
	t.Helper()
	root := t.TempDir()
	r, err := Create(root, "2026-11-13 18.30")
	if err != nil {
		t.Fatal(err)
	}
	return r, root
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEveryPiecePlaysOnItsOwnAndTheJoinHoldsEachMediaOnce(t *testing.T) {
	r, _ := newRecording(t)
	opening := []byte("OPENING|")
	if err := r.PutOpening(opening); err != nil {
		t.Fatal(err)
	}
	for i, media := range []string{"one|", "two|", "three|"} {
		if err := r.PutPiece(i+1, []byte(media)); err != nil {
			t.Fatal(err)
		}
	}
	pieces, after, err := r.Pieces()
	if err != nil || len(pieces) != 3 || after != 0 {
		t.Fatalf("pieces = %v, after = %d, err = %v", pieces, after, err)
	}
	if got := read(t, pieces[1].Path); string(got) != "OPENING|two|" {
		t.Fatalf("a piece isn't the opening segment and its own media: %q", got)
	}
	dst := filepath.Join(t.TempDir(), "out", "meeting.mp4")
	j, err := r.Join(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); string(got) != "OPENING|one|two|three|" || j.Pieces != 3 || j.Bytes != int64(len(got)) || j.After != 0 {
		t.Fatalf("joined %q, %+v", got, j)
	}
	if again, _, _ := r.Pieces(); len(again) != 3 {
		t.Fatalf("the join changed the pieces: %v", again)
	}
	entries, _ := os.ReadDir(filepath.Dir(dst))
	if len(entries) != 1 {
		t.Fatalf("the join left something beside its file: %v", entries)
	}
}

func TestPiecesArriveInOrderAfterTheOpening(t *testing.T) {
	r, _ := newRecording(t)
	if err := r.PutPiece(1, []byte("m")); err == nil || !strings.Contains(err.Error(), "before the opening") {
		t.Fatalf("a piece before the opening segment: %v", err)
	}
	if err := r.PutOpening(nil); err == nil {
		t.Fatal("an empty opening segment was kept")
	}
	if err := r.PutOpening([]byte("O")); err != nil {
		t.Fatal(err)
	}
	if err := r.PutOpening([]byte("P")); err == nil {
		t.Fatal("the opening segment was written twice")
	}
	if err := r.PutPiece(2, []byte("m")); err == nil || !strings.Contains(err.Error(), "where piece 1 belongs") {
		t.Fatalf("piece 2 first: %v", err)
	}
	if err := r.PutPiece(1, nil); err == nil {
		t.Fatal("an empty piece was kept")
	}
	if err := r.PutPiece(1, []byte("m")); err != nil {
		t.Fatal(err)
	}
	if err := r.PutPiece(1, []byte("m")); err == nil {
		t.Fatal("piece 1 was written twice")
	}
	if got := read(t, filepath.Join(r.Dir, "init.mp4")); string(got) != "O" {
		t.Fatalf("the opening segment changed: %q", got)
	}
}

// What a kill leaves: a part that never got its name, and nothing else odd.
// Only named pieces count, and a missing one ends the run of whole pieces.
func TestOnlyNamedPiecesCountAndAGapEndsTheJoin(t *testing.T) {
	r, _ := newRecording(t)
	if err := r.PutOpening([]byte("O|")); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		if err := r.PutPiece(i, []byte{byte('a' + i - 1), '|'}); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{
		".piece-000003.mp4.part": "O|half",   // cut off before it was named
		"piece-000004.mp4":       "O|d|",     // after a gap
		"piece-5.mp4":            "O|e|",     // not a piece's name
		"piece-000000.mp4":       "O|zero|",  // pieces count from 1
		"notes.txt":              "anything", // not a piece
	} {
		if err := os.WriteFile(filepath.Join(r.Dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(r.Dir, "piece-000003.mp4"), 0o700); err != nil {
		t.Fatal(err)
	}
	pieces, after, err := r.Pieces()
	if err != nil || len(pieces) != 2 || after != 1 {
		t.Fatalf("pieces = %v, after = %d, err = %v", pieces, after, err)
	}
	dst := filepath.Join(t.TempDir(), "meeting.mp4")
	j, err := r.Join(dst)
	if err != nil || j.Pieces != 2 || j.After != 1 || string(read(t, dst)) != "O|a|b|" {
		t.Fatalf("joined %+v, %v, %q", j, err, read(t, dst))
	}
}

func TestAJoinNeverReplacesAFileOrKeepsABadPiece(t *testing.T) {
	r, _ := newRecording(t)
	dir := t.TempDir()
	dst := filepath.Join(dir, "meeting.mp4")
	if _, err := r.Join(dst); err == nil {
		t.Fatal("a recording with no opening segment was joined")
	}
	if err := r.PutOpening([]byte("O|")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Join(dst); err == nil || !strings.Contains(err.Error(), "no whole piece") {
		t.Fatalf("a recording with no piece: %v", err)
	}
	if err := r.PutPiece(1, []byte("a|")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("someone else's"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Join(dst); err == nil || string(read(t, dst)) != "someone else's" {
		t.Fatalf("a file at the join's name was replaced: %v, %q", err, read(t, dst))
	}
	// A piece that doesn't start with this recording's opening segment isn't
	// its piece, and the join stops without leaving a file.
	if err := os.WriteFile(filepath.Join(r.Dir, "piece-000002.mp4"), []byte("X|b|"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other.mp4")
	if _, err := r.Join(other); err == nil || !strings.Contains(err.Error(), "piece 2") {
		t.Fatalf("a foreign piece was joined: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("a failed join left a file: %v", entries)
	}
}

func TestUnfinishedIsWhatAKillLeftBehind(t *testing.T) {
	root := t.TempDir()
	if got, err := Unfinished(filepath.Join(root, "missing")); err != nil || got != nil {
		t.Fatalf("no spool yet: %v, %v", got, err)
	}
	make := func(id string, pieces int, done bool) *Recording {
		r, err := Create(root, id)
		if err != nil {
			t.Fatal(err)
		}
		if pieces >= 0 {
			if err := r.PutOpening([]byte("O|")); err != nil {
				t.Fatal(err)
			}
		}
		for i := 1; i <= pieces; i++ {
			if err := r.PutPiece(i, []byte("m|")); err != nil {
				t.Fatal(err)
			}
		}
		if done {
			if err := r.MarkDone("/somewhere/" + id + ".mp4"); err != nil {
				t.Fatal(err)
			}
		}
		return r
	}
	make("b-cut-short", 2, false)
	make("a-cut-short", 1, false)
	finished := make("c-finished", 2, true)
	make("d-no-pieces", 0, false)
	make("e-empty", -1, false)
	if err := os.WriteFile(filepath.Join(root, "stray.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Unfinished(root)
	if err != nil || len(got) != 2 || filepath.Base(got[0].Dir) != "a-cut-short" || filepath.Base(got[1].Dir) != "b-cut-short" {
		t.Fatalf("unfinished = %v, %v", got, err)
	}
	if path, done := finished.Done(); !done || path != "/somewhere/c-finished.mp4" {
		t.Fatalf("done = %q, %v", path, done)
	}
	if _, err := Create(root, "a-cut-short"); err == nil {
		t.Fatal("two recordings share a directory")
	}
	for _, bad := range []string{"", ".hidden", "a/b", ".."} {
		if _, err := Create(root, bad); err == nil {
			t.Fatalf("a recording was named %q", bad)
		}
	}
}

func TestAPieceIsNeverSeenPartlyWritten(t *testing.T) {
	r, _ := newRecording(t)
	if err := r.PutOpening(bytes.Repeat([]byte("O"), 1024)); err != nil {
		t.Fatal(err)
	}
	if err := r.PutPiece(1, bytes.Repeat([]byte("m"), 1<<20)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Fatalf("a part was left beside its piece: %s", e.Name())
		}
	}
	info, err := os.Stat(filepath.Join(r.Dir, "piece-000001.mp4"))
	if err != nil || info.Size() != 1024+1<<20 || info.Mode().Perm() != 0o600 {
		t.Fatalf("piece: %v, %v", info, err)
	}
}
