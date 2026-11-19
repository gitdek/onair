package replay

import (
	"path/filepath"
	"strings"
	"testing"
)

// A replay keeps what it plays and shows in a folder of its own, in On
// Air's own folder, named for the transcript: never beside the transcript,
// in the repository it may be run from, or anywhere a name leads.
func TestAReplayKeepsItsFilesInItsOwnFolder(t *testing.T) {
	support := filepath.Join("/home", "someone", "Library", "Application Support", "On Air")
	replays := filepath.Join(support, "replays")
	inside := func(path, dir string) bool {
		rel, err := filepath.Rel(dir, path)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	dir, err := Folder(support, filepath.Join("/somewhere", "else", "2026-03-02 10.00.00 On Air.whisper.json"))
	if err != nil || dir != filepath.Join(replays, "2026-03-02 10.00.00 On Air") {
		t.Fatalf("the folder is %q, %v", dir, err)
	}
	if Kept(support) != filepath.Join(replays, "kept") {
		t.Errorf("answers are kept in %q", Kept(support))
	}
	// A transcript whose name leaves nothing plain to name the folder by,
	// or names the store's, is refused.
	for _, transcript := range []string{"/x/.json", "/x/..", "/x/.hidden.json", "/x/kept.json", "/"} {
		if got, err := Folder(support, transcript); err == nil {
			t.Errorf("a replay of %q kept in %q", transcript, got)
		}
	}
	run, err := RunFile(dir, "after")
	if err != nil || run != filepath.Join(dir, "after.jsonl") {
		t.Errorf("the run is kept in %q, %v", run, err)
	}
	report, err := ReportFile(dir, []string{"before", "after"})
	if err != nil || report != filepath.Join(dir, "before and after.txt") {
		t.Errorf("the timeline is written to %q, %v", report, err)
	}
	for _, name := range []string{"", ".", "..", ".hidden", "../../../../projects/onair/timeline", `..\x`, "a/b", "a\x00b"} {
		if got, err := RunFile(dir, name); err == nil {
			t.Errorf("a run named %q is kept in %q", name, got)
		}
		for _, names := range [][]string{{name}, {"before", name}} {
			if got, err := ReportFile(dir, names); err == nil {
				t.Errorf("a report of %q is written to %q", names, got)
			}
		}
	}
	for _, path := range []string{run, report} {
		if !inside(path, dir) {
			t.Errorf("%q is outside %q", path, dir)
		}
	}
}
