// Package diag keeps a short log, in a file of On Air's own, of where the
// live side's words go: what speech recognition was given and gave back,
// what the live side kept and asked, and what the panel was told. Its
// lines hold times, counts, kinds and codes only: never words that were
// said, nothing On the Record answered and no key.
package diag

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Limit is the most a log file holds. Past it, the file is kept as the one
// before, with ".1" added to its name, replacing the last one kept, and a
// new one starts.
var Limit int64 = 1 << 20

var (
	mu   sync.Mutex
	path string
	file *os.File
	size int64
)

// Open starts the log at p, adding to what is there. Until it is called,
// Printf does nothing.
func Open(p string) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		file.Close()
	}
	path, file, size = p, f, info.Size()
	return nil
}

// Printf adds a line: the time, then what format says. It never fails: a
// line that can't be written is left out.
func Printf(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	if file == nil {
		return
	}
	line := time.Now().Format("2006-01-02T15:04:05.000Z07:00") + " " + strings.ReplaceAll(fmt.Sprintf(format, args...), "\n", " ") + "\n"
	if size+int64(len(line)) > Limit {
		file.Close()
		file, size = nil, 0
		_ = os.Rename(path, path+".1")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return
		}
		file = f
	}
	n, _ := file.WriteString(line)
	size += int64(n)
}
