// Package delivery hands a finished recording to a folder another program
// watches. It carries out each step the proved core in
// github.com/gitdek/onair/core/handover decides, on the real folder and in
// On Air's spool, and tells the core what came of it. It decides nothing of
// its own about the hand-over.
//
// It keeps the core's record, and each step's result, in the recording's
// own directory before it acts on either: the record is on disk before its
// step is carried out, and the step's result is on disk before the core is
// told. So a hand-over cut short anywhere, by a kill or a quit, carries on
// from where it stood, and a step asked for again gets the result it first
// had (docs/PRD.md, rules 5, 6, 7 and 11).
package delivery

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gitdek/onair/core/handover/handover"
)

const (
	// DifferenceLimit is how many copies may come out different from the
	// recording before the hand-over fails.
	DifferenceLimit = 3
	// Keep is how long On Air keeps its own copy once the folder holds the
	// recording, found the same (docs/PRD.md, decision 7).
	Keep = 3 * 24 * time.Hour
	// recordName is the hand-over's record, in the recording's directory.
	recordName = "handover.json"
	// steps bounds a run. A hand-over takes a few dozen at most.
	steps = 200
	// chunk is how much a copy or a comparison reads at a time.
	chunk = 1 << 20
)

// ErrStuck says a run stopped where the core's model doesn't go. Carried
// on, it stops there again, so the hand-over can't go on by itself.
var ErrStuck = errors.New("the hand-over's core has no step from here")

// stuck is an error that is ErrStuck.
type stuck string

func (e stuck) Error() string        { return string(e) }
func (e stuck) Is(target error) bool { return target == ErrStuck }

var (
	// errNameFreed says the core is stuck where its model never goes. A
	// copy can't take the recording's name because a file has it. The core
	// then has that file compared, and a file that is gone by then leaves
	// it where it was, asking for the same comparison again.
	errNameFreed error = stuck("a file had its name in the folder, then went away, and the hand-over can't go on from there")
	// errCopyGone says the copy was removed from under its hidden name
	// before it was given its name, which the core has no reply for.
	errCopyGone error = stuck("the copy under the hidden name is gone, so it can't be given its name")
)

// Delivery is one recording's hand-over.
type Delivery struct {
	dir  string // the recording's directory in the spool
	kept record
	// removal is why On Air's own copy couldn't be removed, the last time it
	// was tried.
	removal error
	// fault lets a test stop a run at a named point, as a kill would, or fail
	// what is done there, as a full disk would. It is nil otherwise.
	fault func(point string) error
	chunk int
}

// record is what is kept on disk, whole: the core's value and what its
// caller needs beside it.
type record struct {
	Version int `json:"version"`
	// Core is the core's whole value: what it was configured with, which
	// is restored with its record, and the record.
	Core handover.Handover `json:"core"`
	// Folder is the folder the attempt under way hands the recording to.
	// An attempt takes it before it asks anything of a folder, and keeps
	// to it.
	Folder string `json:"folder,omitempty"`
	// Result is what the outstanding step came to, kept before the core is
	// told.
	Result *result `json:"result,omitempty"`
	// HandedOver is when the folder was found to hold the recording, which
	// starts the days On Air keeps its own copy.
	HandedOver time.Time `json:"handedOver,omitzero"`
	// Leftover says a copy may be left under the hidden name, now that the
	// folder holds the recording. It goes before anything else is done.
	Leftover bool `json:"leftover,omitempty"`
}

type result struct {
	Request handover.Request `json:"request"`
	Reply   handover.Reply   `json:"reply"`
}

// Report says where a run left a hand-over.
type Report struct {
	File   string // the recording's name, in the spool and in the folder
	Folder string // the folder it went to, or is going to: empty before an attempt
	// Path is where the recording is now: in the folder once it is there,
	// and On Air's own copy otherwise.
	Path      string
	Delivered bool   // the folder holds the recording, found the same
	Handed    bool   // and this run made it so
	Failed    string // why the hand-over failed, when it did
	// Err is a step this run couldn't carry out. The hand-over carries on
	// from that step the next time it runs.
	Err       error
	Removed   bool  // On Air's own copy is gone, its days over
	RemoveErr error // why removing On Air's own copy failed, in this run
}

