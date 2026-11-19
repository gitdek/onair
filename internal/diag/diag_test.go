package diag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheLogStaysBounded(t *testing.T) {
	Printf("before the log is open, nothing is written")
	p := filepath.Join(t.TempDir(), "live-diag.log")
	limit := Limit
	Limit = 200
	defer func() {
		mu.Lock()
		file.Close()
		file, Limit = nil, limit
		mu.Unlock()
	}()
	if err := Open(p); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		Printf("voice %s: %d buffers\nin", "mic", i)
	}
	now, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := os.ReadFile(p + ".1")
	if err != nil {
		t.Fatalf("nothing was kept from before: %v", err)
	}
	if len(now) > 200 || len(kept) > 200 {
		t.Fatalf("the log holds %d bytes and the one kept %d, past 200", len(now), len(kept))
	}
	lines := strings.Split(strings.TrimSuffix(string(now), "\n"), "\n")
	if last := lines[len(lines)-1]; !strings.HasSuffix(last, "voice mic: 19 buffers in") {
		t.Fatalf("the last line is %q", last)
	}
}
