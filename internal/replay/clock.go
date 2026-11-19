package replay

import (
	"sync"
	"time"
)

// Clock is the time the live side is told in a replay. It runs Speed
// times as fast as the real one, and stands still while it is held, as it
// is while the archive or a model is asked something for the first time.
type Clock struct {
	speed float64
	from  time.Time // the time it starts at

	mu    sync.Mutex
	began time.Time     // the real time it started
	held  time.Duration // the real time it stood still, in all
	holds int
	since time.Time // the real time the hold under way began
}

// NewClock starts a clock at from, running speed times as fast as the
// real one.
func NewClock(from time.Time, speed float64) *Clock {
	if speed <= 0 {
		speed = 1
	}
	return &Clock{speed: speed, from: from, began: time.Now()}
}

// Now is the time by the clock.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	real := time.Now()
	if c.holds > 0 {
		real = c.since
	}
	return c.from.Add(time.Duration(float64(real.Sub(c.began)-c.held) * c.speed))
}

// Hold stops the clock until the function it returns is called. Holds may
// overlap: the clock goes on once none is left.
func (c *Clock) Hold() (release func()) {
	c.mu.Lock()
	if c.holds == 0 {
		c.since = time.Now()
	}
	c.holds++
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.holds--; c.holds == 0 {
				c.held += time.Since(c.since)
			}
		})
	}
}

// Until waits until the clock reads t, and says so, or until stop is
// closed, and says it didn't get there.
func (c *Clock) Until(t time.Time, stop <-chan struct{}) bool {
	for {
		left := t.Sub(c.Now())
		if left <= 0 {
			return true
		}
		// Short waits, so that a hold, which the clock can't foresee,
		// keeps the wait in step with it.
		wait := min(max(time.Duration(float64(left)/c.speed), 200*time.Microsecond), 20*time.Millisecond)
		select {
		case <-stop:
			return false
		case <-time.After(wait):
		}
	}
}