// Prepare returns the hand-over of the recording in dir, which hands over
// the file called file there. One kept from before is carried on, under
// the name it was given then. A new one is kept on disk before it is
// returned.
func Prepare(dir, file string) (*Delivery, error) {
	d, ok, err := Load(dir)
	if err != nil || ok {
		return d, err
	}
	if !valid(file) {
		return nil, fmt.Errorf("a recording can't be handed over as %q", file)
	}
	h, _ := handover.New(file, DifferenceLimit)
	d = &Delivery{dir: dir, chunk: chunk}
	return d, d.keep(record{Version: 1, Core: h})
}

// Load returns the hand-over kept in dir, and whether there is one.
func Load(dir string) (*Delivery, bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, recordName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var k record
	if err := json.Unmarshal(b, &k); err != nil {
		return nil, false, fmt.Errorf("%s isn't a hand-over On Air can read: %w", filepath.Join(dir, recordName), err)
	}
	if k.Version != 1 || !valid(k.Core.Recording) || k.Core.DifferenceLimit <= 0 {
		return nil, false, fmt.Errorf("%s isn't a hand-over On Air can read", filepath.Join(dir, recordName))
	}
	return &Delivery{dir: dir, kept: k, chunk: chunk}, true, nil
}

// Drop forgets a hand-over that hasn't started, such as one whose file
// couldn't be joined. One that has started is kept.
func (d *Delivery) Drop() error {
	if d.kept.Core.Record.Outcome != handover.NotStarted {
		return nil
	}
	if err := os.Remove(filepath.Join(d.dir, recordName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return syncDir(d.dir)
}

// valid says whether a name can be a recording's: one path element, not
// hidden, and not the hand-over's own record.
func valid(file string) bool {
	return file != "" && file == filepath.Base(file) && !strings.HasPrefix(file, ".") && file != recordName &&
		!strings.ContainsAny(file, "/\\\x00")
}

// File is the recording's name, the same in the spool and in the folder.
func (d *Delivery) File() string { return d.kept.Core.Recording }

// Spool is On Air's own copy, in the recording's directory.
func (d *Delivery) Spool() string { return filepath.Join(d.dir, d.File()) }

// Delivered says whether the folder holds the recording.
func (d *Delivery) Delivered() bool { return d.kept.Core.Record.Outcome == handover.HandedOver }

// Done says whether nothing is left to do: the folder holds the recording,
// and On Air's own copy is gone.
func (d *Delivery) Done() bool {
	return d.Delivered() && d.kept.Core.Record.Cleanup == handover.CleanupDone
}

// Run carries the hand-over on from where it stands, as far as it can go
// now, and says where that left it. A new attempt hands the recording to
// folder, and one under way keeps to its own. A hand-over that had failed
// is tried again once, and so is a removal of On Air's own copy that had
// failed. On Air's own copy is removed once Keep has passed, on the clock
// now, since the folder was found to hold the recording.
func (d *Delivery) Run(folder string, now func() time.Time) Report {
	retry := d.kept.Core.Record.Outcome == handover.Failed
	retryRemoval := d.kept.Core.Record.Cleanup == handover.CleanupFailed
	handed, tidied := false, false
	d.removal = nil
	stop := func(err error) Report { return d.report(handed, err) }
	if folder == "" {
		return stop(errors.New("no folder was given"))
	}
	for range steps {
		h := d.kept.Core
		rec := h.Record
		next := d.kept
		switch {
		case rec.Intent != (handover.Request{}):
			reply, err := d.answer(rec.Intent)
			if err != nil {
				return stop(err)
			}
			moved, ok := h.Consume(true, rec.Intent, true, reply)
			if !ok {
				return stop(fmt.Errorf("the hand-over's core refused what a step came to: %v for %v", reply, rec.Intent))
			}
			next.Core, next.Result = moved, nil
			delivered := moved.Record.Outcome == handover.HandedOver && rec.Outcome != handover.HandedOver
			if delivered {
				// The days start now, however long the copy took.
				next.HandedOver, handed = now(), true
				// Found the same rather than given its name, the recording
				// may leave a copy under the hidden name.
				next.Leftover = rec.Phase != handover.Publish
			}
			if err := d.keep(next); err != nil {
				return stop(err)
			}
			if rec.Phase == handover.InspectTaken && reply == handover.Absent {
				return stop(errNameFreed)
			}
			continue
		case d.kept.Leftover && !tidied:
			// Once a run, until it is gone, so neither a kill nor a removal
			// that fails leaves it in the folder for good.
			tidied = true
			if !d.letGoOfLeftover() {
				continue
			}
			next.Leftover = false
		case rec.Outcome == handover.NotStarted:
			if _, err := os.Lstat(d.Spool()); err != nil {
				return stop(fmt.Errorf("On Air's own copy isn't there to hand over: %w", err))
			}
			next.Core, _ = h.Start(true)
		case rec.Outcome == handover.Failed:
			if !retry {
				return stop(nil)
			}
			retry = false
			next.Core, _ = h.RetryDelivery(true, d.hiddenAbsent())
		case rec.Outcome == handover.Active && rec.Phase == handover.Inspect && d.kept.Folder != folder:
			// An attempt asks nothing of a folder before it inspects it, and
			// a retry's cleaning up is done in the folder that attempt used.
			next.Folder = folder
		case rec.Outcome == handover.Active, rec.Cleanup == handover.Cleaning:
			var ok bool
			if next.Core, ok = h.Issue(true); !ok {
				return stop(errors.New("the hand-over's core asked for nothing"))
			}
		case rec.Cleanup == handover.Waiting:
			if at := now(); next.HandedOver.IsZero() {
				next.HandedOver = at
			} else if at.Sub(next.HandedOver) < Keep {
				return stop(nil)
			} else {
				next.Core, _ = h.BeginRetention(true, true)
			}
		case rec.Cleanup == handover.CleanupFailed:
			if !retryRemoval {
				return stop(nil)
			}
			retryRemoval = false
			next.Core, _ = h.RetryCleanup(true)
		default:
			return stop(nil)
		}
		if err := d.keep(next); err != nil {
			return stop(err)
		}
	}
	return stop(errors.New("the hand-over didn't settle"))
}

// report says where the hand-over stands.
func (d *Delivery) report(handed bool, err error) Report {
	rec := d.kept.Core.Record
	r := Report{File: d.File(), Folder: d.kept.Folder, Path: d.Spool(), Delivered: rec.Outcome == handover.HandedOver,
		Handed: handed, Err: err, Removed: rec.Cleanup == handover.CleanupDone, RemoveErr: d.removal}
	if r.Delivered {
		r.Path = filepath.Join(d.kept.Folder, d.File())
	}
	if rec.Outcome == handover.Failed {
		r.Failed = reasons[rec.Reason]
	}
	return r
}

var reasons = map[handover.Reason]string{
	handover.FolderGone:      "the folder isn't there",
	handover.FolderFull:      "its disk is full",
	handover.NameConflict:    "a different file has its name there, and On Air leaves that file as it is",
	handover.DifferentCopies: fmt.Sprintf("%d copies in a row came out different from the recording", DifferenceLimit),
}

// answer is what the outstanding request came to: the result kept for it,
// or else what carrying it out comes to now, kept before it is returned.
func (d *Delivery) answer(r handover.Request) (handover.Reply, error) {
	if k := d.kept.Result; k != nil && k.Request == r {
		return k.Reply, nil
	}
	reply, err := d.do(r)
	if err != nil {
		return handover.NoReply, err
	}
	next := d.kept
	next.Result = &result{Request: r, Reply: reply}
	return reply, d.keep(next)
}

// do carries out a request, and says what came of it in the core's terms.
// An error is a step it couldn't carry out, or answer truthfully.
func (d *Delivery) do(r handover.Request) (handover.Reply, error) {
	name := [...]string{"none", "compareNamed", "copy", "compareHidden", "rename", "removeHidden", "removeSpool"}
	if r.Kind < 0 || int(r.Kind) >= len(name) {
		return handover.NoReply, fmt.Errorf("the hand-over's core asked for a step On Air doesn't know: %d", r.Kind)
	}
	if err := d.at("do " + name[r.Kind]); err != nil {
		return handover.NoReply, err
	}
	path, err := d.path(r.Target)
	if err != nil {
		return handover.NoReply, err
	}
	switch r.Kind {
	case handover.CompareNamed:
		return d.compare(path, handover.Absent)
	case handover.Copy:
		return d.copy(path)
	case handover.CompareHidden:
		// A copy that isn't there isn't the same as the recording.
		return d.compare(path, handover.Different)
	case handover.Rename:
		hidden, err := d.path(handover.HiddenTarget)
		if err != nil {
			return handover.NoReply, err
		}
		return d.publish(hidden, path)
	case handover.RemoveHidden:
		return d.remove(path)
	case handover.RemoveSpool:
		return d.removeOwn(path)
	}
	return handover.NoReply, fmt.Errorf("the hand-over's core asked for a step On Air doesn't know: %d", r.Kind)
}

// path resolves one of the hand-over's own three names, through the core.
func (d *Delivery) path(t handover.Target) (string, error) {
	p, ok := d.kept.Core.Path(t)
	if !ok {
		return "", fmt.Errorf("the hand-over's core named a place On Air doesn't know: %d", t)
	}
	dir := d.dir
	if p.Directory == "folder" {
		if d.kept.Folder == "" {
			return "", errors.New("the hand-over has no folder yet")
		}
		dir = d.kept.Folder
	}
	return filepath.Join(dir, p.Prefix+p.Name), nil
}

// compare compares the file at path in the folder with On Air's own copy,
// byte for byte. A name with nothing under it comes to absent. One with
// anything but a file under it, such as a folder or a link, isn't the
// same, and neither is On Air's own copy itself, which is what has the
// name when the folder is the recording's own directory in the spool.
func (d *Delivery) compare(path string, absent handover.Reply) (handover.Reply, error) {
	if reply, err := d.folderThere(); reply != handover.NoReply || err != nil {
		return reply, err
	}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if reply, err := d.folderThere(); reply != handover.NoReply || err != nil {
			return reply, err
		}
		return absent, nil
	case err != nil:
		return d.failed(err)
	case !info.Mode().IsRegular():
		return handover.Different, nil
	}
	same, err := d.same(path)
	if err != nil {
		return d.failed(err)
	}
	if same {
		return handover.Same, nil
	}
	return handover.Different, nil
}

// same says whether the file at path holds the same bytes as On Air's own
// copy, and is a file of its own. It is opened without following a link,
// so what is compared is what was looked at: a link, anything but a file,
// and On Air's own copy under the name, aren't the same. Counted as the
// folder's, On Air's own copy would be removed once its days were up,
// with nothing left of the recording but its pieces.
func (d *Delivery) same(path string) (bool, error) {
	fa, err := os.Open(d.Spool())
	if err != nil {
		return false, err
	}
	defer fa.Close()
	fb, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ELOOP) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer fb.Close()
	ia, err := fa.Stat()
	if err != nil {
		return false, err
	}
	ib, err := fb.Stat()
	if err != nil {
		return false, err
	}
	if !ib.Mode().IsRegular() || os.SameFile(ia, ib) || ia.Size() != ib.Size() {
		return false, nil
	}
	ba, bb := make([]byte, d.chunk), make([]byte, d.chunk)
	for {
		na, errA := io.ReadFull(fa, ba)
		nb, errB := io.ReadFull(fb, bb)
		endA, endB := ended(errA), ended(errB)
		if errA != nil && !endA {
			return false, errA
		}
		if errB != nil && !endB {
			return false, errB
		}
		if !bytes.Equal(ba[:na], bb[:nb]) {
			return false, nil
		}
		if endA || endB {
			return endA && endB, nil
		}
	}
}

