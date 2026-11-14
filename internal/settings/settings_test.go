package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type choice struct {
	Display string   `json:"display,omitempty"`
	Area    *[4]int  `json:"area,omitempty"`
	Names   []string `json:"names,omitempty"`
}

func TestWhatIsSavedIsWhatIsLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "On Air", "settings.json")
	if err := Save(path, choice{Display: "37D8832A", Area: &[4]int{1, 2, 3, 4}}); err != nil {
		t.Fatal(err)
	}
	var got choice
	if err := Load(path, &got); err != nil || got.Display != "37D8832A" || got.Area == nil || *got.Area != [4]int{1, 2, 3, 4} {
		t.Fatalf("loaded %+v, %v", got, err)
	}
	// A second save replaces the first whole, and leaves nothing beside it.
	if err := Save(path, choice{Display: "other"}); err != nil {
		t.Fatal(err)
	}
	got = choice{}
	if err := Load(path, &got); err != nil || got.Display != "other" || got.Area != nil {
		t.Fatalf("loaded %+v, %v", got, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 || entries[0].Name() != "settings.json" {
		t.Fatalf("beside the settings: %v", entries)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the file: %v, %v", info, err)
	}
}

func TestNoFileYetIsTheDefault(t *testing.T) {
	got := choice{Display: "default"}
	if err := Load(filepath.Join(t.TempDir(), "settings.json"), &got); err != nil || got.Display != "default" {
		t.Fatalf("loaded %+v, %v", got, err)
	}
}

func TestAFileThatIsntSettingsSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{\"display\": "), 0o600); err != nil {
		t.Fatal(err)
	}
	var got choice
	if err := Load(path, &got); err == nil || !strings.Contains(err.Error(), "settings.json isn't settings") {
		t.Fatalf("loaded %+v, %v", got, err)
	}
	if err := Load(t.TempDir(), &got); err == nil {
		t.Fatal("a directory was read as settings")
	}
}

// A save that can't finish leaves the old settings as they were.
func TestAFailedSaveKeepsTheOldSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := Save(path, choice{Display: "kept"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	if err := Save(path, choice{Display: "lost"}); err == nil {
		t.Fatal("a save into a folder that can't be written succeeded")
	}
	var got choice
	if err := Load(path, &got); err != nil || got.Display != "kept" {
		t.Fatalf("loaded %+v, %v", got, err)
	}
	if err := Save(path, make(chan int)); err == nil {
		t.Fatal("something that isn't settings was saved")
	}
}
