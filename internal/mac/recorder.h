// The part of On Air that touches macOS: the menu-bar item, the screen and
// sound capture, and the writer that cuts them into segments. It decides
// nothing. It reports what happened to Go through the callbacks in mac.go,
// and Go tells it what to show and what to record.
#ifndef ONAIR_RECORDER_H
#define ONAIR_RECORDER_H

typedef struct {
    int rows;            // the picture's height at most; its width follows the display
    int fps;             // frames a second at most
    double pieceSeconds; // how long a piece is
    int videoBitrate;    // bits a second
    int microphone;      // 1 to mix the microphone into the display's sound
    int window;          // 1 to record the picked window
    const char *display; // otherwise the display to record, by its identifier, or "" for the main one
    double areaX, areaY, areaWidth, areaHeight; // a part of it, in its points from its top left; no width for all of it
} onair_config;

// What onairEvent reports.
enum {
    ONAIR_STARTED = 1,           // capture is running
    ONAIR_FINISHED = 2,          // the last segment was handed over
    ONAIR_FAILED = 3,            // capture stopped or never started; the message says why
    ONAIR_MICROPHONE_FAILED = 4, // capture didn't start because of the microphone, and isn't running; the message says why
    ONAIR_TARGET_GONE = 5,       // capture didn't start because what it was asked to record isn't there, and isn't running
    ONAIR_WINDOW_CLOSED = 6,     // the picked window closed
    ONAIR_PICKER_FAILED = 7,     // the window picker didn't open; the message says why
};

// What onairMenu reports.
enum {
    ONAIR_MENU_START = 1,
    ONAIR_MENU_STOP = 2,
    ONAIR_MENU_LAST = 3,
    ONAIR_MENU_FOLDER = 4,
    ONAIR_MENU_QUIT = 5,
    ONAIR_MENU_AREA = 6,   // Select Area…
    ONAIR_MENU_WHOLE = 7,  // Whole Display
    ONAIR_MENU_WINDOW = 8, // Select Window…
};

// A connected display, as onairDisplays reports it.
typedef struct {
    const char *id;       // stays the same across launches and reconnections
    const char *name;     // what macOS calls it
    double width, height; // in points
    int main;             // 1 for the display with the menu bar
} onair_display;

// A display, as the menu offers it.
typedef struct {
    const char *id;
    const char *title;
    int picked; // 1 for the one that is marked
} onair_choice;

// Start asks for capture and returns at once. ONAIR_STARTED, ONAIR_FAILED,
// ONAIR_MICROPHONE_FAILED or ONAIR_TARGET_GONE follows. Segments arrive
// through onairPiece: kind 0 is the opening segment, kind 1 a media segment.
void onair_start(onair_config cfg);
// Stop ends capture. ONAIR_FINISHED follows the last segment.
void onair_stop(void);

// Selftest writes segments from made-up frames and sound through the same
// writer, with no capture, and returns once the last one was handed over.
// Each sound track goes to the writer directly. It returns 0, or -1 with a
// message the caller frees.
int onair_selftest(double seconds, int audioTracks, double pieceSeconds, char **error);

// What onair_selftest_mix plays: two made-up sources shaped like capture's.
typedef struct {
    double seconds;      // how long
    double pieceSeconds; // how long a piece is
    double quietFrom;    // when the microphone stops sending
    double quietTo;      // and when it starts again
    double switchAt;     // when the microphone goes from 44.1 kHz to 48 kHz
} onair_mix;

// What the mixer did in onair_selftest_mix.
typedef struct {
    long late, early; // sound frames it left out
    long handed;      // mixed buffers it handed to the writer
    long dropped;     // of those, ones the writer refused
} onair_mix_report;

// Selftest_mix does what onair_selftest does with the display's sound and
// the microphone mixed into one sound track, as capture does.
int onair_selftest_mix(onair_mix plan, onair_mix_report *report, char **error);

// What onair_probe read from a movie file.
typedef struct {
    double seconds;                   // its length
    int videoTracks, audioTracks;     // its tracks
    double videoStart, audioStart;    // where the first of each kind starts
} onair_movie;

// Probe reads a movie file's length and tracks, as a player would. It
// returns 0, or -1 with a message the caller frees.
int onair_probe(const char *path, onair_movie *movie, char **error);

// Sound decodes a movie file's sound track to 48 kHz stereo float samples,
// interleaved, in memory the caller frees. start is when its first sample is
// played, in seconds from the first frame. It returns 0, or -1 with a
// message the caller frees.
int onair_sound(const char *path, float **samples, long *frames, double *start, char **error);

// FrameSize is the picture's size in pixels for content of a size in points
// at a scale, brought down to rows at most, with even sides.
void onair_frame_size(double width, double height, double scale, int rows, int *wide, int *high);

// Run shows the menu-bar item and runs the app. It doesn't return.
// onairDisplays reports the connected displays when it starts and whenever
// they change.
void onair_run(void);
// Show sets what the menu-bar item says. Any thread may call it.
void onair_show(int recording, const char *last, const char *note);
// Choices sets the part of the menu that picks what to record: whether a
// choice can be made, the displays, and what names the picked area and the
// picked window, if any. Any thread may call it.
void onair_choices(int open, const onair_choice *displays, int count, const char *area, const char *window);
// SelectArea dims a display and lets a person drag out an area of it.
// onairArea follows, unless they cancel.
void onair_select_area(const char *display);
// SelectWindow shows the system's picker for one window, which the edge
// keeps. onairWindow follows, unless they cancel.
void onair_select_window(void);
// Reveal shows a file or a folder in the Finder.
void onair_reveal(const char *path);
// Quit ends the app.
void onair_quit(void);

#endif
