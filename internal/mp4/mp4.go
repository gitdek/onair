// Package mp4 reads what On Air needs to know from a fragmented MPEG-4
// file's own bytes, with no player in between: each track's timescale, and
// the base media decode time of its first fragment, which is where the
// track's timeline starts. A recording starts at zero, in the joined file
// and in its first piece, and every later piece starts where the one before
// it ended.
package mp4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// Track is one track of a fragmented MPEG-4 file.
type Track struct {
	ID        uint32
	Handler   string // "vide" for a picture, "soun" for sound
	Timescale uint32 // units a second of the track's times
	// First is the base media decode time of the track's first fragment, its
	// tfdt box, in the track's units.
	First uint64
}

// Seconds is when the track's first fragment starts.
func (t Track) Seconds() float64 {
	if t.Timescale == 0 {
		return 0
	}
	return float64(t.First) / float64(t.Timescale)
}

// File reads the tracks of the fragmented MPEG-4 file at path.
func File(path string) ([]Track, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return Read(f, info.Size())
}

// A movie box or a movie fragment's box is read whole, so one that says it
// is larger than this isn't believed.
const largest = 64 << 20

// Read reads the tracks of a fragmented MPEG-4 file of size bytes: the movie
// box for the tracks, then movie fragments until each track's first one is
// found. Media data is skipped, not read.
func Read(r io.ReaderAt, size int64) ([]Track, error) {
	var tracks []Track
	found := map[uint32]bool{}
	movie, fragments := false, false
	for at := int64(0); at < size; {
		b, err := header(r, at, size)
		if err != nil {
			return nil, err
		}
		switch b.kind {
		case "moov":
			body, err := b.read(r)
			if err != nil {
				return nil, err
			}
			if tracks, err = readMovie(body); err != nil {
				return nil, err
			}
			movie = true
		case "moof":
			if !movie {
				return nil, errors.New("a movie fragment comes before the movie box")
			}
			fragments = true
			body, err := b.read(r)
			if err != nil {
				return nil, err
			}
			if err := readFragment(body, tracks, found); err != nil {
				return nil, err
			}
			if len(found) == len(tracks) {
				return tracks, nil
			}
		}
		at = b.end
	}
	switch {
	case !movie:
		return nil, errors.New("no movie box")
	case !fragments:
		return nil, errors.New("no movie fragment: it isn't a fragmented MPEG-4 file")
	}
	for _, t := range tracks {
		if !found[t.ID] {
			return nil, fmt.Errorf("track %d has no fragment", t.ID)
		}
	}
	return tracks, nil
}

// box is where a box lies: its bytes from start to end, its contents from
// body.
type box struct {
	kind             string
	start, body, end int64
}

func (b box) read(r io.ReaderAt) ([]byte, error) {
	if b.end-b.body > largest {
		return nil, fmt.Errorf("the %q box at byte %d says it holds %d bytes", b.kind, b.start, b.end-b.body)
	}
	data := make([]byte, b.end-b.body)
	if _, err := r.ReadAt(data, b.body); err != nil {
		return nil, fmt.Errorf("the %q box at byte %d: %w", b.kind, b.start, err)
	}
	return data, nil
}

// header reads the header of the box at byte at, which must end by limit.
func header(r io.ReaderAt, at, limit int64) (box, error) {
	var h [16]byte
	if limit-at < 8 {
		return box{}, fmt.Errorf("the box at byte %d is cut short", at)
	}
	if _, err := r.ReadAt(h[:8], at); err != nil {
		return box{}, fmt.Errorf("the box at byte %d: %w", at, err)
	}
	b := box{kind: string(h[4:8]), start: at, body: at + 8}
	switch size := binary.BigEndian.Uint32(h[:4]); size {
	case 0: // it runs to the end
		b.end = limit
	case 1: // its size follows, in 64 bits
		if limit-at < 16 {
			return box{}, fmt.Errorf("the %q box at byte %d is cut short", b.kind, at)
		}
		if _, err := r.ReadAt(h[8:16], at+8); err != nil {
			return box{}, fmt.Errorf("the %q box at byte %d: %w", b.kind, at, err)
		}
		large := binary.BigEndian.Uint64(h[8:16])
		b.body = at + 16
		if large < 16 || large > uint64(limit-at) {
			return box{}, fmt.Errorf("the %q box at byte %d runs past its end", b.kind, at)
		}
		b.end = at + int64(large)
	default:
		if size < 8 {
			return box{}, fmt.Errorf("the %q box at byte %d says it is %d bytes, less than its own header", b.kind, at, size)
		}
		if int64(size) > limit-at {
			return box{}, fmt.Errorf("the %q box at byte %d runs past its end", b.kind, at)
		}
		b.end = at + int64(size)
	}
	return b, nil
}

