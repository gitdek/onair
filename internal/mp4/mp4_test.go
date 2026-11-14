package mp4

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func u64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }

func join(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

func mkbox(kind string, parts ...[]byte) []byte {
	body := join(parts...)
	return join(u32(uint32(8+len(body))), []byte(kind), body)
}

// full is a full box's contents: its version, no flags, then its fields.
func full(version byte, fields ...[]byte) []byte {
	return join([]byte{version, 0, 0, 0}, join(fields...))
}

// times are a header's creation and modification times, in its version's width.
func times(version byte) []byte {
	if version == 1 {
		return join(u64(1), u64(2))
	}
	return join(u32(1), u32(2))
}

func trak(id, timescale uint32, handler string, version byte) []byte {
	tkhd := mkbox("tkhd", full(version, times(version), u32(id), u32(0), make([]byte, 60)))
	duration := u32(0)
	if version == 1 {
		duration = u64(0)
	}
	mdhd := mkbox("mdhd", full(version, times(version), u32(timescale), duration, u32(0)))
	hdlr := mkbox("hdlr", full(0, u32(0), []byte(handler), make([]byte, 12), []byte("On Air\x00")))
	return mkbox("trak", tkhd, mkbox("mdia", mdhd, hdlr, mkbox("minf")))
}

func traf(id uint32, version byte, decode uint64) []byte {
	tfdt := full(0, u32(uint32(decode)))
	if version == 1 {
		tfdt = full(1, u64(decode))
	}
	return mkbox("traf", mkbox("tfhd", full(0, u32(id))), mkbox("tfdt", tfdt), mkbox("trun", full(0, u32(0))))
}

func moof(trafs ...[]byte) []byte {
	return mkbox("moof", mkbox("mfhd", full(0, u32(1))), join(trafs...))
}

// opening is what the writer's opening segment holds: a picture track at
// 600 units a second and a sound track at 48 kHz.
var opening = join(
	mkbox("ftyp", []byte("iso6"), u32(0)),
	mkbox("moov", mkbox("mvhd", full(0, times(0), u32(600))), trak(1, 600, "vide", 0), trak(2, 48000, "soun", 1)),
)

func media() []byte { return mkbox("mdat", bytes.Repeat([]byte{0xAB}, 300)) }

func read(t *testing.T, file []byte) ([]Track, error) {
	t.Helper()
	return Read(bytes.NewReader(file), int64(len(file)))
}

func TestARecordingStartsAtZero(t *testing.T) {
	file := join(opening,
		mkbox("styp", []byte("msdh")), moof(traf(1, 1, 0), traf(2, 0, 0)), media(),
		mkbox("styp", []byte("msdh")), moof(traf(1, 1, 2400), traf(2, 0, 192000)), media())
	tracks, err := read(t, file)
	if err != nil {
		t.Fatal(err)
	}
	want := []Track{{ID: 1, Handler: "vide", Timescale: 600, First: 0}, {ID: 2, Handler: "soun", Timescale: 48000, First: 0}}
	if len(tracks) != 2 || tracks[0] != want[0] || tracks[1] != want[1] {
		t.Fatalf("tracks = %+v", tracks)
	}
}

// A later piece is the opening segment and one fragment, which starts where
// the one before it ended.
func TestAPieceStartsWhereTheOneBeforeItEnded(t *testing.T) {
	tracks, err := read(t, join(opening, moof(traf(2, 0, 96000), traf(1, 1, 1200)), media()))
	if err != nil {
		t.Fatal(err)
	}
	if tracks[0].Seconds() != 2 || tracks[1].Seconds() != 2 {
		t.Fatalf("a piece two seconds in starts at %v and %v", tracks[0].Seconds(), tracks[1].Seconds())
	}
}

// A recording stamped by the host clock carries the machine's uptime: 81
// hours here, past what 32 bits hold at 48 kHz, so only a 64-bit decode
// time can say it.
func TestADecodeTimeCanNeedSixtyFourBits(t *testing.T) {
	uptime := uint64(291093) * 48000
	tracks, err := read(t, join(opening, moof(traf(1, 1, 291093*600), traf(2, 1, uptime)), media()))
	if err != nil {
		t.Fatal(err)
	}
	if tracks[0].Seconds() != 291093 || tracks[1].First != uptime || tracks[1].Seconds() != 291093 {
		t.Fatalf("tracks = %+v", tracks)
	}
}

// Each track's own first fragment counts, whichever fragment that is.
func TestATrackCanStartInALaterFragment(t *testing.T) {
	tracks, err := read(t, join(opening, moof(traf(1, 0, 0)), media(), moof(traf(1, 0, 600), traf(2, 1, 4800)), media()))
	if err != nil {
		t.Fatal(err)
	}
	if tracks[0].First != 0 || tracks[1].First != 4800 {
		t.Fatalf("tracks = %+v", tracks)
	}
}

// Media data can be large enough to need a 64-bit size, or run to the end
// of the file with no size at all, and is skipped either way.
func TestMediaIsSkippedWhateverItsSize(t *testing.T) {
	large := join(u32(1), []byte("mdat"), u64(16+5), []byte("12345"))
	toTheEnd := join(u32(0), []byte("mdat"), []byte("rest of the file"))
	tracks, err := read(t, join(opening, large, moof(traf(1, 1, 600), traf(2, 0, 48000)), toTheEnd))
	if err != nil {
		t.Fatal(err)
	}
	if tracks[0].Seconds() != 1 || tracks[1].Seconds() != 1 {
		t.Fatalf("tracks = %+v", tracks)
	}
}

func TestWhatIsntAWholeFragmentedFileIsRefused(t *testing.T) {
	whole := join(opening, moof(traf(1, 1, 0), traf(2, 0, 0)), media())
	cases := map[string]struct {
		file []byte
		says string
	}{
		"no fragment":                    {opening, "no movie fragment"},
		"no movie box":                   {join(mkbox("ftyp", []byte("iso6")), moof(traf(1, 0, 0))), "before the movie box"},
		"nothing":                        {nil, "no movie box"},
		"cut in the fragment":            {whole[:len(opening)+40], "runs past its end"},
		"cut in a box's header":          {whole[:len(opening)+4], "cut short"},
		"a box smaller than its header":  {join(opening, u32(4), []byte("moof")), "less than its own header"},
		"a track with no fragment":       {join(opening, moof(traf(1, 0, 0)), media()), "track 2 has no fragment"},
		"a fragment for no track":        {join(opening, moof(traf(7, 0, 0)), media()), "track 7"},
		"a fragment with no decode time": {join(opening, moof(mkbox("traf", mkbox("tfhd", full(0, u32(1))))), media()), "tfdt"},
		"a cut decode time":              {join(opening, moof(mkbox("traf", mkbox("tfhd", full(0, u32(1))), mkbox("tfdt", []byte{1, 0, 0, 0, 0, 0}))), media()), "cut short"},
	}
	for name, c := range cases {
		if tracks, err := read(t, c.file); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %+v, %v", name, tracks, err)
		}
	}
}

func TestFileReadsFromDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piece.mp4")
	if err := os.WriteFile(path, join(opening, moof(traf(1, 1, 1200), traf(2, 0, 96000)), media()), 0o600); err != nil {
		t.Fatal(err)
	}
	tracks, err := File(path)
	if err != nil || len(tracks) != 2 || tracks[0].Seconds() != 2 || tracks[1].Seconds() != 2 {
		t.Fatalf("tracks = %+v, %v", tracks, err)
	}
	if _, err := File(filepath.Join(t.TempDir(), "missing.mp4")); err == nil {
		t.Fatal("a missing file was read")
	}
}
