// Package live decides what On Air's live panel shows while it records: the
// last minute or so of what was said, when to ask On the Record what that
// minute connects to, and which of the links it answers with are worth
// showing. It touches no screen, no network and no sound. The Mac
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

// Archive is On the Record, as the live side asks it.
type Archive interface {
	// Connections asks what a stretch of the call connects to. It returns
	// once ctx is done, if not before.
	Connections(ctx context.Context, text string) (Found, error)
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

// Panel is what the panel shows. One that isn't Visible is hidden.
type Panel struct {
	Visible bool
	// Display is the display the panel sits at the right edge of: the one
	// being recorded, or the main one when empty.
	Display string
	// Status is a line or two on what the live side is doing, or what is
	// wrong.
	Status string
	// Links are newest first.
	Links []Link
}

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
}

// Defaults are the numbers On Air runs with.
var Defaults = Config{
	Window:  time.Minute,
	Every:   15 * time.Second,
	Least:   8,
	Chars:   4000,
	Most:    3,
	Quiet:   5 * time.Minute,
	Timeout: 4 * time.Second,
	Chunk:   15 * time.Second,
	Linger:  2 * time.Minute,
	Tick:    time.Second,
}
