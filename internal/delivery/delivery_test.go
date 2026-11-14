package delivery

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gitdek/onair/core/handover/handover"
)

const file = "2026-11-14 10.00.00 On Air.mp4"

// start is when the first run happens, on the clock the tests set.
var start = time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)

// errKilled stops a run where a kill would.
var errKilled = errors.New("killed")

// place is a recording in On Air's spool and the folder it goes to, with
// another program's files in the folder that On Air must never touch.
type place struct {
	t      *testing.T
	spool  string // the recording's directory
	folder string
	movie  []byte // On Air's own copy
	others map[string]entry
}

type entry struct {
	mode  fs.FileMode
	data  string
	mtime time.Time
}

func newPlace(t *testing.T) *place {
	t.Helper()
	root := t.TempDir()
	p := &place{t: t, spool: filepath.Join(root, "spool", "2026-11-14 10.00.00"), folder: filepath.Join(root, "On the Record"),
		movie: bytes.Repeat([]byte("frame|sound|"), 40)}
	p.write(filepath.Join(p.spool, file), p.movie)
	p.write(filepath.Join(p.spool, "piece-000001.mp4"), []byte("a piece"))
	p.write(filepath.Join(p.folder, "2026-11-13 09.00.00 Standup.mp4"), []byte("another recording"))
	p.write(filepath.Join(p.folder, ".DS_Store"), []byte("the Finder's"))
	p.write(filepath.Join(p.folder, "Archive", "old.mp4"), []byte("an older recording"))
	p.others = p.look()
	return p
}

func (p *place) write(path string, data []byte) {
	p.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		p.t.Fatal(err)
	}
}

// look lists what is in the folder besides the hand-over's own two names.
func (p *place) look() map[string]entry {
	p.t.Helper()
	seen := map[string]entry{}
	filepath.WalkDir(p.folder, func(path string, e fs.DirEntry, err error) error {
		if err != nil || path == p.folder {
			return nil
		}
		rel, _ := filepath.Rel(p.folder, path)
		if rel == file || rel == "."+file {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			p.t.Fatal(err)
		}
		data := ""
		if info.Mode().IsRegular() {
			b, _ := os.ReadFile(path)
			data = string(b)
		}
		seen[rel] = entry{mode: info.Mode(), data: data, mtime: info.ModTime()}
		return nil
	})
	return seen
}

// untouched fails the test if anything in the folder but the hand-over's
// own names changed, appeared or went (docs/PRD.md, rule 11).
func (p *place) untouched() {
	p.t.Helper()
	now := p.look()
	for name, was := range p.others {
		if got, ok := now[name]; !ok || got != was {
			p.t.Fatalf("%s in the folder was changed: %+v, was %+v", name, got, was)
		}
	}
	for name := range now {
		if _, ok := p.others[name]; !ok {
			p.t.Fatalf("%s appeared in the folder", name)
		}
	}
}

func (p *place) final() string  { return filepath.Join(p.folder, file) }
func (p *place) hidden() string { return filepath.Join(p.folder, "."+file) }
func (p *place) own() string    { return filepath.Join(p.spool, file) }

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// noPartial fails the test if anything but the whole recording is under its
// name in the folder (docs/PRD.md, rule 5).
func (p *place) noPartial() {
	p.t.Helper()
	got, err := os.ReadFile(p.final())
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil || !bytes.Equal(got, p.movie) {
		p.t.Fatalf("the folder shows %d bytes under the recording's name, not the recording: %v", len(got), err)
	}
}

// delivered fails the test unless the folder holds the recording, whole,
// under its name and nowhere else, and nothing else there was touched.
func (p *place) delivered(r Report) {
	p.t.Helper()
	if !r.Delivered || r.Err != nil || r.Failed != "" || r.Path != p.final() || r.Folder != p.folder || r.File != file {
		p.t.Fatalf("the report: %+v", r)
	}
	if got, err := os.ReadFile(p.final()); err != nil || !bytes.Equal(got, p.movie) {
		p.t.Fatalf("the folder's file: %d bytes, %v", len(got), err)
	}
	if exists(p.hidden()) {
		p.t.Fatal("the hidden copy was left in the folder")
	}
	p.untouched()
}

