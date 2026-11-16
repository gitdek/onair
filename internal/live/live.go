// Package live decides what On Air's live panel shows while it records: the
// last minute or so of what was said, when to ask On the Record what that
// minute connects to, and which of the links it answers with are worth
// showing; and which questions asked in the call, or topics brought up, to
// ask the archive about, and which lines of its answer to show. It touches no screen, no network and no sound. The Mac
// recognizes speech and draws the panel, internal/archive asks On the
// Record, and both report here.
//
// Nothing here can hold up a recording. Every input returns at once, having
// at most set a field or added to a bounded queue, and the work runs on
// goroutines of its own, which recover from a panic.
package live

import (
	"context"
	"time"
)

// Speaker says whose words they are.
type Speaker int

const (
	// Person is the microphone: the person recording.
	Person Speaker = iota + 1
	// Others is the call's sound: everyone else.
	Others
)

// Heard is finished text from speech recognition, and when it was said.
type Heard struct {
	Speaker  Speaker
	Text     string
	From, To time.Time
}

// Found is what On the Record answered about a stretch of the call.
type Found struct {
	// Semantic says the archive matched by meaning as well as by words.
	Semantic bool
	Meetings []Meeting
	Notes    []Note
	Items    []Item
	// StrongMeetings and StrongItems are the ones the archive itself
	// suggests linking: those nearly as strong as its best match.
	StrongMeetings, StrongItems []string
}

// Meeting is an earlier meeting the archive found.
type Meeting struct {
	ID, Title, Date, URL string
	// Why holds the archive's reasons, such as "close in meaning (0.61)"
	// or "shares launch, pricing".
	Why      []string
	Passages []Passage
}

// Passage is a moment in a meeting: what was said, when, and its link.
type Passage struct{ Text, At, URL string }

// Note is a notebook entry the archive found.
type Note struct {
	ID, Title, Updated, URL string
	Why                     []string
}

// Item is an action item on the archive's board.
type Item struct {
	ID, Text, Status, Owner, URL string
	// Meeting and MeetingDate name the meeting it came from, if any.
	Meeting, MeetingDate string
}

// Brief is what On the Record answered about a question: the lines of its
// cited brief that come from meetings, in its order, and how many passages
// of the meetings' notes it found.
type Brief struct {
	Points   []Point
	Passages int
}

// Point is a line of a brief: a bullet of a meeting's notes, or a passage of
// its transcript, with where it was said and what it sits under.
type Point struct {
	Text string
	// Under is what it sits under: its topic's title and its section's
	// heading in the meeting's notes.
	Under         string
	Meeting, Date string
	// At is when it was said in the meeting, such as "6:12", when the brief
	// says. URL opens that moment, or else the meeting.
	At, URL string
	// Said marks a passage of the transcript, not of the notes.
	Said bool
}

// Archive is On the Record, as the live side asks it.
type Archive interface {
	// Connections asks what a stretch of the call connects to. It returns
	// once ctx is done, if not before.
	Connections(ctx context.Context, text string) (Found, error)
	// Answer asks what the archive knows about a question asked in the
	// call, or a topic brought up, with passages of the transcripts when
	// transcript is true. It returns once ctx is done, if not before.
	Answer(ctx context.Context, question string, transcript bool) (Brief, error)
}

// Screen is what the Mac does for the live side.
type Screen interface {
	// Ask asks whether On Air may recognize speech, which macOS asks the
	// person the first time. Report follows with SpeechAllowed or
	// SpeechRefused.
	Ask()
	// Listen starts or stops recognizing the speech in capture's two
	// sounds. While on, finished words come to Heard about every chunk.
	Listen(on bool, chunk time.Duration)
	// Show sets what the panel shows.
	Show(Panel)
}

// Logger is what a Screen may also do: keep the lines of a diagnostic log,
// which say where words go missing in counts and kinds only, never words
// that were said or anything On the Record answered.
type Logger interface {
	Log(line string)
}

