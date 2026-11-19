package replay

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// keptFolder is the folder in replays that the store keeps answers in.
const keptFolder = "kept"

// Folder is the folder a replay of a transcript keeps its runs and
// timelines in: one named for the transcript, in replays in support, On
// Air's own folder, and never beside the transcript. A transcript whose
// name leaves nothing plain to name it by, or names the store's folder, is
// refused.
func Folder(support, transcript string) (string, error) {
	stem := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(transcript), ".json"), ".whisper")
	if plain(stem) != nil || stem == keptFolder {
		return "", fmt.Errorf("a replay's folder is named for its transcript, and %q can't name one", filepath.Base(transcript))
	}
	return filepath.Join(support, "replays", stem), nil
}

// Kept is the folder the store keeps what the archive and the models
// answered in, in support.
func Kept(support string) string { return filepath.Join(support, "replays", keptFolder) }

// RunFile is the file a run named name is kept in, in dir.
func RunFile(dir, name string) (string, error) {
	if err := plain(name); err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".jsonl"), nil
}

// ReportFile is the file the timeline of the runs named is written to, in
// dir.
func ReportFile(dir string, names []string) (string, error) {
	if len(names) == 0 {
		return "", plain("")
	}
	for _, name := range names {
		if err := plain(name); err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, strings.Join(names, " and ")+".txt"), nil
}

// plain says why a name can't name a replay's file or folder, or nothing
// when it can: it has a letter or a digit, and isn't hidden, nor a part
// of a path.
func plain(name string) error {
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	if strings.HasPrefix(name, ".") || strings.ContainsAny(name, "/\\\x00") || !strings.ContainsFunc(name, word) {
		return fmt.Errorf("a run's name is a plain word, not %q", name)
	}
	return nil
}