func ended(err error) bool { return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) }

// copy copies On Air's own copy to path, the hidden name, and has it whole
// on disk before it says so. The copy is always a new file. What is under
// the hidden name, such as a copy cut short, goes first, so the copy is
// never written through a link into another file. A folder there is left
// as it is, and the step stops.
func (d *Delivery) copy(path string) (handover.Reply, error) {
	if reply, err := d.folderThere(); reply != handover.NoReply || err != nil {
		return reply, err
	}
	src, err := os.Open(d.Spool())
	if err != nil {
		return handover.NoReply, fmt.Errorf("On Air's own copy: %w", err)
	}
	defer src.Close()
	if info, err := os.Lstat(path); err == nil && !info.IsDir() {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return d.failed(err)
		}
	}
	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return d.failed(err)
	}
	if err := d.pour(dst, src); err != nil {
		dst.Close()
		return d.failed(err)
	}
	if err := dst.Sync(); err != nil {
		dst.Close()
		return d.failed(err)
	}
	if err := dst.Close(); err != nil {
		return d.failed(err)
	}
	if err := d.syncFolder(); err != nil {
		return d.failed(err)
	}
	if err := d.at("copied"); err != nil {
		return handover.NoReply, err
	}
	return handover.OK, nil
}

// pour copies src to dst a chunk at a time.
func (d *Delivery) pour(dst io.Writer, src io.Reader) error {
	buf := make([]byte, d.chunk)
	begun := false
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, err := dst.Write(buf[:n]); err != nil {
				return err
			}
			if !begun {
				begun = true
				if err := d.at("copying"); err != nil {
					return err
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// publish gives the hidden copy the recording's name, never replacing a
// file that has it: it links the name to the copy, then lets the hidden
// name go.
func (d *Delivery) publish(hidden, final string) (handover.Reply, error) {
	if reply, err := d.folderThere(); reply != handover.NoReply || err != nil {
		return reply, err
	}
	err := os.Link(hidden, final)
	switch {
	case err == nil:
		if err := d.at("linked"); err != nil {
			return handover.NoReply, err
		}
		return d.letGo(hidden)
	case errors.Is(err, fs.ErrExist):
		// The name is taken, unless by the hidden copy itself: this step,
		// cut short between linking the name and letting the hidden one go.
		if a, errA := os.Lstat(hidden); errA == nil {
			if b, errB := os.Lstat(final); errB == nil && os.SameFile(a, b) {
				return d.letGo(hidden)
			}
		}
		return handover.Taken, nil
	case errors.Is(err, fs.ErrNotExist):
		if reply, err := d.folderThere(); reply != handover.NoReply || err != nil {
			return reply, err
		}
		// The hidden copy is gone, and a file has the recording's name:
		// this step's own, cut short once the hidden name was let go and
		// before what it came to was kept, or else another one. The core has
		// it compared, and takes one that is the same as handed over.
		if _, err := os.Lstat(final); err == nil {
			return handover.Taken, nil
		}
		return handover.NoReply, errCopyGone
	}
	return d.failed(err)
}

// letGo lets the hidden name go once the recording's name is linked to the
// same file.
func (d *Delivery) letGo(hidden string) (handover.Reply, error) {
	if err := os.Remove(hidden); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return d.failed(err)
	}
	if err := d.syncFolder(); err != nil {
		return d.failed(err)
	}
	if err := d.at("published"); err != nil {
		return handover.NoReply, err
	}
	return handover.OK, nil
}

// remove removes the hidden copy.
func (d *Delivery) remove(path string) (handover.Reply, error) {
	if reply, err := d.folderThere(); reply != handover.NoReply || err != nil {
		return reply, err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return d.failed(err)
	}
	if err := d.syncFolder(); err != nil {
		return d.failed(err)
	}
	return handover.OK, nil
}

// removeOwn removes On Air's own copy. A removal that fails is the core's to
// know, and is tried again later.
func (d *Delivery) removeOwn(path string) (handover.Reply, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		d.removal = err
		return handover.RemoveFailed, nil
	}
	if err := syncDir(d.dir); err != nil {
		d.removal = err
		return handover.RemoveFailed, nil
	}
	return handover.OK, nil
}

// letGoOfLeftover removes a copy the hand-over left under its hidden name
// once the folder holds the recording, and says whether none is left. That
// happens only when a file the same as the recording took its name while
// the copy was made. The core never asks for it, and nothing else uses it.
// Anything but a file under the hidden name isn't a copy, and stays.
func (d *Delivery) letGoOfLeftover() bool {
	hidden, err := d.path(handover.HiddenTarget)
	if err != nil {
		return true
	}
	// A folder that isn't there, such as a disk unplugged, may come back
	// with the copy in it.
	if reply, err := d.folderThere(); reply != handover.NoReply || err != nil {
		return false
	}
	info, err := os.Lstat(hidden)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return true
	case err != nil:
		return false
	case !info.Mode().IsRegular():
		return true
	}
	if err := os.Remove(hidden); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false
	}
	return d.syncFolder() == nil
}