// prepare starts a process's hand-over, small chunks at a time.
func (p *place) prepare(fault func(string) error) (*Delivery, error) {
	p.t.Helper()
	d, err := Prepare(p.spool, file)
	if d != nil {
		d.fault, d.chunk = fault, 64
	}
	return d, err
}

func (p *place) run(fault func(string) error, now time.Time) Report {
	p.t.Helper()
	d, err := p.prepare(fault)
	if err != nil {
		p.t.Fatal(err)
	}
	return d.Run(p.folder, now)
}

// trail logs the points a run passes, and can act at one.
type trail struct {
	log []string
	at  map[string]func() error // what to do at a point, by its name
}

func (s *trail) fault(point string) error {
	s.log = append(s.log, point)
	if f := s.at[point]; f != nil {
		return f()
	}
	return nil
}

// did lists the steps carried out, in order.
func (s *trail) did() []string {
	var did []string
	for _, p := range s.log {
		if strings.HasPrefix(p, "do ") {
			did = append(did, p)
		}
	}
	return did
}

func (s *trail) count(point string) int {
	n := 0
	for _, p := range s.log {
		if p == point {
			n++
		}
	}
	return n
}

func TestARecordingIsHandedOverWholeAndOnAirsCopyGoesThreeDaysLater(t *testing.T) {
	p := newPlace(t)
	s := &trail{}
	r := p.run(s.fault, start)
	p.delivered(r)
	if !r.Handed || r.Removed || !exists(p.own()) {
		t.Fatalf("the report: %+v", r)
	}
	if s.count("do copy") != 1 || s.count("do compareHidden") != 1 || s.count("do rename") != 1 {
		t.Fatalf("the steps: %v", s.log)
	}
	before, _ := os.Stat(p.final())
	// Carried on later, by another process, it is done: nothing is copied
	// again, and On Air's own copy stays until its days are up.
	for _, at := range []time.Time{start, start.Add(time.Hour), start.Add(Keep - time.Second)} {
		again := &trail{}
		r := p.run(again.fault, at)
		p.delivered(r)
		if r.Handed || r.Removed || !exists(p.own()) || again.count("do copy")+again.count("do removeSpool") != 0 {
			t.Fatalf("at %v: %+v, %v", at, r, again.log)
		}
	}
	r = p.run(nil, start.Add(Keep))
	p.delivered(r)
	if !r.Removed || r.Handed || exists(p.own()) {
		t.Fatalf("three days on: %+v", r)
	}
	if after, _ := os.Stat(p.final()); !os.SameFile(before, after) {
		t.Fatal("the folder's file was replaced")
	}
	// The pieces stay where they are: when they go is for the pieces' core.
	if got, _ := os.ReadFile(filepath.Join(p.spool, "piece-000001.mp4")); string(got) != "a piece" {
		t.Fatalf("a piece is %q", got)
	}
	d, ok, err := Load(p.spool)
	if !ok || err != nil || !d.Done() {
		t.Fatalf("the record: %v, %v, %+v", ok, err, d)
	}
	r = d.Run(p.folder, start.Add(30*24*time.Hour))
	p.delivered(r)
	if !r.Removed {
		t.Fatalf("once done: %+v", r)
	}
}