// children lists the boxes inside a box's contents, each with its own.
func children(data []byte) (map[string][][]byte, error) {
	r := bytes.NewReader(data)
	out := map[string][][]byte{}
	for at := int64(0); at < int64(len(data)); {
		b, err := header(r, at, int64(len(data)))
		if err != nil {
			return nil, err
		}
		out[b.kind] = append(out[b.kind], data[b.body:b.end])
		at = b.end
	}
	return out, nil
}

// one is the only child box of a kind.
func one(boxes map[string][][]byte, kind, in string) ([]byte, error) {
	if len(boxes[kind]) != 1 {
		return nil, fmt.Errorf("%s holds %d %q boxes, not one", in, len(boxes[kind]), kind)
	}
	return boxes[kind][0], nil
}

// field reads n bytes at byte at of a full box's contents, after its
// version and flags.
func field(data []byte, at, n int, kind string) ([]byte, error) {
	if len(data) < 4+at+n {
		return nil, fmt.Errorf("the %q box is cut short", kind)
	}
	return data[4+at : 4+at+n], nil
}

func readMovie(body []byte) ([]Track, error) {
	boxes, err := children(body)
	if err != nil {
		return nil, err
	}
	var tracks []Track
	for _, trak := range boxes["trak"] {
		t, err := readTrack(trak)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	if len(tracks) == 0 {
		return nil, errors.New("the movie box holds no track")
	}
	return tracks, nil
}

func readTrack(trak []byte) (Track, error) {
	boxes, err := children(trak)
	if err != nil {
		return Track{}, err
	}
	tkhd, err := one(boxes, "tkhd", "a track")
	if err != nil {
		return Track{}, err
	}
	mdia, err := one(boxes, "mdia", "a track")
	if err != nil {
		return Track{}, err
	}
	media, err := children(mdia)
	if err != nil {
		return Track{}, err
	}
	mdhd, err := one(media, "mdhd", "a track's media")
	if err != nil {
		return Track{}, err
	}
	hdlr, err := one(media, "hdlr", "a track's media")
	if err != nil {
		return Track{}, err
	}
	var t Track
	// Version 1 has 64-bit times before these fields, version 0 32-bit ones.
	skip := 8
	if len(tkhd) > 0 && tkhd[0] == 1 {
		skip = 16
	}
	id, err := field(tkhd, skip, 4, "tkhd")
	if err != nil {
		return Track{}, err
	}
	t.ID = binary.BigEndian.Uint32(id)
	skip = 8
	if len(mdhd) > 0 && mdhd[0] == 1 {
		skip = 16
	}
	scale, err := field(mdhd, skip, 4, "mdhd")
	if err != nil {
		return Track{}, err
	}
	t.Timescale = binary.BigEndian.Uint32(scale)
	handler, err := field(hdlr, 4, 4, "hdlr")
	if err != nil {
		return Track{}, err
	}
	t.Handler = string(handler)
	if t.Timescale == 0 {
		return Track{}, fmt.Errorf("track %d has no timescale", t.ID)
	}
	return t, nil
}

// readFragment sets First for each track that is in this fragment and in no
// earlier one.
func readFragment(body []byte, tracks []Track, found map[uint32]bool) error {
	boxes, err := children(body)
	if err != nil {
		return err
	}
	for _, traf := range boxes["traf"] {
		parts, err := children(traf)
		if err != nil {
			return err
		}
		tfhd, err := one(parts, "tfhd", "a track fragment")
		if err != nil {
			return err
		}
		raw, err := field(tfhd, 0, 4, "tfhd")
		if err != nil {
			return err
		}
		id := binary.BigEndian.Uint32(raw)
		i := -1
		for j := range tracks {
			if tracks[j].ID == id {
				i = j
			}
		}
		if i < 0 {
			return fmt.Errorf("a fragment is for track %d, which the movie box doesn't have", id)
		}
		if found[id] {
			continue
		}
		tfdt, err := one(parts, "tfdt", fmt.Sprintf("track %d's first fragment", id))
		if err != nil {
			return err
		}
		if len(tfdt) > 0 && tfdt[0] == 1 {
			v, err := field(tfdt, 0, 8, "tfdt")
			if err != nil {
				return err
			}
			tracks[i].First = binary.BigEndian.Uint64(v)
		} else {
			v, err := field(tfdt, 0, 4, "tfdt")
			if err != nil {
				return err
			}
			tracks[i].First = uint64(binary.BigEndian.Uint32(v))
		}
		found[id] = true
	}
	return nil
}