// hiddenAbsent says whether the attempt that failed left nothing under the
// hidden name in its folder.
func (d *Delivery) hiddenAbsent() bool {
	if d.kept.Folder == "" {
		return true
	}
	hidden, err := d.path(handover.HiddenTarget)
	if err != nil {
		return true
	}
	_, err = os.Lstat(hidden)
	return errors.Is(err, fs.ErrNotExist)
}

// folderThere says, as a reply, that the folder is gone, or no reply when it
// is there.
func (d *Delivery) folderThere() (handover.Reply, error) {
	info, err := os.Stat(d.kept.Folder)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return handover.ReplyFolderGone, nil
	case err != nil:
		return handover.NoReply, err
	case !info.IsDir():
		return handover.ReplyFolderGone, nil
	}
	return handover.NoReply, nil
}

// failed says what an error in the folder comes to. A full disk and a
// folder that isn't there are the core's to know. Anything else is a step
// On Air couldn't carry out, which it tries again the next time it runs.
func (d *Delivery) failed(err error) (handover.Reply, error) {
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
		return handover.ReplyFolderFull, nil
	}
	if reply, _ := d.folderThere(); reply == handover.ReplyFolderGone {
		return reply, nil
	}
	return handover.NoReply, err
}

// syncFolder flushes the folder, so a name given in it survives a power
// loss. A file system that can't flush a folder, as some network ones
// can't, isn't held against the hand-over.
func (d *Delivery) syncFolder() error {
	err := syncDir(d.kept.Folder)
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.ENOTTY) {
		return nil
	}
	return err
}

// keep writes the hand-over's record whole under a hidden name, then gives
// it its name, so whatever stops On Air the record is the old one or the
// new one.
func (d *Delivery) keep(k record) error {
	b, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(d.dir, "."+recordName+".part")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
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
	if err := d.at("keeping"); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(d.dir, recordName)); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := syncDir(d.dir); err != nil {
		return err
	}
	d.kept = k
	return d.at("kept")
}

// at is where a test may stop a run or fail what is done there.
func (d *Delivery) at(point string) error {
	if d.fault == nil {
		return nil
	}
	return d.fault(point)
}

// syncDir flushes a directory, so a name given in it survives a power loss.
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