// The core's record is on disk before its step is carried out, and each
// step's result is on disk before the core is told.
func TestTheRecordAndEachResultAreKeptBeforeTheyAreActedOn(t *testing.T) {
	p := newPlace(t)
	onDisk := func(name string) record {
		var k record
		b, err := os.ReadFile(filepath.Join(p.spool, name))
		if err != nil || json.Unmarshal(b, &k) != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return k
	}
	kinds := map[string]handover.Kind{"do compareNamed": handover.CompareNamed, "do copy": handover.Copy,
		"do compareHidden": handover.CompareHidden, "do rename": handover.Rename, "do removeSpool": handover.RemoveSpool}
	checked := 0
	fault := func(point string) error {
		if kind, ok := kinds[point]; ok {
			if got := onDisk(recordName).Core.Record.Intent; got.Kind != kind {
				t.Fatalf("at %q the kept record asks for %+v", point, got)
			}
			checked++
		}
		if point == "keeping" && exists(filepath.Join(p.spool, recordName)) {
			was, next := onDisk(recordName), onDisk("."+recordName+".part")
			told := was.Core.Record.Intent
			if told != (handover.Request{}) && next.Core.Record.Intent != told && (was.Result == nil || was.Result.Request != told) {
				t.Fatalf("the core was told what %+v came to before that was kept", told)
			}
		}
		return nil
	}
	p.delivered(p.run(fault, start))
	if r := p.run(fault, start.Add(Keep)); !r.Removed {
		t.Fatalf("three days on: %+v", r)
	}
	if checked != 5 {
		t.Fatalf("%d steps were checked", checked)
	}
}

// A kill at any point of a hand-over leaves no partial file under the
// recording's name, and the next process carries it on from where it stood,
// without copying the recording again once it has its name.
func TestAKillAtEveryStepIsCarriedOnWithoutASecondCopy(t *testing.T) {
	points := 0
	for k := 1; ; k++ {
		p := newPlace(t)
		s := &trail{}
		calls, killed := 0, ""
		fault := func(point string) error {
			s.fault(point)
			if calls++; calls == k {
				killed = point
				return errKilled
			}
			return nil
		}
		if d, err := p.prepare(fault); err == nil {
			d.Run(p.folder, start)
		}
		if killed == "" {
			points = k - 1
			p.delivered(p.run(nil, start))
			break
		}
		p.noPartial()
		if a, err := os.Lstat(p.final()); err == nil {
			if b, err := os.Lstat(p.hidden()); err == nil && !os.SameFile(a, b) {
				t.Fatalf("killed at %q: two copies in the folder", killed)
			}
		}
		r := p.run(s.fault, start)
		p.delivered(r)
		if !exists(p.own()) {
			t.Fatalf("killed at %q: On Air's own copy went with the hand-over", killed)
		}
		// Once the copy was given its name, nothing is copied again, and a
		// copy is made again only when the kill came before it was kept.
		if i := slices.Index(s.log, "linked"); i >= 0 && slices.Contains(s.log[i:], "do copy") {
			t.Fatalf("killed at %q: copied again after the copy had its name: %v", killed, s.log)
		}
		if n := s.count("do copy"); n > 2 {
			t.Fatalf("killed at %q: %d copies: %v", killed, n, s.log)
		}
	}
	if points < 30 {
		t.Fatalf("only %d points were tried", points)
	}
	t.Logf("a kill at each of %d points was carried on", points)
}

// A step asked for again gets the result it first had. The publication is
// the one that matters: cut short between linking the recording's name and
// letting the hidden one go, it finishes as the same step. Cut short once
// the hidden name is gone, the name is found taken, and the file under it is
// compared and taken as the hand-over's own. Once what it came to was kept,
// it isn't carried out again at all. None of them copies again.
func TestAPublicationCutShortIsTheHandOversOwn(t *testing.T) {
	for _, c := range []struct {
		at   string   // the kill comes here, once the publication is asked for
		then []string // the steps the next process carries out
	}{
		{"do rename", []string{"do rename"}},
		{"linked", []string{"do rename"}},
		{"published", []string{"do rename", "do compareNamed"}},
		{"kept", nil},
	} {
		p := newPlace(t)
		asked, killed := false, false
		r := p.run(func(point string) error {
			asked = asked || point == "do rename"
			if asked && !killed && point == c.at {
				killed = true
				return errKilled
			}
			return nil
		}, start)
		if !killed || r.Delivered {
			t.Fatalf("the kill at %q didn't come: %+v", c.at, r)
		}
		p.noPartial()
		s := &trail{}
		p.delivered(p.run(s.fault, start))
		if got := s.did(); !slices.Equal(got, c.then) {
			t.Fatalf("killed at %q, the next process did %v", c.at, got)
		}
	}
}

