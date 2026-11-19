package live

import (
	"fmt"
	"strings"
	"time"
)

// Settle is how much more sound recognition is given after a sentence of a
// partial result seems to end, at an end mark or at the last word before
// a Pause, before the sentence is taken as finished: time for recognition,
// which hears the sound a few tenths of a second after it is given, to
// revise it or carry it on.
const Settle = 400 * time.Millisecond

// joinGap is how soon after a sentence that ran to the end of a request's
// sound the next request's first must start to carry it on.
const joinGap = 2*Pause + 250*time.Millisecond

// stream follows the results of speech recognition a request at a time.
// Each partial result holds all the words of its request so far, which
// later ones may revise, and its final one ends it. The stream hands on
// each sentence once, as soon as it is finished: one followed by more
// words, at an end mark or a Pause, at once; the last one said, Settle
// after its end mark, or Pause and Settle after its last word, of sound
// recognition was given; and what is left of a request at its final. A
// sentence that runs to the end of a request's sound is held, and carried
// on by the next request's first when that starts at once. What was
// handed on is never handed on again, however later results revise it.
type stream struct {
	req    int           // the request followed
	open   bool          // its final hasn't come
	began  time.Time     // when its sound began
	cut    time.Duration // its words that end by then were handed on, when its words had times
	words  int           // and how many were, with times or without
	last   Said          // its newest result
	tail   string        // its words not handed on, when they last changed
	moved  time.Duration // how much sound it had been given then
	grow   *utterance    // the sentence being said, not yet finished
	held   *utterance    // a sentence that ran to the end of an earlier request's sound
	heldBy int           // that request
	closed int           // the last request that ended
	count  requestCounts
	// ended counts the requests that ended, for the diagnostic log, until
	// it is read.
	ended []requestCounts
}

// requestCounts is how a request's results went, for the diagnostic log:
// in counts and times only.
type requestCounts struct {
	request                  int
	partials, timed, untimed int // partial results, those whose words had times, and those whose didn't
	early, atFinal           int // sentences finished from partial results, and at the final
	byMark, byPause, held    int // how they ended, and whether the last ran on to the next request
	// behind is how far recognition was behind the sound it was given,
	// when a partial result brought new words: from the end of the
	// newest word to the end of the sound, in all and at most.
	behind, behindMost time.Duration
	behinds            int
	noFinal            bool // its final never came
}

func (r requestCounts) String() string {
	behind := "no new words with times"
	if r.behinds > 0 {
		behind = fmt.Sprintf("recognition %d ms behind the sound on average and %d at most", (r.behind / time.Duration(r.behinds)).Milliseconds(), r.behindMost.Milliseconds())
	}
	final := "at its final"
	if r.noFinal {
		final = "when the next began, with no final"
	}
	return fmt.Sprintf("request %d: %d sentences finished as they were said and %d %s (%d by a mark, %d by a pause, %d held for the next); %d partial results, %d with times, %d without, %s",
		r.request, r.early, r.atFinal, final, r.byMark, r.byPause, r.held, r.partials, r.timed, r.untimed, behind)
}

// take takes a result of recognition, and returns the sentences it
// finished, in order.
func (st *stream) take(s Said) []utterance {
	var done []utterance
	if s.Request != 0 && (s.Request == st.closed || s.Request < st.req) {
		// A result of a request that ended: what it said was taken.
		return nil
	}
	if s.Request != st.req || !st.open {
		if st.open {
			// The last request's final never came: its words stand as
			// they were last heard.
			st.count.noFinal = true
			done = st.close(st.last)
		}
		st.req, st.open, st.began, st.cut, st.words, st.tail, st.moved, st.grow = s.Request, true, s.Began, 0, 0, "", 0, nil
		st.count = requestCounts{request: s.Request}
	}
	st.last = s
	if !s.Partial {
		return append(done, st.close(s)...)
	}
	return append(done, st.partial(s)...)
}

// partial takes a partial result: of the sentences of its words not
// handed on, all but the last are finished, and the last once enough
// sound has gone by after it.
func (st *stream) partial(s Said) []utterance {
	rest, timed := st.rest(s)
	extent := s.measured()
	st.count.partials++
	if timed {
		st.count.timed++
	} else if len(s.Segments) > 0 {
		st.count.untimed++
	}
	if text := textOf(rest.Segments); text != st.tail {
		st.tail, st.moved = text, extent
		if newest := s.newest(); timed && text != "" && extent >= newest {
			st.count.behind += extent - newest
			st.count.behindMost = max(st.count.behindMost, extent-newest)
			st.count.behinds++
		}
	}
	ss := rest.sentences()
	st.unfirst(ss)
	st.grow = nil
	var done []utterance
	for i, u := range ss {
		if i == len(ss)-1 {
			if !st.finished(&u, timed, extent) {
				st.grow = &u
				break
			}
		}
		done = append(done, u)
	}
	st.advance(done, timed)
	st.count.early += len(done)
	return st.join(done)
}

// finished says whether the last sentence of a partial result is
// finished, and how it ended.
func (st *stream) finished(u *utterance, timed bool, extent time.Duration) bool {
	switch {
	case u.ended == byMark:
		return extent >= st.moved+Settle
	case timed && u.ended == byPause:
		return extent >= max(u.to.Sub(st.began)+Pause, st.moved)+Settle
	case !timed && extent >= st.moved+Pause:
		// With no times, a pause is sound recognition was given with
		// nothing new heard in it.
		u.ended = byPause
		return true
	}
	return false
}

