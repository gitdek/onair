//go:build darwin

package mac

import "testing"

// The picture keeps its content's shape, at most the rows asked for, with
// even sides.
func TestThePictureKeepsItsShape(t *testing.T) {
	for _, c := range []struct {
		width, height, scale float64
		rows                 int
		wide, high           int
	}{
		{1512, 982, 2, 1080, 1662, 1080}, // a laptop's display, brought down
		{1280, 720, 2, 1080, 1920, 1080}, // an area of it
		{641, 361, 1, 1080, 640, 360},    // small enough already, made even
		{800, 600, 2, 1080, 1440, 1080},  // a window
		{2560, 1440, 1, 0, 2560, 1440},   // no limit asked for
		{1, 1, 1, 1080, 2, 2},            // never no picture at all
		{300, 200, 0, 1080, 300, 200},    // no scale is one
	} {
		if wide, high := frameSize(c.width, c.height, c.scale, c.rows); wide != c.wide || high != c.high {
			t.Errorf("%v × %v at %v, %d rows: %d × %d, want %d × %d", c.width, c.height, c.scale, c.rows, wide, high, c.wide, c.high)
		}
	}
}