// A kill while On Air's own copy is removed leaves the hand-over done, and
// the next process removes it.
func TestAKillWhileOnAirsCopyIsRemovedIsCarriedOn(t *testing.T) {
	for k := 1; ; k++ {
		p := newPlace(t)
		p.delivered(p.run(nil, start))
		calls, killed := 0, ""
		r := p.run(func(point string) error {
			if calls++; calls == k {
				killed = point
				return errKilled
			}
			return nil
		}, start.Add(Keep))
		if killed == "" {
			if !r.Removed || exists(p.own()) {
				t.Fatalf("without a kill: %+v", r)
			}
			break
		}
		p.delivered(p.load())
		r = p.run(nil, start.Add(Keep))
		p.delivered(r)
		if !r.Removed || exists(p.own()) {
			t.Fatalf("killed at %q, then: %+v", killed, r)
		}
	}
}

// A file under the recording's name is compared with the recording. The
// same bytes count as handed over, without a copy, and different ones are
// left exactly as they are, and fail the hand-over with a name conflict.
func TestANameThatIsTakenIsLeftAlone(t *testing.T) {
	p := newPlace(t)
	p.write(p.final(), []byte("a different recording"))
	p.others = p.look()
	before, _ := os.Lstat(p.final())
	s := &trail{}
	r := p.run(s.fault, start)
	if r.Delivered || !strings.Contains(r.Failed, "different file has its name") || r.Path != p.own() || r.Err != nil {
		t.Fatalf("the report: %+v", r)
	}
	if got, _ := os.ReadFile(p.final()); string(got) != "a different recording" {
		t.Fatalf("the file under the name is %q", got)
	}
	if after, _ := os.Lstat(p.final()); !os.SameFile(before, after) || after.ModTime() != before.ModTime() {
		t.Fatal("the file under the name was touched")
	}
	if s.count("do copy") != 0 || exists(p.hidden()) || !exists(p.own()) {
		t.Fatalf("the steps: %v", s.log)
	}
	p.untouched()
	// Tried again, it fails again the same way, and leaves the file be.
	r = p.run(nil, start.Add(time.Hour))
	if r.Delivered || r.Failed == "" {
		t.Fatalf("tried again: %+v", r)
	}
	if got, _ := os.ReadFile(p.final()); string(got) != "a different recording" {
		t.Fatalf("the file under the name is %q", got)
	}
	// Once the name is free, it is handed over.
	if err := os.Remove(p.final()); err != nil {
		t.Fatal(err)
	}
	p.delivered(p.run(nil, start.Add(2*time.Hour)))

	// The same bytes already there are the recording, and nothing is copied.
	q := newPlace(t)
	q.write(q.final(), q.movie)
	before, _ = os.Lstat(q.final())
	s = &trail{}
	q.delivered(q.run(s.fault, start))
	if after, _ := os.Lstat(q.final()); !os.SameFile(before, after) || s.count("do copy") != 0 {
		t.Fatalf("the file already there was replaced, or copied: %v", s.log)
	}
}