// close takes the final result of a request, or the last partial one when
// its final never came: what it has left is finished, but for a last
// sentence that runs to the end of its sound, which is held.
func (st *stream) close(s Said) []utterance {
	rest, _ := st.rest(s)
	ss := rest.sentences()
	st.unfirst(ss)
	st.open, st.grow, st.closed = false, nil, st.req
	st.count.atFinal = len(ss)
	ss = st.join(ss)
	var done []utterance
	for i, u := range ss {
		if i == len(ss)-1 && u.ended == open {
			st.held, st.heldBy = &u, st.req
			st.count.held++
			st.count.atFinal--
			break
		}
		done = append(done, u)
	}
	for _, u := range done {
		st.tally(u)
	}
	st.ended = append(st.ended, st.count)
	return done
}

// rest is a result with only its words not yet handed on, and whether its
// words have times. Of words with times, those whose middle falls after
// the cut are left, when words with times were handed on; otherwise, as
// when results with times follow results without or the other way round,
// those after the count of words handed on.
func (st *stream) rest(s Said) (Said, bool) {
	timed := false
	for _, seg := range s.Segments {
		timed = timed || seg.At > 0 || seg.Length > 0
	}
	r := s
	r.Segments = nil
	seen := 0
	for _, seg := range s.Segments {
		if timed && (st.cut > 0 || st.words == 0) {
			if seg.At+max(seg.Length, 0)/2 > st.cut {
				r.Segments = append(r.Segments, seg)
			}
			continue
		}
		ws := strings.Fields(seg.Text)
		if skip := st.words - seen; skip < len(ws) {
			seg.Text = strings.Join(ws[max(skip, 0):], " ")
			r.Segments = append(r.Segments, seg)
		}
		seen += len(ws)
	}
	return r, timed
}

// unfirst marks the first of a request's sentences as not starting as its
// sound does, once some of its words were handed on.
func (st *stream) unfirst(ss []utterance) {
	if len(ss) > 0 && (st.cut > 0 || st.words > 0) {
		ss[0].first = false
	}
}

// advance moves the cut past the sentences handed on, and counts them and
// their words.
func (st *stream) advance(done []utterance, timed bool) {
	for _, u := range done {
		if timed {
			st.cut = max(st.cut, u.to.Sub(st.began))
		}
		st.words += len(strings.Fields(u.text))
		st.tally(u)
	}
}

func (st *stream) tally(u utterance) {
	switch u.ended {
	case byMark:
		st.count.byMark++
	case byPause, byTime:
		st.count.byPause++
	}
}

// join carries a held sentence on with the first of the sentences given,
// when that starts as its request's sound does, within joinGap of it, and
// else hands it on before them.
func (st *stream) join(ss []utterance) []utterance {
	if st.held == nil || len(ss) == 0 {
		return ss
	}
	held := *st.held
	st.held = nil
	if ss[0].first && ss[0].from.Sub(held.to) <= joinGap && len(strings.Fields(held.text)) < 60 {
		j := held
		j.text, j.to, j.ended = held.text+" "+ss[0].text, ss[0].to, ss[0].ended
		return append([]utterance{j}, ss[1:]...)
	}
	return append([]utterance{held}, ss...)
}

// flush hands on a held sentence that the request after it doesn't carry
// on: once that request has been given joinGap and Settle of sound past
// it with no word in it, or a first word that starts too late, or, when no
// result of a next request comes, once patience has gone by.
func (st *stream) flush(now time.Time, patience time.Duration) []utterance {
	if st.held == nil {
		return nil
	}
	h := *st.held
	next := st.open && st.req != st.heldBy
	switch {
	case next && st.grow != nil && st.grow.first && st.grow.from.Sub(h.to) <= joinGap:
		// It is being carried on.
		return nil
	case next && st.grow != nil:
		h.ended = byPause
	case next && st.grow == nil && st.last.Partial && st.began.Add(st.last.measured()).Sub(h.to) >= joinGap+Settle:
		h.ended = byPause
	case now.Sub(h.to) >= patience:
		h.ended = byTime
	default:
		return nil
	}
	st.held = nil
	return []utterance{h}
}

// growing is the sentence being said, not yet finished: the last of a
// partial result's, carrying on a held sentence when it starts as its
// request's sound does, or a held sentence by itself.
func (st *stream) growing() (utterance, bool) {
	switch {
	case st.grow != nil && st.held != nil && st.grow.first && st.grow.from.Sub(st.held.to) <= joinGap:
		j := *st.held
		j.text, j.to = j.text+" "+st.grow.text, st.grow.to
		return j, true
	case st.grow != nil:
		return *st.grow, true
	case st.held != nil:
		return *st.held, true
	}
	return utterance{}, false
}

// read returns the counts of the requests that ended since it was last
// called.
func (st *stream) read() []requestCounts {
	out := st.ended
	st.ended = nil
	return out
}

// textOf is the text of segments, its words joined by spaces.
func textOf(segments []Segment) string {
	var b strings.Builder
	for _, seg := range segments {
		for _, w := range strings.Fields(seg.Text) {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(w)
		}
	}
	return b.String()
}

// newest is when the newest word of a result ends, from the start of its
// sound.
func (s Said) newest() time.Duration {
	var end time.Duration
	for _, seg := range s.Segments {
		end = max(end, seg.At+max(seg.Length, 0))
	}
	return end
}