// Panel is what the panel shows. One that isn't Visible is hidden.
type Panel struct {
	Visible bool
	// Display is the display the panel sits at the right edge of: the one
	// being recorded, or the main one when empty.
	Display string
	// Headline is the line under the panel's title: whether speech may be
	// recognized, the words heard in the last minute, the person's and
	// everyone else's, and how the last question to On the Record went.
	Headline string
	// Status is a line or two on what the live side is doing, or what is
	// wrong.
	Status string
	// Calls are what a judge said that cleared its thresholds, newest
	// first, such as "Decision: agreed, at 15:04."
	Calls []string
	// Answers are answer cards, newest first, above the links.
	Answers []Answer
	// Links are newest first.
	Links []Link
}

// Answer is an answer card: a question asked in the call, or a topic
// brought up, as heard, the lines of On the Record's notes that answer it,
// best first, and a word on it when there are none, such as that it is
// still being asked or that the archive has no answer.
type Answer struct {
	Question string
	Lines    []AnswerLine
	Note     string
}

// AnswerLine is a line of an answer: what was noted, short enough for a
// line or two, where it was said, such as "Pricing review · 2026-11-03 ·
// 12:31", and the moment a click opens.
type AnswerLine struct{ Text, Where, URL string }

// Kind is what a link leads to.
type Kind int

const (
	MeetingLink Kind = iota + 1
	NoteLink
	ItemLink
)

func (k Kind) String() string {
	switch k {
	case MeetingLink:
		return "Meeting"
	case NoteLink:
		return "Notebook"
	case ItemLink:
		return "Open item"
	}
	return ""
}

// Link is one thing the panel shows: a title, a date, a line of why, and
// where a click goes.
type Link struct {
	Kind  Kind
	ID    string
	Title string
	Date  string
	Why   string
	URL   string
}

func (l Link) key() string { return l.Kind.String() + ":" + l.ID }

// Problem is something the Mac reports about listening.
type Problem int

const (
	// SpeechAllowed says On Air may recognize speech.
	SpeechAllowed Problem = iota + 1
	// SpeechRefused says it may not. The detail says how.
	SpeechRefused
	// SpeechUnsupported says this Mac can't recognize the language on the
	// Mac itself, and On Air doesn't ask Apple's servers. The detail
	// names the language.
	SpeechUnsupported
	// SpeechFailed says recognition reported an error. The detail says
	// what.
	SpeechFailed
	// SoundDropped says some of the sound was left out because
	// recognition fell behind. The detail is how many buffers so far.
	SoundDropped
)

// Config holds the numbers the live side decides by.
type Config struct {
	Window  time.Duration // how much of the call is kept and sent: about a minute
	Every   time.Duration // the least time between two questions to On the Record
	Least   int           // words in the window before a question is worth asking
	Chars   int           // the most of the transcript kept, from the newest
	Most    int           // links on the panel at most
	Quiet   time.Duration // how long a link shown isn't shown again
	Timeout time.Duration // how long a question may take
	Chunk   time.Duration // how often speech recognition hands over finished words
	Linger  time.Duration // how long the panel stays after a recording stops, when it shows links
	Tick    time.Duration // how often the controller looks at the clock

	JudgeEvery   time.Duration // the least time between two rounds of typed questions
	JudgeTimeout time.Duration // how long a round may take
	Hold         time.Duration // how long what a judge said stays in the panel

	Cards      int           // answer cards on the panel at most
	Again      time.Duration // how long a question asked isn't asked again
	Transcript bool          // whether answers draw on the transcripts too, which takes the archive longer
}

// Defaults are the numbers On Air runs with.
var Defaults = Config{
	Window:  time.Minute,
	Every:   15 * time.Second,
	Least:   8,
	Chars:   4000,
	Most:    3,
	Quiet:   5 * time.Minute,
	Timeout: 10 * time.Second,
	Chunk:   15 * time.Second,
	Linger:  2 * time.Minute,
	Tick:    time.Second,

	JudgeEvery:   10 * time.Second,
	JudgeTimeout: 2 * time.Second,
	Hold:         2 * time.Minute,

	Cards: 3,
	Again: 3 * time.Minute,
	// The archive took 2.0 s for a brief without the transcripts and 3.6 s
	// with them, idle on the owner's Mac, and its other answers took up to
	// 6 s during a call: without them an answer stays well inside Timeout.
	Transcript: false,
}