// A file can take the recording's name while the copy is made. The copy is
// never given the name then: the file is compared, and left as it is.
func TestANameTakenDuringTheHandOverIsLeftAlone(t *testing.T) {
	p := newPlace(t)
	s := &trail{at: map[string]func() error{"copied": func() error {
		p.write(p.final(), []byte("a recording made meanwhile"))
		return nil
	}}}
	r := p.run(s.fault, start)
	if r.Delivered || !strings.Contains(r.Failed, "different file has its name") || r.Path != p.own() {
		t.Fatalf("the report: %+v", r)
	}
	if got, _ := os.ReadFile(p.final()); string(got) != "a recording made meanwhile" {
		t.Fatalf("the file under the name is %q", got)
	}
	// The verified copy stays under its hidden name until the hand-over is
	// tried again, which first removes it.
	if !exists(p.hidden()) {
		t.Fatal("the hidden copy went without being asked to")
	}
	p.untouched()
	r = p.run(nil, start.Add(time.Hour))
	if r.Delivered || r.Failed == "" || exists(p.hidden()) {
		t.Fatalf("tried again: %+v, hidden copy there: %v", r, exists(p.hidden()))
	}
	if got, _ := os.ReadFile(p.final()); string(got) != "a recording made meanwhile" {
		t.Fatalf("the file under the name is %q", got)
	}
	p.untouched()

	// A file the same as the recording that takes its name meanwhile is the
	// recording handed over. That file stays as it is, and the copy On Air
	// made of it goes.
	q := newPlace(t)
	var taken os.FileInfo
	s = &trail{at: map[string]func() error{"copied": func() error {
		q.write(q.final(), q.movie)
		taken, _ = os.Lstat(q.final())
		return nil
	}}}
	q.delivered(q.run(s.fault, start))
	if now, _ := os.Lstat(q.final()); !os.SameFile(taken, now) {
		t.Fatal("the file that took the name was replaced")
	}
}

// A file that takes the recording's name, then goes before it is compared,
// leaves the core where its model never goes: it asks for the comparison
// again. The run stops there and says so, rather than asking forever, and
// nothing in the folder is touched.
func TestANameTakenThenFreedStopsTheHandOver(t *testing.T) {
	p := newPlace(t)
	compared := 0
	s := &trail{at: map[string]func() error{
		"copied": func() error {
			p.write(p.final(), []byte("a recording made meanwhile"))
			return nil
		},
		"do compareNamed": func() error {
			if compared++; compared == 2 {
				return os.Remove(p.final())
			}
			return nil
		},
	}}
	r := p.run(s.fault, start)
	if r.Delivered || r.Failed != "" || !errors.Is(r.Err, errNameFreed) || r.Path != p.own() || exists(p.final()) {
		t.Fatalf("the report: %+v", r)
	}
	p.untouched()
	r = p.run(nil, start.Add(time.Hour))
	if !errors.Is(r.Err, errNameFreed) || exists(p.final()) || !exists(p.own()) {
		t.Fatalf("carried on: %+v", r)
	}
}

// A folder that isn't there fails the hand-over, which keeps On Air's own
// copy and is tried again.
func TestAFolderThatIsGoneFailsTheHandOver(t *testing.T) {
	p := newPlace(t)
	aside := p.folder + " (unplugged)"
	if err := os.Rename(p.folder, aside); err != nil {
		t.Fatal(err)
	}
	r := p.run(nil, start)
	if r.Delivered || r.Failed != "the folder isn't there" || r.Path != p.own() || !exists(p.own()) || exists(p.folder) {
		t.Fatalf("the report: %+v", r)
	}
	if err := os.Rename(aside, p.folder); err != nil {
		t.Fatal(err)
	}
	p.delivered(p.run(nil, start.Add(time.Hour)))

	// A file where the folder should be is no folder either.
	q := newPlace(t)
	if err := os.RemoveAll(q.folder); err != nil {
		t.Fatal(err)
	}
	q.write(q.folder, []byte("not a folder"))
	if r := q.run(nil, start); r.Failed != "the folder isn't there" {
		t.Fatalf("the report: %+v", r)
	}
	if got, _ := os.ReadFile(q.folder); string(got) != "not a folder" {
		t.Fatalf("the file is %q", got)
	}
}

// A full disk fails the hand-over. The partial copy stays under its hidden
// name, where On the Record doesn't look, until the hand-over is tried
// again, which removes it first.
func TestAFullDiskFailsTheHandOver(t *testing.T) {
	p := newPlace(t)
	full := &trail{at: map[string]func() error{"copying": func() error { return syscall.ENOSPC }}}
	r := p.run(full.fault, start)
	if r.Delivered || r.Failed != "its disk is full" || r.Path != p.own() || exists(p.final()) {
		t.Fatalf("the report: %+v", r)
	}
	// Tried again at the next run, not at once.
	if full.count("do copy") != 1 {
		t.Fatalf("the steps: %v", full.did())
	}
	if got, err := os.ReadFile(p.hidden()); err != nil || len(got) == 0 || len(got) >= len(p.movie) {
		t.Fatalf("the partial copy: %d bytes, %v", len(got), err)
	}
	p.untouched()
	s := &trail{}
	p.delivered(p.run(s.fault, start.Add(time.Hour)))
	if got := s.did(); !slices.Equal(got, []string{"do removeHidden", "do compareNamed", "do copy", "do compareHidden", "do rename"}) {
		t.Fatalf("tried again: %v", got)
	}
}

// Copies that come out different from the recording are never given its
// name. Each is removed and made again, and after three in a row the
// hand-over fails, with the last one removed.
func TestCopiesThatKeepComingOutDifferentFailTheHandOver(t *testing.T) {
	p := newPlace(t)
	spoil := func() error {
		b, _ := os.ReadFile(p.hidden())
		b[len(b)/2] ^= 0xff
		return os.WriteFile(p.hidden(), b, 0o644)
	}
	s := &trail{at: map[string]func() error{"copied": spoil}}
	r := p.run(s.fault, start)
	if r.Delivered || r.Failed != "3 copies in a row came out different from the recording" || exists(p.final()) || exists(p.hidden()) {
		t.Fatalf("the report: %+v", r)
	}
	if s.count("do copy") != 3 || s.count("do removeHidden") != 3 || s.count("do rename") != 0 || !exists(p.own()) {
		t.Fatalf("the steps: %v", s.log)
	}
	p.untouched()
	p.delivered(p.run(nil, start.Add(time.Hour)))

	// One copy that comes out different is replaced by one that doesn't.
	q := newPlace(t)
	spoiled := false
	s = &trail{at: map[string]func() error{"copied": func() error {
		if spoiled {
			return nil
		}
		spoiled = true
		b, _ := os.ReadFile(q.hidden())
		return os.WriteFile(q.hidden(), append(b, '!'), 0o644)
	}}}
	q.delivered(q.run(s.fault, start))
	if s.count("do copy") != 2 {
		t.Fatalf("the steps: %v", s.log)
	}
}

// A removal of On Air's own copy that fails leaves the hand-over done, and
// is tried again later.
func TestAFailedRemovalIsTriedAgainAndNeverUndoesTheHandOver(t *testing.T) {
	p := newPlace(t)
	p.delivered(p.run(nil, start))
	// Something that can't be removed is where On Air's own copy was.
	aside := p.own() + ".aside"
	if err := os.Rename(p.own(), aside); err != nil {
		t.Fatal(err)
	}
	p.write(filepath.Join(p.own(), "in the way"), []byte("x"))
	for _, at := range []time.Time{start.Add(Keep), start.Add(Keep + time.Hour)} {
		r := p.run(nil, at)
		p.delivered(r)
		if r.Removed || r.RemoveErr == nil {
			t.Fatalf("at %v: %+v", at, r)
		}
	}
	if err := os.RemoveAll(p.own()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(aside, p.own()); err != nil {
		t.Fatal(err)
	}
	r := p.run(nil, start.Add(Keep+2*time.Hour))
	p.delivered(r)
	if !r.Removed || r.RemoveErr != nil || exists(p.own()) {
		t.Fatalf("tried again: %+v", r)
	}
}

// An attempt keeps to the folder it started in. A new attempt, after one
// failed, goes to the folder given then, once it has cleaned up after the
// one before.
func TestAnAttemptKeepsToItsFolder(t *testing.T) {
	// Cut short in one folder, carried on there, whatever folder is given.
	p := newPlace(t)
	other := filepath.Join(filepath.Dir(p.folder), "Elsewhere")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if d, _ := p.prepare(func(point string) error {
		if point == "copying" {
			return errKilled
		}
		return nil
	}); d.Run(p.folder, start).Err == nil {
		t.Fatal("the kill didn't stop the run")
	}
	p.delivered(p.runTo(nil, start, other))
	if entries, _ := os.ReadDir(other); len(entries) != 0 {
		t.Fatalf("the other folder holds %v", entries)
	}

	// Failed for a full disk in one folder, then given another: the partial
	// copy is removed from the first, and the recording goes to the second.
	q := newPlace(t)
	full := &trail{at: map[string]func() error{"copying": func() error { return syscall.ENOSPC }}}
	if r := q.run(full.fault, start); r.Failed == "" {
		t.Fatalf("the report: %+v", r)
	}
	elsewhere := newPlace(t)
	r := q.runTo(nil, start, elsewhere.folder)
	if !r.Delivered || r.Folder != elsewhere.folder || exists(q.hidden()) || exists(q.final()) {
		t.Fatalf("the report: %+v", r)
	}
	q.untouched()
	elsewhere.movie = q.movie
	elsewhere.delivered(r)
}

// load reports where the kept hand-over stands, as a fresh process finds it.
func (p *place) load() Report {
	p.t.Helper()
	d, ok, err := Load(p.spool)
	if !ok || err != nil {
		p.t.Fatalf("the record: %v, %v", ok, err)
	}
	return d.report(false, nil)
}

func (p *place) runTo(fault func(string) error, now time.Time, folder string) Report {
	p.t.Helper()
	d, err := p.prepare(fault)
	if err != nil {
		p.t.Fatal(err)
	}
	return d.Run(folder, now)
}

// A step that couldn't be carried out stops the run without telling the
// core anything, and the next run carries it out.
func TestAStepThatCantBeCarriedOutIsCarriedOnLater(t *testing.T) {
	p := newPlace(t)
	refused := errors.New("the network went away")
	s := &trail{at: map[string]func() error{"do rename": func() error { return refused }}}
	r := p.run(s.fault, start)
	if r.Delivered || r.Failed != "" || !errors.Is(r.Err, refused) || r.Path != p.own() || exists(p.final()) {
		t.Fatalf("the report: %+v", r)
	}
	again := &trail{}
	p.delivered(p.run(again.fault, start.Add(time.Hour)))
	if again.count("do copy") != 0 || again.count("do rename") != 1 {
		t.Fatalf("carried on: %v", again.log)
	}
}

// A hand-over that hasn't started can be forgotten, and one that has can't.
func TestOnlyAHandOverThatHasntStartedIsDropped(t *testing.T) {
	p := newPlace(t)
	d, err := p.prepare(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Drop(); err != nil || exists(filepath.Join(p.spool, recordName)) {
		t.Fatalf("dropping: %v", err)
	}
	p.delivered(p.run(nil, start))
	d, _, _ = Load(p.spool)
	if err := d.Drop(); err != nil || !exists(filepath.Join(p.spool, recordName)) {
		t.Fatalf("dropping one handed over: %v", err)
	}
	// A record keeps the name it was given first.
	again, err := Prepare(p.spool, "another name.mp4")
	if err != nil || again.File() != file {
		t.Fatalf("prepared again: %v, %v", again, err)
	}
	for _, name := range []string{"", ".hidden.mp4", "a/b.mp4", recordName} {
		if _, err := Prepare(t.TempDir(), name); err == nil {
			t.Fatalf("%q was taken for a recording's name", name)
		}
	}
	// A record that can't be read is said, and left alone.
	q := newPlace(t)
	q.write(filepath.Join(q.spool, recordName), []byte("not a record"))
	if _, _, err := Load(q.spool); err == nil {
		t.Fatal("a record that can't be read was loaded")
	}
	if _, err := Prepare(q.spool, file); err == nil {
		t.Fatal("a record that can't be read was replaced")
	}
}
